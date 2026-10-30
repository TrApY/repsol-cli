package app

import (
	"context"
	"testing"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/config"
	"github.com/felipe9962/repsol-cli/internal/fakerepsol"
)

func TestStatusCheck(t *testing.T) {
	ctx := context.Background()

	t.Run("valid session", func(t *testing.T) {
		a, fake, st, _ := newTestApp(t)
		storeSession(t, st, config.Credentials{
			LoginToken: fakerepsol.ValidToken,
			GMID:       fakerepsol.ValidGMID,
			Email:      fakerepsol.Email,
			FirstName:  fakerepsol.FirstName,
			Password:   fakerepsol.Password,
		})

		status, err := a.Status(ctx, true)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if !status.Checked {
			t.Error("status.Checked = false, want true")
		}
		if status.SessionValid == nil {
			t.Fatal("status.SessionValid = nil, want a value")
		}
		if !*status.SessionValid {
			t.Error("status.SessionValid = false, want true")
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
		if got := fake.Count("/gigya/accounts.getAccountInfo"); got != 1 {
			t.Errorf("getAccountInfo calls = %d, want 1", got)
		}
	})

	t.Run("rejected without password", func(t *testing.T) {
		a, fake, st, _ := newTestApp(t)
		storeSession(t, st, config.Credentials{
			LoginToken: fakerepsol.ValidToken,
			GMID:       fakerepsol.ValidGMID,
		})
		fake.RejectToken(fakerepsol.ValidToken)

		_, err := a.Status(ctx, true)
		typed := requireErrorCode(t, err, "session_expired")
		if typed.Category != apperr.Auth {
			t.Errorf("category = %v, want %v", typed.Category, apperr.Auth)
		}
	})

	t.Run("not checked", func(t *testing.T) {
		a, fake, st, _ := newTestApp(t)
		storeSession(t, st, config.Credentials{
			LoginToken: fakerepsol.ValidToken,
			GMID:       fakerepsol.ValidGMID,
		})

		status, err := a.Status(ctx, false)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if status.Checked {
			t.Error("status.Checked = true, want false")
		}
		if status.SessionValid != nil {
			t.Errorf("status.SessionValid = %v, want nil", *status.SessionValid)
		}
		if !status.LoggedIn {
			t.Error("status.LoggedIn = false, want true")
		}
		if got := fake.Count("/gigya/accounts.getAccountInfo"); got != 0 {
			t.Errorf("getAccountInfo calls = %d, want 0", got)
		}
	})
}

func TestLogout(t *testing.T) {
	a, _, st, _ := newTestApp(t)
	storeSession(t, st, config.Credentials{
		LoginToken: fakerepsol.ValidToken,
		GMID:       fakerepsol.ValidGMID,
	})
	storeProfile(t, st, "work", config.Credentials{
		LoginToken: fakerepsol.ValidToken,
		GMID:       fakerepsol.ValidGMID,
	})

	result, err := a.Logout()
	if err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if result.Profile != defaultTestProfile {
		t.Errorf("result.Profile = %q, want %q", result.Profile, defaultTestProfile)
	}
	if !result.Removed {
		t.Error("result.Removed = false, want true")
	}
	if _, found, err := st.LoadCredentials(defaultTestProfile); err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	} else if found {
		t.Error("the default profile is still stored after logout")
	}

	other := requireCredentials(t, st, "work")
	if other.LoginToken != fakerepsol.ValidToken {
		t.Errorf("the other profile's login token changed to %q", other.LoginToken)
	}

	again, err := a.Logout()
	if err != nil {
		t.Fatalf("second Logout: %v", err)
	}
	if again.Removed {
		t.Error("second Logout removed credentials, want false")
	}

	// The cached signature is cleared with the session it belonged to.
	_, err = a.session.Sign(context.Background())
	requireErrorCode(t, err, "not_logged_in")
}
