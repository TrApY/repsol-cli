package gigya

import (
	"strconv"
	"strings"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// The machine codes of the failures Gigya can cause.
const (
	CodeSessionRejected    = "session_rejected"
	CodeInvalidCredentials = "invalid_credentials"
	CodeCaptchaRequired    = "captcha_required"
	CodeAccountLocked      = "account_locked"
)

// call identifies which Gigya call failed, because the same errorCode means
// "the session is rejected" for a refresh and "the login failed" for a login.
type call int

const (
	callAccountInfo call = iota
	callBootstrap
	callLogin
)

// gigyaError classifies a non-zero Gigya errorCode into a typed error. The
// message names the errorCode and Gigya's errorMessage, never the request.
func gigyaError(errorCode int, errorMessage string, which call) *apperr.Error {
	detail := "Gigya errorCode " + strconv.Itoa(errorCode)
	if msg := strings.TrimSpace(errorMessage); msg != "" {
		detail += ": " + msg
	}
	switch {
	case errorCode == 403042:
		return apperr.New(apperr.Auth, CodeInvalidCredentials,
			"Repsol rejected the email or password ("+detail+")")
	case errorCode == 401020 || errorCode == 401021:
		return apperr.New(apperr.Auth, CodeCaptchaRequired,
			"Repsol asks for a captcha or extra verification ("+detail+")").
			WithHint(`log in on the web and run "repsol auth login --har FILE"`)
	case errorCode == 403120:
		return apperr.New(apperr.Auth, CodeAccountLocked,
			"Repsol locked the account ("+detail+")")
	case errorCode >= 500000 && errorCode <= 599999:
		return &apperr.Error{
			Category:  apperr.Network,
			Code:      "gigya_unavailable",
			Message:   "Gigya is unavailable (" + detail + ")",
			Retryable: true,
		}
	case errorCode >= 400000 && errorCode <= 400999:
		return apperr.New(apperr.APIChanged, "gigya_bad_request",
			"Gigya rejected the request as malformed ("+detail+")")
	case errorCode >= 401000 && errorCode <= 401999,
		errorCode >= 403000 && errorCode <= 403999:
		code, message := CodeSessionRejected, "Gigya rejected the session"
		if which == callLogin {
			code, message = "login_failed", "Gigya rejected the login"
		}
		return apperr.New(apperr.Auth, code, message+" ("+detail+")")
	case errorCode >= 200000 && errorCode <= 299999:
		return apperr.New(apperr.Auth, "account_pending",
			"Repsol account is pending verification ("+detail+")")
	default:
		return apperr.New(apperr.APIChanged, "gigya_error",
			"Gigya returned an unexpected error ("+detail+")")
	}
}
