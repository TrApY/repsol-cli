package app

import (
	"context"
	"strings"

	"github.com/felipe9962/repsol-cli/internal/config"
	"github.com/felipe9962/repsol-cli/internal/gigya"
)

// AuthStatus is the state of the stored session of one profile.
type AuthStatus struct {
	// Profile is the profile the status describes.
	Profile string `json:"profile"`
	// LoggedIn reports whether a session (stored or from the environment) is
	// available.
	LoggedIn bool `json:"logged_in"`
	// Source is "stored", "env", or empty when there is no session.
	Source string `json:"source"`
	// Email is the account email the CLI knows.
	Email string `json:"email,omitempty"`
	// FirstName is the account first name the CLI knows.
	FirstName string `json:"first_name,omitempty"`
	// PasswordStored reports whether automatic re-login is possible.
	PasswordStored bool `json:"password_stored"`
	// AddressSet reports whether the profile has a stored delivery address.
	AddressSet bool `json:"address_set"`
	// Checked reports whether the session was validated with Gigya.
	Checked bool `json:"checked"`
	// SessionValid is the result of the check, or nil when none was done.
	SessionValid *bool `json:"session_valid,omitempty"`
}

// LogoutResult reports the effect of Logout.
type LogoutResult struct {
	// Profile is the profile whose credentials were removed.
	Profile string `json:"profile"`
	// Removed reports whether there were credentials to remove.
	Removed bool `json:"removed"`
}

// ImportSession validates an imported session with one signature refresh and
// stores it. A stored password survives only when it belongs to the imported
// account, because a password from another account would log the wrong user
// in.
func (a *App) ImportSession(ctx context.Context, session gigya.Session) (AuthStatus, error) {
	sig, account, err := a.gigya.AccountInfo(ctx, session)
	if err != nil {
		return AuthStatus{}, err
	}

	err = a.store.UpdateCredentials(a.profile, func(c *config.Credentials) error {
		if !strings.EqualFold(strings.TrimSpace(c.Email), strings.TrimSpace(account.Email)) {
			c.Password = ""
		}
		c.LoginToken = session.LoginToken
		c.GMID = session.GMID
		c.UID = account.UID
		c.FirstName = account.FirstName
		c.Email = account.Email
		c.UpdatedAt = a.now()
		return nil
	})
	if err != nil {
		return AuthStatus{}, err
	}

	a.session.cache(sig)
	if err := a.ensureDefaultProfile(); err != nil {
		return AuthStatus{}, err
	}
	return a.Status(ctx, false)
}

// PasswordLogin bootstraps a device, logs in with the email and password, and
// stores the resulting session together with the password, so the CLI can log
// in again on its own.
func (a *App) PasswordLogin(ctx context.Context, email, password string) (AuthStatus, error) {
	gmid, err := a.gigya.Bootstrap(ctx)
	if err != nil {
		return AuthStatus{}, err
	}

	session, sig, account, err := a.gigya.Login(ctx, gmid, email, password)
	if err != nil {
		return AuthStatus{}, err
	}

	accountEmail := account.Email
	if accountEmail == "" {
		accountEmail = email
	}
	err = a.store.UpdateCredentials(a.profile, func(c *config.Credentials) error {
		c.LoginToken = session.LoginToken
		c.GMID = session.GMID
		c.Email = accountEmail
		c.Password = password
		c.UID = account.UID
		c.FirstName = account.FirstName
		c.UpdatedAt = a.now()
		return nil
	})
	if err != nil {
		return AuthStatus{}, err
	}

	a.session.cache(sig)
	if err := a.ensureDefaultProfile(); err != nil {
		return AuthStatus{}, err
	}
	return a.Status(ctx, false)
}

// Status reports the stored session. With check set it also proves the session
// still works, which may refresh the signature or log in again; a session that
// cannot be signed is returned as an auth error.
func (a *App) Status(ctx context.Context, check bool) (AuthStatus, error) {
	stored, found, err := a.storedCredentials()
	if err != nil {
		return AuthStatus{}, err
	}
	cfg, err := a.store.LoadConfig()
	if err != nil {
		return AuthStatus{}, err
	}

	status := AuthStatus{Profile: a.profile}
	if found {
		status.Email = stored.Email
		status.FirstName = stored.FirstName
		status.PasswordStored = stored.Password != ""
	}
	switch {
	case a.env.LoginToken != "":
		status.Source = "env"
		status.LoggedIn = true
		if a.env.Email != "" {
			status.Email = a.env.Email
		}
	case found && stored.LoginToken != "":
		status.Source = "stored"
		status.LoggedIn = true
	}
	status.AddressSet = cfg.Profiles[a.profile].Address != nil

	if check {
		status.Checked = true
		if _, err := a.session.Sign(ctx); err != nil {
			return AuthStatus{}, err
		}
		valid := true
		status.SessionValid = &valid
	}
	return status, nil
}

// Logout deletes the stored credentials of the profile and forgets the cached
// signature. Other profiles are left untouched.
func (a *App) Logout() (LogoutResult, error) {
	removed, err := a.store.DeleteCredentials(a.profile)
	if err != nil {
		return LogoutResult{}, err
	}
	a.session.clear()
	return LogoutResult{Profile: a.profile, Removed: removed}, nil
}
