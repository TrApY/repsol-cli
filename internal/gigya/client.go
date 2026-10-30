// Package gigya implements the minimal SAP Customer Data Cloud (Gigya) client
// that the CLI needs to sign into Repsol: device bootstrap, password login,
// signature refresh, and the import of a session captured from a browser HAR
// file or pasted from the developer tools.
//
// A Gigya session is the pair (login token, gmid): a login token is bound to
// the device cookie gmid, so every authenticated call carries both.
package gigya

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/httpx"
)

const (
	// APIKey is Repsol's public Gigya API key.
	APIKey = "3_kaQ1PkNv-0n4Nho4cD_Yz3-v6vbjqFJA6RgdpgTEBtw-2z6Rlnxe3IWDIDBtaETi"
	// DefaultBaseURL is Gigya's European data centre.
	DefaultBaseURL = "https://accounts.eu1.gigya.com"
	// PageURL is the Repsol page the Gigya calls are attributed to.
	PageURL = "https://pidetubombona.repsol.es/"
)

// Session is the pair of values a Gigya session is bound to: a login token
// and the device cookie gmid it was issued for.
type Session struct {
	LoginToken string
	GMID       string
}

// Signature holds the values Repsol's API expects in the uid, signature and
// signatureTimestamp headers.
type Signature struct {
	UID          string
	UIDSignature string
	Timestamp    string
	ObtainedAt   time.Time
}

// Account is the subset of the Gigya profile the CLI shows and stores.
type Account struct {
	UID       string
	Email     string
	FirstName string
}

// Client calls a Gigya data centre. BaseURL and APIKey default to
// DefaultBaseURL and APIKey when empty.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *httpx.Client
	Now     func() time.Time
}

// AccountInfo refreshes the signature of an existing session. It sends the
// login token and the gmid cookie it is bound to.
func (c *Client) AccountInfo(ctx context.Context, s Session) (Signature, Account, error) {
	const endpoint = "/accounts.getAccountInfo"
	form := url.Values{
		"APIKey":      {c.apiKey()},
		"login_token": {s.LoginToken},
		"format":      {"json"},
	}
	header := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}
	if s.GMID != "" {
		header.Set("Cookie", "gmid="+s.GMID)
	}
	resp, err := c.HTTP.Do(ctx, httpx.Request{
		Method:   http.MethodPost,
		URL:      c.baseURL() + endpoint,
		Header:   header,
		Body:     []byte(form.Encode()),
		Retry:    true,
		Endpoint: endpoint,
	})
	if err != nil {
		return Signature{}, Account{}, err
	}
	body, err := decodeResponse(endpoint, resp.Body)
	if err != nil {
		return Signature{}, Account{}, err
	}
	if body.ErrorCode != 0 {
		return Signature{}, Account{}, gigyaError(body.ErrorCode, body.ErrorMessage, callAccountInfo)
	}
	return c.signature(body), c.account(body), nil
}

// Bootstrap asks Gigya for a fresh anonymous device. It returns the gmid
// cookie Gigya sets, which a later password login must reuse.
func (c *Client) Bootstrap(ctx context.Context) (gmid string, err error) {
	const endpoint = "/accounts.webSdkBootstrap"
	query := url.Values{
		"apiKey":  {c.apiKey()},
		"pageURL": {PageURL},
		"sdk":     {"js_latest"},
		"format":  {"json"},
	}
	resp, err := c.HTTP.Do(ctx, httpx.Request{
		Method:   http.MethodGet,
		URL:      c.baseURL() + endpoint + "?" + query.Encode(),
		Header:   http.Header{},
		Retry:    true,
		Endpoint: endpoint,
	})
	if err != nil {
		return "", err
	}
	if len(bytes.TrimSpace(resp.Body)) > 0 {
		body, err := decodeResponse(endpoint, resp.Body)
		if err != nil {
			return "", err
		}
		if body.ErrorCode != 0 {
			return "", gigyaError(body.ErrorCode, body.ErrorMessage, callBootstrap)
		}
	}
	for _, cookie := range (&http.Response{Header: resp.Header}).Cookies() {
		if cookie.Name == "gmid" && cookie.Value != "" {
			return cookie.Value, nil
		}
	}
	return "", &apperr.Error{
		Category: apperr.APIChanged,
		Code:     "gigya_error",
		Message:  "Gigya did not set a gmid cookie during bootstrap",
		Endpoint: endpoint,
	}
}

