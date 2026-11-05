package repsolapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/httpx"
)

// The request headers of authenticated calls and of writes.
const (
	headerUID                = "uid"
	headerSignature          = "signature"
	headerSignatureTimestamp = "signatureTimestamp"
	headerGigyaUser          = "ptb-gigya-user"

	headerOrderProcessID     = "ptb-order-process-id"
	headerGeneratedUserID    = "ptb-app-generated-user-id"
	headerAppInsightsRequest = "ptb-appinsights-request"
	headerAppInsightsSession = "ptb-appinsights-session"
	headerAppInsightsUser    = "ptb-appinsights-user"
)

// The identifier shapes the web bundles send. The alphabet has exactly 64
// characters, so masking a random byte with randomAlphabetMask picks one
// character without modulo bias.
const (
	processIDLength     = 21
	userIDLength        = 48
	sessionLength       = 22
	telemetryUserLength = 22
	requestIDLength     = 21

	userIDPrefix       = "us:"
	requestIDPrefix    = "rq:"
	randomAlphabet     = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-"
	randomAlphabetMask = 0x3f
)

// Auth is the signature triple that authenticates one call, as Gigya issues it.
type Auth struct {
	UID       string
	Signature string
	Timestamp string
}

// Signer signs authenticated calls. The implementation refreshes the signature
// when it has expired.
type Signer interface {
	Sign(ctx context.Context) (Auth, error)
}

// Telemetry is the set of identifiers the web bundles send with the telemetry
// headers of a write. One Telemetry covers a whole order flow, so the process
// id is the same from /cart/check to /solicitude or /order.
type Telemetry struct {
	ProcessID string
	UserID    string
	Session   string
	User      string
}

// NewTelemetry returns fresh telemetry identifiers: a 21 character process id,
// a "us:" prefixed user id of 48 characters, and 22 character session and user
// ids.
//
// It panics when the system's random source fails, which is unrecoverable for a
// process that needs fresh identifiers.
func NewTelemetry() *Telemetry {
	return &Telemetry{
		ProcessID: randomString(processIDLength),
		UserID:    userIDPrefix + randomString(userIDLength),
		Session:   randomString(sessionLength),
		User:      randomString(telemetryUserLength),
	}
}

// randomString returns n random characters from randomAlphabet.
func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("repsolapi: reading random bytes: " + err.Error())
	}
	for i := range b {
		b[i] = randomAlphabet[b[i]&randomAlphabetMask]
	}
	return string(b)
}

// sign obtains a signature for one authenticated call. A missing signer is an
// auth failure, so no request leaves the process without credentials.
func (c *Client) sign(ctx context.Context) (Auth, error) {
	if c.Signer == nil {
		return Auth{}, apperr.New(apperr.Auth, "not_logged_in",
			`the call needs a signed-in session; run "repsol auth login"`)
	}
	return c.Signer.Sign(ctx)
}

// authHeaders returns the four headers every authenticated call carries.
func authHeaders(a Auth) http.Header {
	h := make(http.Header, 4)
	h.Set(headerUID, a.UID)
	h.Set(headerSignature, a.Signature)
	h.Set(headerSignatureTimestamp, a.Timestamp)
	h.Set(headerGigyaUser, a.UID)
	return h
}

// addTelemetry adds the five telemetry headers of one write. The request id is
// fresh for every call; the other identifiers come from the order flow. A nil
// Telemetry leaves the headers off.
func addTelemetry(h http.Header, t *Telemetry, endpoint string) {
	if t == nil {
		return
	}
	h.Set(headerOrderProcessID, t.ProcessID)
	h.Set(headerGeneratedUserID, t.UserID)
	h.Set(headerAppInsightsRequest, requestIDPrefix+endpoint+":"+randomString(requestIDLength))
	h.Set(headerAppInsightsSession, t.Session)
	h.Set(headerAppInsightsUser, t.User)
}

// signedGET performs a retryable GET carrying the auth headers.
func (c *Client) signedGET(ctx context.Context, auth Auth, endpoint string, params ...queryParam) (*httpx.Response, error) {
	return c.signedRead(ctx, auth, endpoint, true, params...)
}

// signedRead is signedGET with the choice of retrying. A read that is polled
// by its caller sends one request per poll.
func (c *Client) signedRead(ctx context.Context, auth Auth, endpoint string, retry bool, params ...queryParam) (*httpx.Response, error) {
	target := strings.TrimRight(c.BaseURL, "/") + endpoint
	if qs := encodeQuery(params); qs != "" {
		target += "?" + qs
	}
	return c.HTTP.Do(ctx, httpx.Request{
		Method:   http.MethodGet,
		URL:      target,
		Header:   authHeaders(auth),
		Retry:    retry,
		Endpoint: endpoint,
	})
}

// doWrite performs exactly one POST of body. It never retries: a failed write
// may still have reached Repsol.
func doWrite(ctx context.Context, h *httpx.Client, baseURL, endpoint string, header http.Header, body any) (*httpx.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, apperr.New(apperr.Generic, "invalid_request", "could not encode the "+endpoint+" request").WithCause(err)
	}
	return h.Do(ctx, httpx.Request{
		Method:   http.MethodPost,
		URL:      strings.TrimRight(baseURL, "/") + endpoint,
		Header:   header,
		Body:     payload,
		Retry:    false,
		Endpoint: endpoint,
	})
}
