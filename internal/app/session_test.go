package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/config"
	"github.com/felipe9962/repsol-cli/internal/fakerepsol"
	"github.com/felipe9962/repsol-cli/internal/gigya"
)

func TestImportSessionStoresSession(t *testing.T) {
	a, fake, st, _ := newTestApp(t)
	ctx := context.Background()

	status, err := a.ImportSession(ctx, gigya.Session{
		LoginToken: fakerepsol.ValidToken,
		GMID:       fakerepsol.ValidGMID,
	})
	if err != nil {
		t.Fatalf("ImportSession: %v", err)
	}
	if !status.LoggedIn {
		t.Error("status.LoggedIn = false, want true")
	}
	if status.Source != "stored" {
		t.Errorf("status.Source = %q, want %q", status.Source, "stored")
	}
	if status.Email != fakerepsol.Email {
		t.Errorf("status.Email = %q, want %q", status.Email, fakerepsol.Email)
	}
	if status.FirstName != fakerepsol.FirstName {
		t.Errorf("status.FirstName = %q, want %q", status.FirstName, fakerepsol.FirstName)
	}
	if status.AddressSet {
		t.Error("status.AddressSet = true, want false")
	}
	if got := fake.Count("/gigya/accounts.getAccountInfo"); got != 1 {
		t.Errorf("getAccountInfo calls = %d, want 1", got)
	}

	cred := requireCredentials(t, st, defaultTestProfile)
	if cred.LoginToken != fakerepsol.ValidToken {
		t.Errorf("stored login token = %q, want the imported one", cred.LoginToken)
	}
	if cred.GMID != fakerepsol.ValidGMID {
		t.Errorf("stored gmid = %q, want the imported one", cred.GMID)
	}
	if cred.UID != fakerepsol.UID {
		t.Errorf("stored UID = %q, want %q", cred.UID, fakerepsol.UID)
	}
	if cred.FirstName != fakerepsol.FirstName {
		t.Errorf("stored first name = %q, want %q", cred.FirstName, fakerepsol.FirstName)
	}
	if cred.Email != fakerepsol.Email {
		t.Errorf("stored email = %q, want %q", cred.Email, fakerepsol.Email)
	}

	cfg, err := st.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DefaultProfile != defaultTestProfile {
		t.Errorf("DefaultProfile = %q, want %q", cfg.DefaultProfile, defaultTestProfile)
	}
}

func TestImportSessionRejected(t *testing.T) {
	a, fake, st, _ := newTestApp(t)
	fake.RejectToken(fakerepsol.ValidToken)

	_, err := a.ImportSession(context.Background(), gigya.Session{
		LoginToken: fakerepsol.ValidToken,
		GMID:       fakerepsol.ValidGMID,
	})
	typed := requireErrorCode(t, err, gigya.CodeSessionRejected)
	if typed.Category != apperr.Auth {
		t.Errorf("category = %v, want %v", typed.Category, apperr.Auth)
	}

	if _, found, err := st.LoadCredentials(defaultTestProfile); err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	} else if found {
		t.Error("a rejected session was stored")
	}
}

func TestImportKeepsPasswordOnlyForSameAccount(t *testing.T) {
	ctx := context.Background()
	session := gigya.Session{LoginToken: fakerepsol.ValidToken, GMID: fakerepsol.ValidGMID}

	t.Run("same account", func(t *testing.T) {
		a, _, st, _ := newTestApp(t)
		storeSession(t, st, config.Credentials{Email: fakerepsol.Email, Password: "pw"})

		if _, err := a.ImportSession(ctx, session); err != nil {
			t.Fatalf("ImportSession: %v", err)
		}
		if cred := requireCredentials(t, st, defaultTestProfile); cred.Password != "pw" {
			t.Errorf("stored password = %q, want it kept", cred.Password)
		}
	})

	t.Run("same account different case", func(t *testing.T) {
		a, _, st, _ := newTestApp(t)
		storeSession(t, st, config.Credentials{
			Email:    strings.ToUpper(fakerepsol.Email),
			Password: "pw",
		})

		if _, err := a.ImportSession(ctx, session); err != nil {
			t.Fatalf("ImportSession: %v", err)
		}
		if cred := requireCredentials(t, st, defaultTestProfile); cred.Password != "pw" {
			t.Errorf("stored password = %q, want it kept for the same account", cred.Password)
		}
	})

	t.Run("other account", func(t *testing.T) {
		a, _, st, _ := newTestApp(t)
		storeSession(t, st, config.Credentials{
			Email:    "other@example.com",
			Password: "pw",
		})

		if _, err := a.ImportSession(ctx, session); err != nil {
			t.Fatalf("ImportSession: %v", err)
		}
		cred := requireCredentials(t, st, defaultTestProfile)
		if cred.Password != "" {
			t.Errorf("stored password = %q, want it cleared", cred.Password)
		}
		if cred.Email != fakerepsol.Email {
			t.Errorf("stored email = %q, want %q", cred.Email, fakerepsol.Email)
		}
	})
}