// Login exchanges an email and password for a new session. gmid must be the
// device cookie obtained from Bootstrap. A login is never retried because a
// retry could count as another failed attempt.
func (c *Client) Login(ctx context.Context, gmid, email, password string) (Session, Signature, Account, error) {
	const endpoint = "/accounts.login"
	form := url.Values{
		"loginID":           {email},
		"password":          {password},
		"APIKey":            {c.apiKey()},
		"sessionExpiration": {"0"},
		"targetEnv":         {"jssdk"},
		"include":           {"profile,data,emails,subscriptions,preferences,"},
		"includeUserInfo":   {"true"},
		"loginMode":         {"standard"},
		"lang":              {"es"},
		"sdk":               {"js_latest"},
		"authMode":          {"cookie"},
		"pageURL":           {PageURL},
		"format":            {"json"},
	}
	header := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}
	if gmid != "" {
		header.Set("Cookie", "gmid="+gmid)
	}
	resp, err := c.HTTP.Do(ctx, httpx.Request{
		Method:   http.MethodPost,
		URL:      c.baseURL() + endpoint,
		Header:   header,
		Body:     []byte(form.Encode()),
		Retry:    false,
		Endpoint: endpoint,
	})
	if err != nil {
		return Session{}, Signature{}, Account{}, err
	}
	body, err := decodeResponse(endpoint, resp.Body)
	if err != nil {
		return Session{}, Signature{}, Account{}, err
	}
	if body.ErrorCode != 0 {
		return Session{}, Signature{}, Account{}, gigyaError(body.ErrorCode, body.ErrorMessage, callLogin)
	}
	if body.SessionInfo.LoginToken == "" {
		return Session{}, Signature{}, Account{}, &apperr.Error{
			Category: apperr.APIChanged,
			Code:     "gigya_error",
			Message:  "Gigya login succeeded without a session token",
			Endpoint: endpoint,
		}
	}
	return Session{LoginToken: body.SessionInfo.LoginToken, GMID: gmid},
		c.signature(body), c.account(body), nil
}

func (c *Client) baseURL() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return DefaultBaseURL
}

func (c *Client) apiKey() string {
	if c.APIKey != "" {
		return c.APIKey
	}
	return APIKey
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) signature(body apiResponse) Signature {
	return Signature{
		UID:          body.UID,
		UIDSignature: body.UIDSignature,
		Timestamp:    string(body.SignatureTimestamp),
		ObtainedAt:   c.now(),
	}
}

func (c *Client) account(body apiResponse) Account {
	return Account{
		UID:       body.UID,
		Email:     body.Profile.Email,
		FirstName: body.Profile.FirstName,
	}
}

// apiResponse is the subset of Gigya's answers the CLI reads. It deliberately
// omits the data block (it holds the NIF) and every other personal field.
type apiResponse struct {
	ErrorCode          int            `json:"errorCode"`
	ErrorMessage       string         `json:"errorMessage"`
	UID                string         `json:"UID"`
	UIDSignature       string         `json:"UIDSignature"`
	SignatureTimestamp timestamp      `json:"signatureTimestamp"`
	Profile            apiProfile     `json:"profile"`
	SessionInfo        apiSessionInfo `json:"sessionInfo"`
}

type apiProfile struct {
	Email     string `json:"email"`
	FirstName string `json:"firstName"`
}

type apiSessionInfo struct {
	LoginToken string `json:"login_token"`
}

// timestamp decodes signatureTimestamp, which Gigya returns as either a JSON
// string or a JSON number.
type timestamp string

func (ts *timestamp) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*ts = ""
		return nil
	}
	if data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*ts = timestamp(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*ts = timestamp(n.String())
	return nil
}

// decodeResponse parses a Gigya answer. The raw body is never included in the
// returned error: it may hold personal data.
func decodeResponse(endpoint string, data []byte) (apiResponse, error) {
	var body apiResponse
	if err := json.Unmarshal(data, &body); err != nil {
		return apiResponse{}, &apperr.Error{
			Category: apperr.APIChanged,
			Code:     "gigya_invalid_response",
			Message:  "Gigya returned a response that is not valid JSON",
			Endpoint: endpoint,
		}
	}
	return body, nil
}
