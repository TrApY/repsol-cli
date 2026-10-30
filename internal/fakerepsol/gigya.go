package fakerepsol

import (
	"net/http"
)

// The Gigya error codes the fake answers with. Gigya always answers HTTP 200;
// success or failure is in the body.
const (
	errorSessionRejected = 403005
	errorCaptchaRequired = 401020
	errorInvalidPassword = 403042
)

// handleAccountInfo refreshes the signature of a session. The login token is
// bound to the gmid cookie and to the API key, so a request without either is
// rejected.
func (s *Server) handleAccountInfo(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeGigyaError(w, errorSessionRejected, "Unauthorized user")
		return
	}
	gmid, cookieErr := r.Cookie("gmid")
	token := r.PostForm.Get("login_token")

	s.mu.Lock()
	rejected := s.rejected[token]
	s.mu.Unlock()

	knownToken := token == ValidToken || token == RenewedToken
	if cookieErr != nil || gmid.Value == "" || r.PostForm.Get("APIKey") == "" || !knownToken || rejected {
		writeGigyaError(w, errorSessionRejected, "Unauthorized user")
		return
	}
	writeJSON(w, http.StatusOK, gigyaAccount())
}

// handleBootstrap hands out a fresh anonymous device cookie.
func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "gmid", Value: BootGMID, Path: "/"})
	writeJSON(w, http.StatusOK, map[string]any{"errorCode": 0})
}

// handleLogin exchanges the synthetic credentials for a renewed session. The
// device cookie is required, as the real login is bound to it.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeGigyaError(w, errorSessionRejected, "Unauthorized user")
		return
	}
	gmid, cookieErr := r.Cookie("gmid")
	if cookieErr != nil || gmid.Value == "" {
		writeGigyaError(w, errorSessionRejected, "Unauthorized user")
		return
	}

	switch {
	case r.PostForm.Get("loginID") == CaptchaEmail:
		writeGigyaError(w, errorCaptchaRequired, "Captcha required")
	case r.PostForm.Get("loginID") == Email && r.PostForm.Get("password") == Password:
		body := gigyaAccount()
		body["sessionInfo"] = map[string]any{
			"login_token": RenewedToken,
			"expires_in":  "0",
		}
		writeJSON(w, http.StatusOK, body)
	default:
		writeGigyaError(w, errorInvalidPassword, "Invalid loginID or password")
	}
}

// gigyaAccount is the successful identity block shared by account info and
// login.
func gigyaAccount() map[string]any {
	return map[string]any{
		"errorCode":          0,
		"UID":                UID,
		"UIDSignature":       Signature,
		"signatureTimestamp": "1700000000",
		"profile": map[string]any{
			"email":     Email,
			"firstName": FirstName,
		},
	}
}

// writeGigyaError writes a Gigya failure. Gigya reports failures as HTTP 200.
func writeGigyaError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, http.StatusOK, map[string]any{
		"errorCode":    code,
		"errorMessage": message,
	})
}
