package gigya

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/httpx"
)

func newTestClient(t *testing.T, serverURL string, now time.Time) *Client {
	t.Helper()
	return &Client{
		BaseURL: serverURL,
		APIKey:  APIKey,
		HTTP:    httpx.New("repsol-cli/test", nil),
		Now:     func() time.Time { return now },
	}
}

func checkAppError(t *testing.T, err error, wantCategory apperr.Category, wantCode string) *apperr.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil, want category %v and code %q", wantCategory, wantCode)
	}
	typed := apperr.From(err)
	if typed.Category != wantCategory {
		t.Fatalf("category = %v, want %v (code %q, message %q)", typed.Category, wantCategory, typed.Code, typed.Message)
	}
	if typed.Code != wantCode {
		t.Fatalf("code = %q, want %q", typed.Code, wantCode)
	}
	return typed
}

func TestAccountInfoSendsGMIDCookie(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/accounts.getAccountInfo" {
			t.Errorf("path = %q, want /accounts.getAccountInfo", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		want := map[string]string{
			"APIKey":      APIKey,
			"login_token": "tok",
			"format":      "json",
		}
		if len(r.PostForm) != len(want) {
			t.Errorf("form has %d fields (%v), want %d", len(r.PostForm), r.PostForm, len(want))
		}
		for key, value := range want {
			if got := r.PostForm.Get(key); got != value {
				t.Errorf("form %s = %q, want %q", key, got, value)
			}
		}
		cookie, err := r.Cookie("gmid")
		if err != nil {
			t.Errorf("gmid cookie: %v", err)
		} else if cookie.Value != "g1" {
			t.Errorf("gmid cookie = %q, want g1", cookie.Value)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"errorCode":0,"UID":"u1","UIDSignature":"s1","signatureTimestamp":"1700000000","profile":{"email":"owner@example.com","firstName":"Owner"}}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, now)
	sig, acct, err := c.AccountInfo(context.Background(), Session{LoginToken: "tok", GMID: "g1"})
	if err != nil {
		t.Fatalf("AccountInfo: %v", err)
	}
	wantSig := Signature{UID: "u1", UIDSignature: "s1", Timestamp: "1700000000", ObtainedAt: now}
	if sig != wantSig {
		t.Errorf("signature = %+v, want %+v", sig, wantSig)
	}
	wantAcct := Account{UID: "u1", Email: "owner@example.com", FirstName: "Owner"}
	if acct != wantAcct {
		t.Errorf("account = %+v, want %+v", acct, wantAcct)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestAccountInfoErrors(t *testing.T) {
	tests := []struct {
		name         string
		errorCode    int
		errorMessage string
		wantCategory apperr.Category
		wantCode     string
		wantRetry    bool
	}{
		{
			name:         "session rejected",
			errorCode:    403005,
			errorMessage: "Unauthorized user",
			wantCategory: apperr.Auth,
			wantCode:     CodeSessionRejected,
		},
		{
			name:         "gigya unavailable",
			errorCode:    500001,
			errorMessage: "Server error",
			wantCategory: apperr.Network,
			wantCode:     "gigya_unavailable",
			wantRetry:    true,
		},
		{
			name:         "bad request",
			errorCode:    400006,
			errorMessage: "Invalid request",
			wantCategory: apperr.APIChanged,
			wantCode:     "gigya_bad_request",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"errorCode":%d,"errorMessage":%q}`, tc.errorCode, tc.errorMessage)
			}))
			defer srv.Close()

			c := newTestClient(t, srv.URL, time.Now())
			_, _, err := c.AccountInfo(context.Background(), Session{LoginToken: "tok", GMID: "g1"})
			typed := checkAppError(t, err, tc.wantCategory, tc.wantCode)
			if typed.Retryable != tc.wantRetry {
				t.Errorf("Retryable = %v, want %v", typed.Retryable, tc.wantRetry)
			}
			if got := fmt.Sprint(tc.errorCode); !strings.Contains(typed.Message, got) {
				t.Errorf("message %q does not mention errorCode %s", typed.Message, got)
			}
			if !strings.Contains(typed.Message, tc.errorMessage) {
				t.Errorf("message %q does not mention Gigya's errorMessage %q", typed.Message, tc.errorMessage)
			}
		})
	}
}

func TestBootstrapReadsGMIDCookie(t *testing.T) {
	t.Run("reads gmid", func(t *testing.T) {
		var calls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.URL.Path != "/accounts.webSdkBootstrap" {
				t.Errorf("path = %q, want /accounts.webSdkBootstrap", r.URL.Path)
			}
			if r.Method != http.MethodGet {
				t.Errorf("method = %q, want GET", r.Method)
			}
			query := r.URL.Query()
			want := map[string]string{
				"apiKey":  APIKey,
				"pageURL": PageURL,
				"sdk":     "js_latest",
				"format":  "json",
			}
			if len(query) != len(want) {
				t.Errorf("query has %d fields (%v), want %d", len(query), query, len(want))
			}
			for key, value := range want {
				if got := query.Get(key); got != value {
					t.Errorf("query %s = %q, want %q", key, got, value)
				}
			}
			w.Header().Add("Set-Cookie", "gmid=boot-gmid; Path=/")
			w.Header().Add("Set-Cookie", "ucid=x")
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"errorCode":0}`)
		}))
		defer srv.Close()

		c := newTestClient(t, srv.URL, time.Now())
		gmid, err := c.Bootstrap(context.Background())
		if err != nil {
			t.Fatalf("Bootstrap: %v", err)
		}
		if gmid != "boot-gmid" {
			t.Errorf("gmid = %q, want boot-gmid", gmid)
		}
		if calls != 1 {
			t.Errorf("calls = %d, want 1", calls)
		}
	})

	t.Run("no gmid cookie", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Set-Cookie", "ucid=x")
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"errorCode":0}`)
		}))
		defer srv.Close()

		c := newTestClient(t, srv.URL, time.Now())
		_, err := c.Bootstrap(context.Background())
		if err == nil {
			t.Fatal("Bootstrap err = nil, want APIChanged")
		}
		if got := apperr.From(err).Category; got != apperr.APIChanged {
			t.Fatalf("category = %v, want %v", got, apperr.APIChanged)
		}
	})
}

func TestLoginForm(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	wantForm := map[string]string{
		"loginID":           "owner@example.com",
		"password":          "hunter2",
		"APIKey":            APIKey,
		"sessionExpiration": "0",
		"targetEnv":         "jssdk",
		"include":           "profile,data,emails,subscriptions,preferences,",
		"includeUserInfo":   "true",
		"loginMode":         "standard",
		"lang":              "es",
		"sdk":               "js_latest",
		"authMode":          "cookie",
		"pageURL":           PageURL,
		"format":            "json",
	}
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/accounts.login" {
			t.Errorf("path = %q, want /accounts.login", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		if len(r.PostForm) != len(wantForm) {
			t.Errorf("form has %d fields, want %d: %v", len(r.PostForm), len(wantForm), r.PostForm)
		}
		for key, value := range wantForm {
			if got := r.PostForm.Get(key); got != value {
				t.Errorf("form %s = %q, want %q", key, got, value)
			}
		}
		cookie, err := r.Cookie("gmid")
		if err != nil {
			t.Errorf("gmid cookie: %v", err)
		} else if cookie.Value != "g1" {
			t.Errorf("gmid cookie = %q, want g1", cookie.Value)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"errorCode":0,"UID":"u1","UIDSignature":"s1","signatureTimestamp":1700000001,"profile":{"email":"owner@example.com","firstName":"Owner"},"sessionInfo":{"login_token":"st2.new","expires_in":"0"}}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, now)
	session, sig, acct, err := c.Login(context.Background(), "g1", "owner@example.com", "hunter2")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	wantSession := Session{LoginToken: "st2.new", GMID: "g1"}
	if session != wantSession {
		t.Errorf("session = %+v, want %+v", session, wantSession)
	}
	wantSig := Signature{UID: "u1", UIDSignature: "s1", Timestamp: "1700000001", ObtainedAt: now}
	if sig != wantSig {
		t.Errorf("signature = %+v, want %+v", sig, wantSig)
	}
	wantAcct := Account{UID: "u1", Email: "owner@example.com", FirstName: "Owner"}
	if acct != wantAcct {
		t.Errorf("account = %+v, want %+v", acct, wantAcct)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestLoginErrors(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode string
		wantHint string
	}{
		{
			name:     "invalid credentials",
			body:     `{"errorCode":403042,"errorMessage":"Invalid loginID or password"}`,
			wantCode: CodeInvalidCredentials,
		},
		{
			name:     "captcha required",
			body:     `{"errorCode":401020,"errorMessage":"Captcha required"}`,
			wantCode: CodeCaptchaRequired,
			wantHint: "--har",
		},
		{
			name:     "captcha required by risk",
			body:     `{"errorCode":401021,"errorMessage":"Verification required"}`,
			wantCode: CodeCaptchaRequired,
			wantHint: "--har",
		},
		{
			name:     "account locked",
			body:     `{"errorCode":403120,"errorMessage":"Account locked"}`,
			wantCode: CodeAccountLocked,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			c := newTestClient(t, srv.URL, time.Now())
			_, _, _, err := c.Login(context.Background(), "g1", "owner@example.com", "hunter2")
			typed := checkAppError(t, err, apperr.Auth, tc.wantCode)
			if tc.wantHint != "" && !strings.Contains(typed.Hint, tc.wantHint) {
				t.Errorf("hint = %q, want it to contain %q", typed.Hint, tc.wantHint)
			}
		})
	}

	t.Run("no session token", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"errorCode":0,"UID":"u1"}`)
		}))
		defer srv.Close()

		c := newTestClient(t, srv.URL, time.Now())
		_, _, _, err := c.Login(context.Background(), "g1", "owner@example.com", "hunter2")
		if err == nil {
			t.Fatal("Login err = nil, want APIChanged")
		}
		if got := apperr.From(err).Category; got != apperr.APIChanged {
			t.Fatalf("category = %v, want %v", got, apperr.APIChanged)
		}
	})
}

func TestLoginIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, time.Now())
	_, _, _, err := c.Login(context.Background(), "g1", "owner@example.com", "hunter2")
	if err == nil {
		t.Fatal("Login err = nil, want an error")
	}
	if got := apperr.From(err).Category; got != apperr.Network {
		t.Errorf("category = %v, want %v", got, apperr.Network)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("server saw %d calls, want 1", got)
	}
}

func TestAccountInfoEscapesFormValues(t *testing.T) {
	// Guards against a token or gmid being placed in the URL instead of the
	// form or the cookie header.
	const token = "st2.a=b=="
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want empty", r.URL.RawQuery)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		if got := r.PostForm.Get("login_token"); got != token {
			t.Errorf("login_token = %q, want %q", got, token)
		}
		cookie, err := r.Cookie("gmid")
		if err != nil {
			t.Errorf("gmid cookie: %v", err)
		} else if cookie.Value != "gmid.ver4.x==" {
			t.Errorf("gmid cookie = %q, want gmid.ver4.x==", cookie.Value)
		}
		io.WriteString(w, `{"errorCode":0,"UID":"u1","UIDSignature":"s1","signatureTimestamp":"1"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, time.Now())
	if _, _, err := c.AccountInfo(context.Background(), Session{LoginToken: token, GMID: "gmid.ver4.x=="}); err != nil {
		t.Fatalf("AccountInfo: %v", err)
	}
}
