package gigya

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// SessionFromHAR extracts the newest Gigya session from a browser HAR export.
//
// Only Gigya entries are considered. A login token is read from the request's
// login_token parameter or from the response's sessionInfo.login_token; a gmid
// comes from the request cookies or its Cookie header. The last entry in file
// order that carries both wins. No other field of the HAR is read or kept.
func SessionFromHAR(r io.Reader) (Session, error) {
	if r == nil {
		return Session{}, &apperr.Error{
			Category: apperr.Usage,
			Code:     "har_invalid",
			Message:  "the HAR file is missing",
		}
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return Session{}, apperr.New(apperr.Usage, "har_invalid",
			"the HAR file could not be read").WithCause(err)
	}
	var har harFile
	if err := json.Unmarshal(data, &har); err != nil {
		return Session{}, &apperr.Error{
			Category: apperr.Usage,
			Code:     "har_invalid",
			Message:  "the HAR file is not valid JSON",
		}
	}

	var (
		found    Session
		sawToken bool
	)
	for _, entry := range har.Log.Entries {
		if !isGigyaURL(entry.Request.URL) {
			continue
		}
		token := harLoginToken(entry)
		gmid := harGMID(entry)
		if token != "" {
			sawToken = true
		}
		if token != "" && gmid != "" {
			found = Session{LoginToken: token, GMID: gmid}
		}
	}
	if found.LoginToken != "" && found.GMID != "" {
		return found, nil
	}
	if sawToken {
		return Session{}, &apperr.Error{
			Category: apperr.Usage,
			Code:     "har_no_gmid",
			Message:  "the HAR file has a Gigya login token but no gmid cookie",
			Hint:     "export the HAR with sensitive data (Chrome DevTools → Network → Export HAR (with sensitive data))",
		}
	}
	return Session{}, &apperr.Error{
		Category: apperr.Usage,
		Code:     "har_no_session",
		Message:  "the HAR file has no Gigya login token",
	}
}

// ParseSessionLine parses a value pasted from the browser developer tools,
// such as a whole Cookie header or a single document.cookie string. Both
// login_token and gmid must be present; a trailing CRLF and inner '=' signs
// are preserved exactly.
func ParseSessionLine(line string) (Session, error) {
	var session Session
	for _, part := range strings.Split(strings.TrimSpace(line), ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "login_token", "glt_" + APIKey:
			session.LoginToken = value
		case "gmid":
			session.GMID = value
		}
	}
	if session.LoginToken == "" || session.GMID == "" {
		return Session{}, &apperr.Error{
			Category: apperr.Usage,
			Code:     "invalid_session_line",
			Message:  "the session line needs both a login token and a gmid value",
		}
	}
	return session, nil
}

// harFile is the minimal shape of a HAR export this package decodes.
type harFile struct {
	Log struct {
		Entries []harEntry `json:"entries"`
	} `json:"log"`
}

type harEntry struct {
	Request  harRequest  `json:"request"`
	Response harResponse `json:"response"`
}

type harRequest struct {
	URL      string         `json:"url"`
	Cookies  []harNameValue `json:"cookies"`
	Headers  []harNameValue `json:"headers"`
	PostData harPostData    `json:"postData"`
}

type harPostData struct {
	Params []harNameValue `json:"params"`
	Text   string         `json:"text"`
}

type harResponse struct {
	Content struct {
		Text string `json:"text"`
	} `json:"content"`
}

type harNameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// isGigyaURL reports whether the URL belongs to a Gigya host.
func isGigyaURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "gigya.com" || strings.HasSuffix(host, ".gigya.com")
}

// harLoginToken reads a login token from the request parameters or form text,
// falling back to the response's sessionInfo.
func harLoginToken(entry harEntry) string {
	for _, param := range entry.Request.PostData.Params {
		if param.Name == "login_token" && param.Value != "" {
			return param.Value
		}
	}
	if entry.Request.PostData.Text != "" {
		if form, err := url.ParseQuery(entry.Request.PostData.Text); err == nil {
			if token := form.Get("login_token"); token != "" {
				return token
			}
		}
	}
	if entry.Response.Content.Text != "" {
		var body struct {
			SessionInfo struct {
				LoginToken string `json:"login_token"`
			} `json:"sessionInfo"`
		}
		if err := json.Unmarshal([]byte(entry.Response.Content.Text), &body); err == nil {
			return body.SessionInfo.LoginToken
		}
	}
	return ""
}

// harGMID reads the gmid device cookie from the request cookies or its Cookie
// header.
func harGMID(entry harEntry) string {
	for _, cookie := range entry.Request.Cookies {
		if cookie.Name == "gmid" && cookie.Value != "" {
			return cookie.Value
		}
	}
	for _, header := range entry.Request.Headers {
		if !strings.EqualFold(header.Name, "Cookie") {
			continue
		}
		req := &http.Request{Header: http.Header{"Cookie": []string{header.Value}}}
		for _, cookie := range req.Cookies() {
			if cookie.Name == "gmid" && cookie.Value != "" {
				return cookie.Value
			}
		}
	}
	return ""
}
