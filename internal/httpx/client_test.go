package httpx

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

func TestDoSendsUserAgentAndAccept(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []*http.Request
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		seen = append(seen, r.Clone(context.Background()))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := New("repsol-cli/test", nil)

	do := func(req Request) {
		t.Helper()
		if _, err := c.Do(context.Background(), req); err != nil {
			t.Fatalf("Do(%s %s): %v", req.Method, req.Endpoint, err)
		}
	}
	do(Request{Method: http.MethodGet, URL: srv.URL + "/plain"})
	do(Request{Method: http.MethodPost, URL: srv.URL + "/json", Body: []byte(`{"a":1}`)})
	do(Request{
		Method: http.MethodPost,
		URL:    srv.URL + "/text",
		Header: http.Header{"Content-Type": {"text/plain"}},
		Body:   []byte(`plain`),
	})

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 {
		t.Fatalf("server saw %d requests, want 3", len(seen))
	}
	for i, r := range seen {
		if got := r.UserAgent(); got != "repsol-cli/test" {
			t.Errorf("request %d User-Agent = %q, want %q", i, got, "repsol-cli/test")
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("request %d Accept = %q, want %q", i, got, "application/json")
		}
	}
	if got := seen[0].Header.Get("Content-Type"); got != "" {
		t.Errorf("GET Content-Type = %q, want empty", got)
	}
	if got := seen[1].Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("POST with body Content-Type = %q, want %q", got, "application/json")
	}
	if got := seen[2].Header.Get("Content-Type"); got != "text/plain" {
		t.Errorf("explicit Content-Type = %q, want %q", got, "text/plain")
	}
}

func TestDoMapsStatus(t *testing.T) {
	cases := []struct {
		status    int
		cat       apperr.Category
		retryable bool
	}{
		{401, apperr.Auth, false},
		{403, apperr.Blocked, false},
		{404, apperr.NotFound, false},
		{429, apperr.Blocked, true},
		{500, apperr.Network, true},
		{503, apperr.Network, true},
		{418, apperr.APIChanged, false},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":true}`))
			}))
			defer srv.Close()

			c := New("repsol-cli/test", nil)
			_, err := c.Do(context.Background(), Request{
				Method: http.MethodGet,
				URL:    srv.URL + "/thing",
			})
			if err == nil {
				t.Fatalf("Do returned no error for status %d", tc.status)
			}
			e := apperr.From(err)
			if e.Category != tc.cat {
				t.Errorf("Category = %v (%d), want %v (%d)", e.Category, e.Category, tc.cat, tc.cat)
			}
			if e.Retryable != tc.retryable {
				t.Errorf("Retryable = %v, want %v", e.Retryable, tc.retryable)
			}
			if e.HTTPStatus != tc.status {
				t.Errorf("HTTPStatus = %d, want %d", e.HTTPStatus, tc.status)
			}
			if e.Endpoint != "/thing" {
				t.Errorf("Endpoint = %q, want %q", e.Endpoint, "/thing")
			}
			if tc.status == http.StatusForbidden && !strings.Contains(e.Hint, "auth status --check") {
				t.Errorf("403 Hint = %q, want mention of auth status --check", e.Hint)
			}
		})
	}
}

func TestDoRetriesOnlyWhenAllowed(t *testing.T) {
	t.Run("retry enabled retries and succeeds", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) < 3 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		defer srv.Close()

		c := New("repsol-cli/test", nil)
		c.Backoff = func(int) time.Duration { return 0 }

		resp, err := c.Do(context.Background(), Request{
			Method: http.MethodGet,
			URL:    srv.URL + "/thing",
			Retry:  true,
		})
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		if resp.Status != http.StatusOK {
			t.Errorf("Status = %d, want %d", resp.Status, http.StatusOK)
		}
		if got := calls.Load(); got != 3 {
			t.Errorf("server saw %d calls, want 3", got)
		}
	})

	t.Run("retry disabled sends once", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := New("repsol-cli/test", nil)
		c.Backoff = func(int) time.Duration { return 0 }

		_, err := c.Do(context.Background(), Request{
			Method: http.MethodGet,
			URL:    srv.URL + "/thing",
		})
		if err == nil {
			t.Fatal("Do returned no error")
		}
		if got := apperr.From(err).Category; got != apperr.Network {
			t.Errorf("Category = %v, want %v", got, apperr.Network)
		}
		if got := calls.Load(); got != 1 {
			t.Errorf("server saw %d calls, want 1", got)
		}
	})

	t.Run("attempts are capped", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := New("repsol-cli/test", nil)
		c.Backoff = func(int) time.Duration { return 0 }

		_, err := c.Do(context.Background(), Request{
			Method: http.MethodGet,
			URL:    srv.URL + "/thing",
			Retry:  true,
		})
		if err == nil {
			t.Fatal("Do returned no error")
		}
		if got := apperr.From(err).Category; got != apperr.Network {
			t.Errorf("Category = %v, want %v", got, apperr.Network)
		}
		if got := calls.Load(); got != int32(c.Attempts) {
			t.Errorf("server saw %d calls, want %d", got, c.Attempts)
		}
	})
}

func TestTransportErrorHidesQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	target := srv.URL + "/x?login_token=SECRET"
	srv.Close()

	c := New("repsol-cli/test", nil)
	c.Backoff = func(int) time.Duration { return 0 }

	_, err := c.Do(context.Background(), Request{Method: http.MethodGet, URL: target})
	if err == nil {
		t.Fatal("Do returned no error for a closed server")
	}
	e := apperr.From(err)
	if e.Category != apperr.Network {
		t.Errorf("Category = %v, want %v", e.Category, apperr.Network)
	}
	if !e.Retryable {
		t.Error("Retryable = false, want true")
	}
	if e.Endpoint != "/x" {
		t.Errorf("Endpoint = %q, want %q", e.Endpoint, "/x")
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("error message leaks the query: %v", err)
	}
}

func TestDebugLogRedactsSecrets(t *testing.T) {
	const responseBody = `{"UID":"uid-SECRET","UIDSignature":"sig-SECRET",` +
		`"profile":{"email":"me@example.com"},` +
		`"data":{"DocID":{"Pers":"12345678Z"}},` +
		`"sessionInfo":{"login_token":"st2.SECRET"},` +
		`"address":{"street":"Calle SECRET","number":"7","localityCode":"loc-SECRET"},` +
		`"emails":{"verified":["owner@example.com"],"unverified":[]},` +
		`"userInfo":{"UIDSig":"uidsig-SECRET","nickname":"Owner SECRET",` +
		`"loginProviderUID":"lpuid-SECRET",` +
		`"identities":[{"provider":"site","providerUID":"puid-SECRET","nickname":"Owner SECRET"}]},` +
		`"localityCode":"loc-SECRET","streetCode":"street-SECRET","agencyCode":"0000000001"}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseBody))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	c := New("repsol-cli/test", logger)

	_, err := c.Do(context.Background(), Request{
		Method: http.MethodPost,
		URL:    srv.URL + "/accounts.login?apiKey=k&input=Calle%20SECRET",
		Header: http.Header{
			"Cookie":    {"gmid=gmid-SECRET"},
			"Signature": {"sig-SECRET"},
			"Uid":       {"uid-SECRET"},
		},
		Body: []byte("loginID=me%40example.com&password=pw-SECRET&login_token=st2.SECRET&APIKey=k"),
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	out := buf.String()
	for _, s := range []string{"SECRET", "me@example.com", "me%40example.com", "owner@example.com", "12345678Z"} {
		if strings.Contains(out, s) {
			t.Fatalf("debug log leaks %q:\n%s", s, out)
		}
	}
	if !strings.Contains(out, "REDACTED") {
		t.Fatalf("debug log missing the redaction marker:\n%s", out)
	}
	if !strings.Contains(out, "0000000001") {
		t.Fatalf("debug log dropped the non-sensitive agency code:\n%s", out)
	}
	if !strings.Contains(out, "/accounts.login") {
		t.Fatalf("debug log missing the request path:\n%s", out)
	}
	for _, attr := range []string{"method", "url", "status", "duration_ms", "request_body", "response_body"} {
		if !strings.Contains(out, `"`+attr+`"`) {
			t.Errorf("debug log missing attribute %q:\n%s", attr, out)
		}
	}
}

func TestDoReportsWhetherTheRequestWasSent(t *testing.T) {
	// A failure before a single byte of the request is written cannot have been
	// processed; one after it can, and a status is an answer.
	dropped := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer dropped.Close()

	hung := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-hung
	}))
	defer slow.Close()
	defer close(hung)

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer failing.Close()

	refused := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	refusedURL := refused.URL
	refused.Close() // nothing listens there any more: the connection is refused

	cases := []struct {
		name        string
		url         string
		wantNotSent bool
	}{
		{"connection refused", refusedURL, true},
		{"dropped after the request", dropped.URL, false},
		{"timeout after the request", slow.URL, false},
		{"server error", failing.URL, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New("repsol-cli/test", nil)
			c.Attempts = 1
			c.HTTP.Timeout = 300 * time.Millisecond

			_, err := c.Do(context.Background(), Request{
				Method: http.MethodPost, URL: tc.url + "/write", Body: []byte(`{"a":1}`), Endpoint: "/write",
			})
			if err == nil {
				t.Fatal("Do succeeded, want a failure")
			}
			if got := apperr.From(err).NotSent; got != tc.wantNotSent {
				t.Errorf("NotSent = %v, want %v (%v)", got, tc.wantNotSent, err)
			}
		})
	}
}
