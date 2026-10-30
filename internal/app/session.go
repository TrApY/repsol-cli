package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/config"
	"github.com/felipe9962/repsol-cli/internal/gigya"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// SignatureMaxAge is how long an obtained signature is reused before it is
// refreshed. The web application refreshes it every 50 seconds.
const SignatureMaxAge = 50 * time.Second

// The auth hints and messages. They never carry a token, a gmid or a password.
const (
	loginHint   = `run "repsol auth login --har FILE" or "repsol auth login --email ADDRESS"`
	reloginHint = loginHint + `; log in with --email to enable automatic re-login`
)

// missingGMID is the usage error for REPSOL_LOGIN_TOKEN without REPSOL_GMID: a
// login token is bound to the device cookie and is rejected on its own.
func missingGMID() *apperr.Error {
	return apperr.New(apperr.Usage, "missing_gmid",
		"REPSOL_LOGIN_TOKEN is set without REPSOL_GMID").WithHint("set REPSOL_GMID as well")
}

// expiredSession is the auth error for a rejected token when no password is
// available to log in again.
func expiredSession() *apperr.Error {
	return apperr.New(apperr.Auth, "session_expired",
		"the Repsol session has expired").WithHint(reloginHint)
}

// Session signs the authenticated Repsol calls. It caches the signature Gigya
// issued and re-logs in once when the stored session is rejected and a
// password is available. A mutex serialises the cache and the re-login,
// because the MCP server runs tool calls concurrently.
type Session struct {
	app *App

	mu     sync.Mutex
	sig    gigya.Signature
	signed bool
}

// Sign implements repsolapi.Signer. The rules, in order:
//
//  1. a cached signature younger than SignatureMaxAge is returned as is;
//  2. the session comes from the environment when it carries a login token,
//     otherwise from the stored credentials of the profile;
//  3. no token and no password is not_logged_in;
//  4. a rejected token is retried with exactly one password login when a
//     password is available, and is session_expired otherwise;
//  5. the login reuses the session gmid, bootstraps a new one when it has
//     none, and saves the new token and gmid unless the session came from the
//     environment.
func (s *Session) Sign(ctx context.Context) (repsolapi.Auth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.signed && s.app.now().Sub(s.sig.ObtainedAt) < SignatureMaxAge {
		return authFromSignature(s.sig), nil
	}

	creds, fromEnv, err := s.app.sessionSource()
	if err != nil {
		return repsolapi.Auth{}, err
	}
	email, password := s.app.loginCredentials(creds)

	if creds.LoginToken == "" && password == "" {
		return repsolapi.Auth{}, apperr.New(apperr.Auth, "not_logged_in",
			"no Repsol session is stored").WithHint(loginHint)
	}

	if creds.LoginToken != "" {
		sig, _, err := s.app.gigya.AccountInfo(ctx, gigya.Session{
			LoginToken: creds.LoginToken,
			GMID:       creds.GMID,
		})
		switch {
		case err == nil:
			s.store(sig)
			return authFromSignature(sig), nil
		case isErrorCode(err, gigya.CodeSessionRejected) && password != "":
			// The stale token is replaced by exactly one password login below.
		case isErrorCode(err, gigya.CodeSessionRejected):
			return repsolapi.Auth{}, expiredSession()
		default:
			return repsolapi.Auth{}, err
		}
	}

	return s.login(ctx, creds, fromEnv, email, password)
}

// login performs exactly one password login. It never retries: a retry could
// count as another failed attempt. The caller holds the mutex.
func (s *Session) login(
	ctx context.Context,
	creds config.Credentials,
	fromEnv bool,
	email, password string,
) (repsolapi.Auth, error) {
	gmid := creds.GMID
	if gmid == "" {
		fresh, err := s.app.gigya.Bootstrap(ctx)
		if err != nil {
			return repsolapi.Auth{}, err
		}
		gmid = fresh
	}

	session, sig, _, err := s.app.gigya.Login(ctx, gmid, email, password)
	if err != nil {
		return repsolapi.Auth{}, err
	}

	if !fromEnv {
		if err := s.app.store.UpdateCredentials(s.app.profile, func(c *config.Credentials) error {
			c.LoginToken = session.LoginToken
			c.GMID = session.GMID
			c.UpdatedAt = s.app.now()
			return nil
		}); err != nil {
			return repsolapi.Auth{}, err
		}
	}

	s.store(sig)
	return authFromSignature(sig), nil
}

// store records sig as the current signature. The caller holds the mutex.
func (s *Session) store(sig gigya.Signature) {
	s.sig = sig
	s.signed = true
}

// cache records a signature obtained outside Sign, such as the one an import
// or a password login validated the session with.
func (s *Session) cache(sig gigya.Signature) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store(sig)
}

// clear forgets the cached signature, so a logout cannot keep signing with it.
func (s *Session) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sig = gigya.Signature{}
	s.signed = false
}

// authFromSignature converts a Gigya signature into the triple the Repsol API
// expects in its headers.
func authFromSignature(sig gigya.Signature) repsolapi.Auth {
	return repsolapi.Auth{
		UID:       sig.UID,
		Signature: sig.UIDSignature,
		Timestamp: sig.Timestamp,
	}
}

// isErrorCode reports whether err is a typed error with the given code.
func isErrorCode(err error, code string) bool {
	var typed *apperr.Error
	return errors.As(err, &typed) && typed.Code == code
}