func TestPasswordLogin(t *testing.T) {
	a, fake, st, _ := newTestApp(t)

	status, err := a.PasswordLogin(context.Background(), fakerepsol.Email, fakerepsol.Password)
	if err != nil {
		t.Fatalf("PasswordLogin: %v", err)
	}
	if !status.LoggedIn {
		t.Error("status.LoggedIn = false, want true")
	}
	if status.Source != "stored" {
		t.Errorf("status.Source = %q, want %q", status.Source, "stored")
	}
	if status.Email != fakerepsol.Email {
		t.Errorf("status.Email = %q, want %q", status.Email, fakerepsol.Email)
	}
	if !status.PasswordStored {
		t.Error("status.PasswordStored = false, want true")
	}

	cred := requireCredentials(t, st, defaultTestProfile)
	if cred.LoginToken != fakerepsol.RenewedToken {
		t.Errorf("stored login token = %q, want %q", cred.LoginToken, fakerepsol.RenewedToken)
	}
	if cred.GMID != fakerepsol.BootGMID {
		t.Errorf("stored gmid = %q, want %q", cred.GMID, fakerepsol.BootGMID)
	}
	if cred.Password != fakerepsol.Password {
		t.Errorf("stored password = %q, want %q", cred.Password, fakerepsol.Password)
	}
	if cred.Email != fakerepsol.Email {
		t.Errorf("stored email = %q, want %q", cred.Email, fakerepsol.Email)
	}

	if got := fake.Count("/gigya/accounts.webSdkBootstrap"); got != 1 {
		t.Errorf("bootstrap calls = %d, want 1", got)
	}
	if got := fake.Count("/gigya/accounts.login"); got != 1 {
		t.Errorf("login calls = %d, want 1", got)
	}
	logins := fake.Requests("/gigya/accounts.login")
	if len(logins) != 1 {
		t.Fatalf("login requests = %d, want 1", len(logins))
	}
	if cookie := logins[0].Header.Get("Cookie"); !strings.Contains(cookie, "gmid="+fakerepsol.BootGMID) {
		t.Errorf("login cookie = %q, want it to carry the bootstrap gmid", cookie)
	}
}

func TestSignCachesSignature(t *testing.T) {
	a, fake, st, clock := newTestApp(t)
	ctx := context.Background()
	storeSession(t, st, config.Credentials{
		LoginToken: fakerepsol.ValidToken,
		GMID:       fakerepsol.ValidGMID,
	})

	auth, err := a.session.Sign(ctx)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if auth.UID != fakerepsol.UID {
		t.Errorf("auth.UID = %q, want %q", auth.UID, fakerepsol.UID)
	}
	if auth.Signature != fakerepsol.Signature {
		t.Errorf("auth.Signature = %q, want %q", auth.Signature, fakerepsol.Signature)
	}
	if auth.Timestamp != "1700000000" {
		t.Errorf("auth.Timestamp = %q, want %q", auth.Timestamp, "1700000000")
	}
	if got := fake.Count("/gigya/accounts.getAccountInfo"); got != 1 {
		t.Fatalf("getAccountInfo calls = %d, want 1", got)
	}

	clock.Advance(49 * time.Second)
	if _, err := a.session.Sign(ctx); err != nil {
		t.Fatalf("Sign within the cache window: %v", err)
	}
	if got := fake.Count("/gigya/accounts.getAccountInfo"); got != 1 {
		t.Errorf("getAccountInfo calls = %d after 49s, want the cached signature", got)
	}

	clock.Advance(2 * time.Second)
	if _, err := a.session.Sign(ctx); err != nil {
		t.Fatalf("Sign after the cache window: %v", err)
	}
	if got := fake.Count("/gigya/accounts.getAccountInfo"); got != 2 {
		t.Errorf("getAccountInfo calls = %d after 51s, want 2", got)
	}
}

func TestAutoRelogin(t *testing.T) {
	a, fake, st, _ := newTestApp(t)
	ctx := context.Background()
	storeSession(t, st, config.Credentials{
		LoginToken: fakerepsol.ValidToken,
		GMID:       fakerepsol.ValidGMID,
		Email:      fakerepsol.Email,
		Password:   fakerepsol.Password,
	})
	fake.RejectToken(fakerepsol.ValidToken)

	auth, err := a.session.Sign(ctx)
	if err != nil {
		t.Fatalf("Sign with automatic re-login: %v", err)
	}
	if auth.UID != fakerepsol.UID {
		t.Errorf("auth.UID = %q, want %q", auth.UID, fakerepsol.UID)
	}

	cred := requireCredentials(t, st, defaultTestProfile)
	if cred.LoginToken != fakerepsol.RenewedToken {
		t.Errorf("stored login token = %q, want %q", cred.LoginToken, fakerepsol.RenewedToken)
	}
	if cred.GMID != fakerepsol.ValidGMID {
		t.Errorf("stored gmid = %q, want the session gmid %q", cred.GMID, fakerepsol.ValidGMID)
	}
	if got := fake.Count("/gigya/accounts.webSdkBootstrap"); got != 0 {
		t.Errorf("bootstrap calls = %d, want 0 (the session gmid is reused)", got)
	}
	if got := fake.Count("/gigya/accounts.login"); got != 1 {
		t.Errorf("login calls = %d, want exactly 1", got)
	}
	logins := fake.Requests("/gigya/accounts.login")
	if len(logins) != 1 {
		t.Fatalf("login requests = %d, want 1", len(logins))
	}
	if cookie := logins[0].Header.Get("Cookie"); !strings.Contains(cookie, "gmid="+fakerepsol.ValidGMID) {
		t.Errorf("login cookie = %q, want it to carry the session gmid", cookie)
	}
}

