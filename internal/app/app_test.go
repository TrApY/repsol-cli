package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/config"
	"github.com/felipe9962/repsol-cli/internal/fakerepsol"
	"github.com/felipe9962/repsol-cli/internal/gigya"
	"github.com/felipe9962/repsol-cli/internal/httpx"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// defaultTestProfile is the profile every test works on.
const defaultTestProfile = "default"

// testClock is a manually advanced clock. It keeps the signature cache and the
// fake's notion of today deterministic.
type testClock struct{ t time.Time }

// Now returns the current test time.
func (c *testClock) Now() time.Time { return c.t }

// Advance moves the clock forward.
func (c *testClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// newTestApp wires every client to a fresh fakerepsol server. The transport
// uses the test user agent with zero backoff, the store lives under t.TempDir,
// the profile is "default", and the clock starts at 2026-11-01T10:00:00Z.
func newTestApp(t *testing.T, opts ...func(*Deps)) (*App, *fakerepsol.Server, config.Store, *testClock) {
	t.Helper()

	fake := fakerepsol.New()
	t.Cleanup(fake.Close)
	fake.SetToday(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))

	clock := &testClock{t: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)}

	transport := httpx.New("repsol-cli/test", nil)
	transport.Backoff = func(int) time.Duration { return 0 }

	dir := t.TempDir()
	store := config.Store{Paths: config.Paths{
		Config:      filepath.Join(dir, "config.toml"),
		Credentials: filepath.Join(dir, "credentials.toml"),
		Lock:        filepath.Join(dir, "credentials.toml.lock"),
	}}

	deps := Deps{
		API:     &repsolapi.Client{BaseURL: fake.APIURL(), HTTP: transport},
		Places:  &repsolapi.PlacesClient{BaseURL: fake.PlacesURL(), HTTP: transport},
		Gigya:   &gigya.Client{BaseURL: fake.GigyaURL(), HTTP: transport, Now: clock.Now},
		Store:   store,
		Profile: defaultTestProfile,
		Now:     clock.Now,
	}
	for _, opt := range opts {
		opt(&deps)
	}
	return New(deps), fake, deps.Store, clock
}

// storeSession writes c into the default profile's credentials.
func storeSession(t *testing.T, st config.Store, c config.Credentials) {
	t.Helper()
	storeProfile(t, st, defaultTestProfile, c)
}

// storeProfile writes c into one profile's credentials.
func storeProfile(t *testing.T, st config.Store, profile string, c config.Credentials) {
	t.Helper()
	if err := st.UpdateCredentials(profile, func(cred *config.Credentials) error {
		*cred = c
		return nil
	}); err != nil {
		t.Fatalf("storing credentials for profile %q: %v", profile, err)
	}
}

// requireCredentials returns the stored credentials of a profile and fails the
// test when the profile is missing.
func requireCredentials(t *testing.T, st config.Store, profile string) config.Credentials {
	t.Helper()
	cred, found, err := st.LoadCredentials(profile)
	if err != nil {
		t.Fatalf("loading credentials for profile %q: %v", profile, err)
	}
	if !found {
		t.Fatalf("no credentials stored for profile %q", profile)
	}
	return cred
}

// requireErrorCode asserts that err is an *apperr.Error with the given code and
// returns it.
func requireErrorCode(t *testing.T, err error, code string) *apperr.Error {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %q, got nil", code)
	}
	var typed *apperr.Error
	if !errors.As(err, &typed) {
		t.Fatalf("expected *apperr.Error with code %q, got %T: %v", code, err, err)
	}
	if typed.Code != code {
		t.Fatalf("expected error code %q, got %q (%v)", code, typed.Code, err)
	}
	return typed
}

// requireNoCredentialsFile asserts that credentials.toml was never written.
func requireNoCredentialsFile(t *testing.T, st config.Store) {
	t.Helper()
	if _, err := os.Stat(st.Paths.Credentials); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected no credentials file, stat error = %v", err)
	}
}
