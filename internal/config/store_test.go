package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

func newStore(t *testing.T) Store {
	t.Helper()
	d := filepath.Join(t.TempDir(), AppDir)
	return Store{Paths: Paths{
		Config:      filepath.Join(d, "config.toml"),
		Credentials: filepath.Join(d, "credentials.toml"),
		Lock:        filepath.Join(d, "credentials.toml.lock"),
	}}
}

func ptr(s string) *string { return &s }

func TestMissingFilesAreEmpty(t *testing.T) {
	s := newStore(t)

	cfg, err := s.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig on missing file: %v", err)
	}
	if !reflect.DeepEqual(cfg, Config{}) {
		t.Errorf("LoadConfig on missing file = %+v, want zero Config", cfg)
	}

	cred, found, err := s.LoadCredentials("default")
	if err != nil {
		t.Fatalf("LoadCredentials on missing file: %v", err)
	}
	if found {
		t.Error("LoadCredentials on missing file reported found = true")
	}
	if !reflect.DeepEqual(cred, Credentials{}) {
		t.Errorf("LoadCredentials on missing file = %+v, want zero Credentials", cred)
	}
}

func TestCredentialsFileIsPrivate(t *testing.T) {
	s := newStore(t)

	err := s.UpdateCredentials("default", func(c *Credentials) error {
		c.LoginToken = "t"
		c.GMID = "g"
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateCredentials: %v", err)
	}

	info, err := os.Stat(s.Paths.Credentials)
	if err != nil {
		t.Fatalf("stat credentials file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("credentials file mode = %04o, want 0600", got)
	}

	dirInfo, err := os.Stat(filepath.Dir(s.Paths.Credentials))
	if err != nil {
		t.Fatalf("stat credentials directory: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("credentials directory mode = %04o, want 0700", got)
	}

	cred, found, err := s.LoadCredentials("default")
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if !found {
		t.Fatal("LoadCredentials found = false, want true")
	}
	if cred.LoginToken != "t" || cred.GMID != "g" {
		t.Errorf("LoadCredentials = %+v, want LoginToken %q and GMID %q", cred, "t", "g")
	}
}

func TestConfigAddressRoundTrip(t *testing.T) {
	s := newStore(t)

	addr := &repsolapi.Address{
		Province:         "Madrid",
		ProvinceCode:     "28",
		Locality:         "Madrid",
		LocalityCode:     "28000000001",
		Municipality:     "Madrid",
		MunicipalityCode: "28000000001",
		Street:           "Gran Via",
		StreetCode:       "28000000002",
		Number:           "1",
		FullAddress:      "Gran Via 1, Madrid",
		PostalCode:       "28013",
		Latitude:         repsolapi.FlexNumber("40.4168"),
		Longitude:        repsolapi.FlexNumber("-3.7038"),
		IsPeninsula:      true,
		Confidence:       &repsolapi.Confidence{},
		CurrentStreet:    ptr(""),
		Neutral:          nil,
	}
	want := Config{
		DefaultProfile: "default",
		Profiles: map[string]Profile{
			"default": {Address: addr},
		},
	}

	err := s.UpdateConfig(func(c *Config) error {
		c.DefaultProfile = "default"
		if c.Profiles == nil {
			c.Profiles = make(map[string]Profile)
		}
		c.Profiles["default"] = Profile{Address: addr}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	got, err := s.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("config round trip mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestConcurrentCredentialUpdates(t *testing.T) { // Review Focus 5
	s := newStore(t)

	const n = 20
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each goroutine uses its own Store value on the same paths.
			st := Store{Paths: s.Paths}
			err := st.UpdateCredentials(fmt.Sprintf("p%d", i), func(c *Credentials) error {
				c.LoginToken = fmt.Sprintf("t%d", i)
				return nil
			})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent UpdateCredentials: %v", err)
	}

	for i := 0; i < n; i++ {
		name := fmt.Sprintf("p%d", i)
		cred, found, err := s.LoadCredentials(name)
		if err != nil {
			t.Fatalf("LoadCredentials(%q): %v", name, err)
		}
		if !found {
			t.Errorf("profile %q is missing after concurrent updates", name)
			continue
		}
		if want := fmt.Sprintf("t%d", i); cred.LoginToken != want {
			t.Errorf("profile %q token = %q, want %q", name, cred.LoginToken, want)
		}
	}

	info, err := os.Stat(s.Paths.Credentials)
	if err != nil {
		t.Fatalf("stat credentials file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("credentials file mode = %04o, want 0600", got)
	}
}

func TestDeleteCredentials(t *testing.T) {
	s := newStore(t)

	set := func(profile, token string) {
		t.Helper()
		err := s.UpdateCredentials(profile, func(c *Credentials) error {
			c.LoginToken = token
			return nil
		})
		if err != nil {
			t.Fatalf("UpdateCredentials(%q): %v", profile, err)
		}
	}
	set("a", "ta")
	set("b", "tb")

	deleted, err := s.DeleteCredentials("a")
	if err != nil {
		t.Fatalf("DeleteCredentials(a): %v", err)
	}
	if !deleted {
		t.Error("DeleteCredentials(a) = false, want true")
	}

	if _, found, err := s.LoadCredentials("a"); err != nil {
		t.Fatalf("LoadCredentials(a): %v", err)
	} else if found {
		t.Error("profile a is still present after deletion")
	}

	cred, found, err := s.LoadCredentials("b")
	if err != nil {
		t.Fatalf("LoadCredentials(b): %v", err)
	}
	if !found || cred.LoginToken != "tb" {
		t.Errorf("profile b = %+v, found %v, want token %q", cred, found, "tb")
	}

	deleted, err = s.DeleteCredentials("a")
	if err != nil {
		t.Fatalf("second DeleteCredentials(a): %v", err)
	}
	if deleted {
		t.Error("second DeleteCredentials(a) = true, want false")
	}
}

func TestTryLockOrder(t *testing.T) {
	s := newStore(t)

	release, ok, err := s.TryLockOrder("default")
	if err != nil || !ok {
		t.Fatalf("first TryLockOrder = %v, %v, want true, nil", ok, err)
	}

	if _, ok, err := s.TryLockOrder("default"); err != nil || ok {
		t.Fatalf("TryLockOrder while held = %v, %v, want false, nil", ok, err)
	}

	// Another profile has a lock of its own.
	other, ok, err := s.TryLockOrder("work")
	if err != nil || !ok {
		t.Fatalf("TryLockOrder(work) = %v, %v, want true, nil", ok, err)
	}
	other()

	// A re-login during an order writes the credentials under the store lock,
	// which must not wait for the order lock.
	if err := s.UpdateCredentials("default", func(c *Credentials) error {
		c.LoginToken = "renewed"
		return nil
	}); err != nil {
		t.Fatalf("UpdateCredentials while the order lock is held: %v", err)
	}

	release()
	again, ok, err := s.TryLockOrder("default")
	if err != nil || !ok {
		t.Fatalf("TryLockOrder after release = %v, %v, want true, nil", ok, err)
	}
	again()

	_, _, err = s.TryLockOrder("../escape")
	var typed *apperr.Error
	if !errors.As(err, &typed) || typed.Code != "invalid_profile" {
		t.Fatalf("TryLockOrder(../escape) error = %v, want invalid_profile", err)
	}
}

func TestResolveProfile(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		env     string
		def     string
		want    string
		wantErr bool
	}{
		{name: "flag wins", flag: "work", env: "home", def: "x", want: "work"},
		{name: "env next", env: "home", def: "x", want: "home"},
		{name: "config default", def: "x", want: "x"},
		{name: "fallback", want: "default"},
		{name: "invalid", flag: "Bad Name", wantErr: true},
		{name: "uppercase", flag: "Work", wantErr: true},
		{name: "leading dash", flag: "-work", wantErr: true},
		{name: "too long", flag: strings.Repeat("a", 33), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveProfile(tt.flag, tt.env, Config{DefaultProfile: tt.def})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveProfile(%q, %q, %q) = %q, want error", tt.flag, tt.env, tt.def, got)
				}
				var typed *apperr.Error
				if !errors.As(err, &typed) {
					t.Fatalf("error %v is not *apperr.Error", err)
				}
				if typed.Category != apperr.Usage {
					t.Errorf("category = %v, want Usage", typed.Category)
				}
				if typed.Code != "invalid_profile" {
					t.Errorf("code = %q, want %q", typed.Code, "invalid_profile")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveProfile(%q, %q, %q): %v", tt.flag, tt.env, tt.def, err)
			}
			if got != tt.want {
				t.Errorf("ResolveProfile(%q, %q, %q) = %q, want %q", tt.flag, tt.env, tt.def, got, tt.want)
			}
		})
	}
}

func TestInvalidTOMLNeverLeaksContent(t *testing.T) {
	s := newStore(t)
	if err := os.MkdirAll(filepath.Dir(s.Paths.Credentials), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	const secret = "hunter2-do-not-leak"
	corrupt := "[profiles.default]\npassword = \"" + secret + "\n"
	if err := os.WriteFile(s.Paths.Credentials, []byte(corrupt), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, _, err := s.LoadCredentials("default")
	if err == nil {
		t.Fatal("LoadCredentials on invalid TOML returned nil error")
	}
	var typed *apperr.Error
	if !errors.As(err, &typed) {
		t.Fatalf("error %v is not *apperr.Error", err)
	}
	if typed.Category != apperr.Generic {
		t.Errorf("category = %v, want Generic", typed.Category)
	}
	if typed.Code != "config_invalid" {
		t.Errorf("code = %q, want %q", typed.Code, "config_invalid")
	}
	if !strings.Contains(typed.Message, s.Paths.Credentials) {
		t.Errorf("message %q does not name path %q", typed.Message, s.Paths.Credentials)
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error message leaks file content: %v", err)
	}
}

func TestPendingPayment(t *testing.T) {
	s := newStore(t)
	started := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)

	if _, found, err := s.LoadPendingPayment("default"); err != nil || found {
		t.Fatalf("LoadPendingPayment on a missing file = found %v, err %v; want neither", found, err)
	}

	if err := s.SavePendingPayment("default", PendingPayment{PaymentID: "p-1", StartedAt: started}); err != nil {
		t.Fatalf("SavePendingPayment: %v", err)
	}
	got, found, err := s.LoadPendingPayment("default")
	if err != nil || !found {
		t.Fatalf("LoadPendingPayment = found %v, err %v; want the payment", found, err)
	}
	if got.PaymentID != "p-1" || !got.StartedAt.Equal(started) {
		t.Errorf("pending payment = %+v, want p-1 started at %v", got, started)
	}

	// The id is as private as a credential, and other profiles are not touched.
	path, err := s.pendingPath("default")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 0600", info.Mode().Perm())
	}
	if _, found, _ := s.LoadPendingPayment("other"); found {
		t.Error("another profile sees the payment")
	}
	if data, err := os.ReadFile(s.Paths.Config); err == nil && strings.Contains(string(data), "p-1") {
		t.Error("the payment id reached config.toml")
	}

	// A payment started later is not dropped by the resolution of an older one.
	if err := s.ClearPendingPayment("default", "p-0"); err != nil {
		t.Fatalf("ClearPendingPayment of another id: %v", err)
	}
	if _, found, _ := s.LoadPendingPayment("default"); !found {
		t.Fatal("clearing p-0 dropped p-1")
	}
	if err := s.ClearPendingPayment("default", "p-1"); err != nil {
		t.Fatalf("ClearPendingPayment: %v", err)
	}
	if _, found, _ := s.LoadPendingPayment("default"); found {
		t.Error("the payment is still pending after it was cleared")
	}
	if err := s.ClearPendingPayment("default", ""); err != nil {
		t.Errorf("clearing a missing payment: %v", err)
	}

	if err := s.SavePendingPayment("Bad Name", PendingPayment{PaymentID: "x"}); err == nil {
		t.Error("an invalid profile name was accepted")
	}
}

func TestLogoutKeepsThePendingPayment(t *testing.T) {
	s := newStore(t)
	if err := s.SavePendingPayment("default", PendingPayment{PaymentID: "p-1"}); err != nil {
		t.Fatalf("SavePendingPayment: %v", err)
	}
	if err := s.UpdateCredentials("default", func(c *Credentials) error { c.LoginToken = "t"; return nil }); err != nil {
		t.Fatalf("UpdateCredentials: %v", err)
	}
	if _, err := s.DeleteCredentials("default"); err != nil {
		t.Fatalf("DeleteCredentials: %v", err)
	}
	if _, found, _ := s.LoadPendingPayment("default"); !found {
		t.Error("deleting the credentials dropped the pending payment")
	}
}

func TestDiscardPendingPayment(t *testing.T) {
	s := newStore(t)
	if err := s.DiscardPendingPayment("default"); err != nil {
		t.Fatalf("DiscardPendingPayment with nothing remembered: %v", err)
	}

	// A readable payment goes, whatever its id.
	if err := s.SavePendingPayment("default", PendingPayment{PaymentID: "p-1"}); err != nil {
		t.Fatalf("SavePendingPayment: %v", err)
	}
	if err := s.DiscardPendingPayment("default"); err != nil {
		t.Fatalf("DiscardPendingPayment: %v", err)
	}
	if _, found, _ := s.LoadPendingPayment("default"); found {
		t.Error("the payment is still remembered")
	}

	// So does a file that cannot be read, which is what the call is for.
	path, err := s.pendingPath("default")
	if err != nil {
		t.Fatalf("pendingPath: %v", err)
	}
	if err := os.WriteFile(path, []byte("[[not toml"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := s.LoadPendingPayment("default"); err == nil {
		t.Fatal("LoadPendingPayment of a damaged file succeeded")
	}
	if err := s.DiscardPendingPayment("default"); err != nil {
		t.Fatalf("DiscardPendingPayment of a damaged file: %v", err)
	}
	if _, found, err := s.LoadPendingPayment("default"); err != nil || found {
		t.Errorf("after the discard: found %v, err %v; want neither", found, err)
	}

	if err := s.DiscardPendingPayment("Bad Name"); err == nil {
		t.Error("DiscardPendingPayment accepted an invalid profile name")
	}
}