func TestExpiredWithoutPassword(t *testing.T) {
	a, fake, st, _ := newTestApp(t)
	storeSession(t, st, config.Credentials{
		LoginToken: fakerepsol.ValidToken,
		GMID:       fakerepsol.ValidGMID,
	})
	fake.RejectToken(fakerepsol.ValidToken)

	_, err := a.session.Sign(context.Background())
	typed := requireErrorCode(t, err, "session_expired")
	if typed.Category != apperr.Auth {
		t.Errorf("category = %v, want %v", typed.Category, apperr.Auth)
	}
	if !strings.Contains(typed.Hint, "repsol auth login") {
		t.Errorf("hint = %q, want it to point at auth login", typed.Hint)
	}
	if !strings.Contains(typed.Hint, "--email") {
		t.Errorf("hint = %q, want it to mention --email", typed.Hint)
	}
	if got := fake.Count("/gigya/accounts.login"); got != 0 {
		t.Errorf("login calls = %d, want 0 without a password", got)
	}
}

func TestNotLoggedIn(t *testing.T) {
	a, _, _, _ := newTestApp(t)

	_, err := a.session.Sign(context.Background())
	typed := requireErrorCode(t, err, "not_logged_in")
	if typed.Category != apperr.Auth {
		t.Errorf("category = %v, want %v", typed.Category, apperr.Auth)
	}
	if !strings.Contains(typed.Hint, "repsol auth login") {
		t.Errorf("hint = %q, want it to point at auth login", typed.Hint)
	}
}

func TestCaptchaDoesNotLoop(t *testing.T) {
	a, fake, st, _ := newTestApp(t)
	storeSession(t, st, config.Credentials{
		LoginToken: fakerepsol.ValidToken,
		GMID:       fakerepsol.ValidGMID,
		Email:      fakerepsol.CaptchaEmail,
		Password:   "x",
	})
	fake.RejectToken(fakerepsol.ValidToken)

	_, err := a.session.Sign(context.Background())
	typed := requireErrorCode(t, err, gigya.CodeCaptchaRequired)
	if typed.Category != apperr.Auth {
		t.Errorf("category = %v, want %v", typed.Category, apperr.Auth)
	}
	if got := fake.Count("/gigya/accounts.login"); got != 1 {
		t.Errorf("login calls = %d, want exactly 1", got)
	}
}

func TestEnvSessionIsNotPersisted(t *testing.T) {
	ctx := context.Background()

	t.Run("valid token", func(t *testing.T) {
		a, fake, st, _ := newTestApp(t, func(d *Deps) {
			d.Env = Env{LoginToken: fakerepsol.ValidToken, GMID: fakerepsol.ValidGMID}
		})

		if _, err := a.session.Sign(ctx); err != nil {
			t.Fatalf("Sign with an environment session: %v", err)
		}
		if got := fake.Count("/gigya/accounts.getAccountInfo"); got != 1 {
			t.Errorf("getAccountInfo calls = %d, want 1", got)
		}
		requireNoCredentialsFile(t, st)
	})

	t.Run("rejected token with password", func(t *testing.T) {
		a, fake, st, _ := newTestApp(t, func(d *Deps) {
			d.Env = Env{
				LoginToken: fakerepsol.ValidToken,
				GMID:       fakerepsol.ValidGMID,
				Email:      fakerepsol.Email,
				Password:   fakerepsol.Password,
			}
		})
		fake.RejectToken(fakerepsol.ValidToken)

		if _, err := a.session.Sign(ctx); err != nil {
			t.Fatalf("Sign with an environment session and password: %v", err)
		}
		if got := fake.Count("/gigya/accounts.login"); got != 1 {
			t.Errorf("login calls = %d, want 1", got)
		}
		requireNoCredentialsFile(t, st)
	})
}

func TestEnvTokenWithoutGMID(t *testing.T) {
	a, _, _, _ := newTestApp(t, func(d *Deps) {
		d.Env = Env{LoginToken: fakerepsol.ValidToken}
	})

	_, err := a.session.Sign(context.Background())
	typed := requireErrorCode(t, err, "missing_gmid")
	if typed.Category != apperr.Usage {
		t.Errorf("category = %v, want %v", typed.Category, apperr.Usage)
	}
	if !strings.Contains(typed.Hint, "REPSOL_GMID") {
		t.Errorf("hint = %q, want it to mention REPSOL_GMID", typed.Hint)
	}
}
