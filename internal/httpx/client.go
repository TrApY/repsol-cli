// Package httpx provides the HTTP transport shared by every client in the
// CLI. It sends the CLI user agent, maps HTTP statuses onto typed application
// errors, retries safe reads with backoff, and writes one redacted debug
// record per attempt.
package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

const (
	// maxResponseBytes caps how much of a response body the client reads.
	maxResponseBytes = 8 << 20
	// maxLoggedBodyRunes caps the response body in a debug record.
	maxLoggedBodyRunes = 4096
	// defaultTimeout bounds a single HTTP call, response body included.
	defaultTimeout = 30 * time.Second
	// fallbackUserAgent is sent when the caller configures no user agent, so
	// Go's default user agent is never used.
	fallbackUserAgent = "repsol-cli"
)

// Client is the shared HTTP transport. It is safe for concurrent use.
type Client struct {
	// HTTP is the underlying client. Do falls back to http.DefaultClient when
	// it is nil.
	HTTP *http.Client
	// UserAgent is sent on every request.
	UserAgent string
	// Logger receives one debug record per attempt. A nil Logger disables
	// debug logging.
	Logger *slog.Logger
	// Attempts is the maximum number of attempts one Do call makes.
	Attempts int
	// Backoff returns how long to wait before retry attempt n (1 for the
	// first wait). A nil Backoff means no wait.
	Backoff func(attempt int) time.Duration
}

// New returns a client with a 30 second timeout, three attempts and an
// exponential backoff of 250 ms, 1 s and 4 s. A nil logger disables debug
// logging.
func New(userAgent string, logger *slog.Logger) *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: defaultTimeout},
		UserAgent: userAgent,
		Logger:    logger,
		Attempts:  3,
		Backoff: func(attempt int) time.Duration {
			if attempt < 1 {
				attempt = 1
			}
			delay := 250 * time.Millisecond
			for i := 1; i < attempt; i++ {
				delay *= 4
			}
			return delay
		},
	}
}

// Request is one HTTP call. Endpoint defaults to the URL path and is the only
// location that appears in errors.
type Request struct {
	// Method is the HTTP method. An empty Method means GET.
	Method string
	// URL is the full request URL, query string included.
	URL string
	// Header holds extra request headers. It is cloned, never mutated.
	Header http.Header
	// Body is the request body, sent as-is.
	Body []byte
	// Retry allows retries on retryable failures. Only safe reads should set
	// it.
	Retry bool
	// Endpoint is the API path reported in errors and logs. It defaults to
	// the path of URL.
	Endpoint string
}

// Response is one HTTP response.
type Response struct {
	// Status is the HTTP status code.
	Status int
	// Header holds the response headers.
	Header http.Header
	// Body is the response body, capped at 8 MiB.
	Body []byte
}

// Do sends the request, retrying retryable failures when r.Retry is set. It
// returns an *apperr.Error for any non-2xx status or transport failure, and
// ctx.Err() unwrapped when the context is done.
func (c *Client) Do(ctx context.Context, r Request) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	endpoint := r.Endpoint
	if endpoint == "" {
		if u, err := url.Parse(r.URL); err == nil {
			endpoint = u.Path
		}
	}

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	attempts := c.Attempts
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			var delay time.Duration
			if c.Backoff != nil {
				delay = c.Backoff(attempt - 1)
			}
			if err := wait(ctx, delay); err != nil {
				return nil, err
			}
		}

		resp, retryable, err := c.attempt(ctx, httpClient, method, endpoint, r)
		if err == nil {
			return resp, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		lastErr = err
		if !r.Retry || !retryable {
			return nil, err
		}
	}
	return nil, lastErr
}

// attempt performs one HTTP request. It reports whether the returned error is
// retryable.
func (c *Client) attempt(
	ctx context.Context,
	httpClient *http.Client,
	method, endpoint string,
	r Request,
) (*Response, bool, error) {
	req, err := newRequest(ctx, c.UserAgent, method, r)
	if err != nil {
		appErr := apperr.From(err)
		appErr.Endpoint = endpoint
		return nil, false, appErr
	}

	start := time.Now()
	raw, doErr := httpClient.Do(req)
	elapsed := time.Since(start)
	if doErr != nil {
		appErr := transportError(method, endpoint, req.URL, doErr)
		c.logAttempt(ctx, r, req, 0, elapsed, nil)
		return nil, true, appErr
	}
	defer raw.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(raw.Body, maxResponseBytes))
	c.logAttempt(ctx, r, req, raw.StatusCode, elapsed, body)
	if readErr != nil {
		appErr := apperr.New(apperr.Network, "network_error",
			fmt.Sprintf("%s %s%s: reading the response: %v", method, req.URL.Host, endpoint, readErr))
		appErr.Endpoint = endpoint
		appErr.Retryable = true
		return nil, true, appErr
	}

	if raw.StatusCode < http.StatusOK || raw.StatusCode > 299 {
		appErr := statusError(method, endpoint, raw.StatusCode)
		return nil, appErr.Retryable, appErr
	}

	return &Response{Status: raw.StatusCode, Header: raw.Header, Body: body}, false, nil
}

