// Package app implements the use cases shared by the CLI and the MCP server:
// session signing with automatic re-login, the auth commands, and the domain
// operations built on the Repsol API.
package app

import (
	"context"
	"time"

	"github.com/felipe9962/repsol-cli/internal/config"
	"github.com/felipe9962/repsol-cli/internal/gigya"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// Env holds the session and login values read from the REPSOL_* environment
// variables. They are never persisted.
type Env struct {
	// LoginToken is REPSOL_LOGIN_TOKEN.
	LoginToken string
	// GMID is REPSOL_GMID, the device cookie the login token is bound to.
	GMID string
	// Email is REPSOL_EMAIL, used for automatic re-login.
	Email string
	// Password is REPSOL_PASSWORD, used for automatic re-login.
	Password string
}

// Deps are the dependencies of an App. API, Gigya, Store and Profile are
// required; Places is required by the address use cases; Now defaults to
// time.Now.
type Deps struct {
	// API talks to the Repsol API. New installs the app's signer on it.
	API *repsolapi.Client
	// Places talks to the address service.
	Places *repsolapi.PlacesClient
	// Gigya validates sessions and performs password logins.
	Gigya *gigya.Client
	// Store reads and writes config.toml and credentials.toml.
	Store config.Store
	// Profile is the profile the app works on.
	Profile string
	// Env carries the REPSOL_* environment values.
	Env Env
	// Now returns the current time. It is the clock the signature cache ages
	// against, and the one a payment wait counts its deadline on.
	Now func() time.Time
	// Sleep pauses between two lookups of a payment wait. It returns early
	// with the context's error when the context ends. It defaults to a timer.
	Sleep func(ctx context.Context, d time.Duration) error
}

// App is the entry point of every use case. Create it with New.
type App struct {
	api     *repsolapi.Client
	places  *repsolapi.PlacesClient
	gigya   *gigya.Client
	store   config.Store
	profile string
	env     Env
	now     func() time.Time
	sleep   func(ctx context.Context, d time.Duration) error
	session *Session
}

// New builds an App and makes its session the signer of the API client.
func New(d Deps) *App {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	sleep := d.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	profile := d.Profile
	if profile == "" {
		profile = "default"
	}

	a := &App{
		api:     d.API,
		places:  d.Places,
		gigya:   d.Gigya,
		store:   d.Store,
		profile: profile,
		env:     d.Env,
		now:     now,
		sleep:   sleep,
	}
	a.session = &Session{app: a}
	if a.api != nil {
		a.api.Signer = a.session
	}
	return a
}

// storedCredentials loads the credentials of the app's profile. found is false
// when the profile has no entry yet.
func (a *App) storedCredentials() (config.Credentials, bool, error) {
	return a.store.LoadCredentials(a.profile)
}

// sessionSource returns the credentials Sign works from, and whether they come
// from the environment. An environment token without its gmid is a usage
// error, because a login token alone is rejected.
func (a *App) sessionSource() (config.Credentials, bool, error) {
	stored, _, err := a.store.LoadCredentials(a.profile)
	if err != nil {
		return config.Credentials{}, false, err
	}
	if a.env.LoginToken == "" {
		return stored, false, nil
	}
	if a.env.GMID == "" {
		return config.Credentials{}, false, missingGMID()
	}
	stored.LoginToken = a.env.LoginToken
	stored.GMID = a.env.GMID
	return stored, true, nil
}

// loginCredentials returns the email and password a login may use: the
// environment pair when both are set, otherwise the stored pair.
func (a *App) loginCredentials(stored config.Credentials) (email, password string) {
	if a.env.Email != "" && a.env.Password != "" {
		return a.env.Email, a.env.Password
	}
	return stored.Email, stored.Password
}

// ensureDefaultProfile records the profile as the default one when no default
// is configured yet.
func (a *App) ensureDefaultProfile() error {
	return a.store.UpdateConfig(func(cfg *config.Config) error {
		if cfg.DefaultProfile == "" {
			cfg.DefaultProfile = a.profile
		}
		return nil
	})
}

// sleepContext waits for d, or returns the context's error when it ends first.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
