// Package repsolapi implements the public and authenticated clients of the
// Repsol API and of its address service.
package repsolapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/httpx"
)

const (
	// DefaultBaseURL is the default base URL of the Repsol API.
	DefaultBaseURL = "https://pidetubombona.repsol.es/api"
	// DefaultPlacesBaseURL is the default base URL of the address service.
	DefaultPlacesBaseURL = "https://pro-repsol.ed-integrations.com"
)

// Client talks to the Repsol API. The zero value is not usable; BaseURL and
// HTTP must be set.
type Client struct {
	// BaseURL is the API base URL, without a trailing slash.
	BaseURL string
	// HTTP is the shared transport.
	HTTP *httpx.Client
	// Signer signs authenticated calls. A nil Signer makes them fail with an
	// auth error before any request is sent.
	Signer Signer
	// Telemetry carries the identifiers of the current order flow. A nil
	// Telemetry leaves the ptb-* headers off writes.
	Telemetry *Telemetry
}

// queryParam is one query string parameter. The slice order is the order the
// parameters are written to the URL.
type queryParam struct {
	key   string
	value string
}

// param builds a query parameter.
func param(key, value string) queryParam {
	return queryParam{key: key, value: value}
}

// encodeQuery renders the parameters in order and leaves out every parameter
// with an empty value.
func encodeQuery(params []queryParam) string {
	var b strings.Builder
	for _, p := range params {
		if p.value == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(p.key))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(p.value))
	}
	return b.String()
}

// doGET performs a retryable GET against baseURL+endpoint. endpoint is also
// the path reported in errors.
func doGET(ctx context.Context, h *httpx.Client, baseURL, endpoint string, params ...queryParam) (*httpx.Response, error) {
	target := strings.TrimRight(baseURL, "/") + endpoint
	if qs := encodeQuery(params); qs != "" {
		target += "?" + qs
	}
	return h.Do(ctx, httpx.Request{
		Method:   http.MethodGet,
		URL:      target,
		Retry:    true,
		Endpoint: endpoint,
	})
}

// apiErrorBody is the error envelope some endpoints return with HTTP 200.
type apiErrorBody struct {
	Type      string `json:"type"`
	ErrorCode string `json:"errorCode"`
	Error     bool   `json:"error"`
	Message   string `json:"message"`
}

// decodeBody decodes a successful response body into T. It is the single
// decoder for both clients, so every call reports refusals and shape changes
// the same way.
func decodeBody[T any](resp *httpx.Response, endpoint string) (T, error) {
	var out T
	body := bytes.TrimSpace(resp.Body)

	var envelope apiErrorBody
	if len(body) > 0 && body[0] == '{' && json.Unmarshal(body, &envelope) == nil && envelope.Error {
		msg := envelope.Message
		if msg == "" {
			msg = "the API refused the request"
		}
		if envelope.ErrorCode != "" {
			msg += " (" + envelope.ErrorCode + ")"
		}
		return out, apiError(apperr.Refused, "api_error", msg, resp, endpoint)
	}

	if err := json.Unmarshal(body, &out); err != nil {
		e := apiError(apperr.APIChanged, "unexpected_response",
			"unexpected response from "+endpoint, resp, endpoint)
		return out, e.WithCause(err)
	}
	return out, nil
}

// apiError builds a typed error carrying the HTTP context of the call.
func apiError(cat apperr.Category, code, msg string, resp *httpx.Response, endpoint string) *apperr.Error {
	e := apperr.New(cat, code, msg)
	e.Endpoint = endpoint
	if resp != nil {
		e.HTTPStatus = resp.Status
	}
	return e
}