// newRequest builds a fresh request for one attempt with the CLI defaults
// applied. The caller's Header is cloned and never mutated.
func newRequest(ctx context.Context, userAgent, method string, r Request) (*http.Request, error) {
	var body io.Reader
	if len(r.Body) > 0 {
		body = bytes.NewReader(r.Body)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.URL, body)
	if err != nil {
		// The parse error embeds the whole URL, which may carry secrets, so
		// it is deliberately not wrapped.
		return nil, apperr.New(apperr.Generic, "invalid_request", "malformed request URL")
	}

	if r.Header != nil {
		req.Header = r.Header.Clone()
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if userAgent == "" {
		userAgent = fallbackUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	if len(r.Body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// transportError maps a failed round trip onto a retryable network error. Only
// the host and the endpoint are reported; the full URL never appears.
func transportError(method, endpoint string, reqURL *url.URL, err error) *apperr.Error {
	inner := err
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		inner = urlErr.Err
	}

	code := "network_error"
	var timedOut interface{ Timeout() bool }
	if errors.As(err, &timedOut) && timedOut.Timeout() {
		code = "timeout"
	}

	host := ""
	if reqURL != nil {
		host = reqURL.Host
	}
	appErr := apperr.New(apperr.Network, code,
		fmt.Sprintf("%s %s%s: %v", method, host, endpoint, inner))
	appErr.Endpoint = endpoint
	appErr.Retryable = true
	return appErr
}

// statusError maps a non-2xx response onto the documented category and code.
func statusError(method, endpoint string, status int) *apperr.Error {
	message := fmt.Sprintf("%s %s: HTTP %d %s", method, endpoint, status, http.StatusText(status))

	var appErr *apperr.Error
	switch {
	case status == http.StatusUnauthorized:
		appErr = apperr.New(apperr.Auth, "unauthorized", message)
	case status == http.StatusForbidden:
		appErr = apperr.New(apperr.Blocked, "forbidden", message).
			WithHint(`Repsol's firewall or session refused the request; run "repsol auth status --check"`)
	case status == http.StatusNotFound:
		appErr = apperr.New(apperr.NotFound, "not_found", message)
	case status == http.StatusTooManyRequests:
		appErr = apperr.New(apperr.Blocked, "rate_limited", message)
		appErr.Retryable = true
	case status >= 500 && status <= 599:
		appErr = apperr.New(apperr.Network, "server_error", message)
		appErr.Retryable = true
	default:
		appErr = apperr.New(apperr.APIChanged, "unexpected_status", message)
	}
	appErr.HTTPStatus = status
	appErr.Endpoint = endpoint
	return appErr
}

// logAttempt writes one debug record for an attempt. The request body is
// redacted, and the response body is redacted before it is cut to
// maxLoggedBodyRunes.
func (c *Client) logAttempt(
	ctx context.Context,
	r Request,
	req *http.Request,
	status int,
	elapsed time.Duration,
	responseBody []byte,
) {
	if c.Logger == nil {
		return
	}

	redactedURL := ""
	if req.URL != nil {
		redactedURL = RedactURL(req.URL)
	}
	attrs := []slog.Attr{
		slog.String("method", req.Method),
		slog.String("url", redactedURL),
		slog.Int("status", status),
		slog.Int64("duration_ms", elapsed.Milliseconds()),
		slog.Any("request_headers", RedactHeader(req.Header)),
		slog.String("request_body", RedactBody(r.Body)),
		slog.String("response_body", truncateRunes(RedactBody(responseBody), maxLoggedBodyRunes)),
	}
	c.Logger.LogAttrs(ctx, slog.LevelDebug, "http", attrs...)
}

// wait sleeps for d unless the context is done first.
func wait(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// truncateRunes cuts s to at most max runes without splitting a character.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
