package app

import (
	"context"
	"testing"

	"github.com/felipe9962/repsol-cli/internal/config"
	"github.com/felipe9962/repsol-cli/internal/fakerepsol"
)

// requireCheck returns the named check of a report and fails the test when it
// is missing.
func requireCheck(t *testing.T, report DoctorReport, name string) DoctorCheck {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("report has no %q check: %+v", name, report.Checks)
	return DoctorCheck{}
}

// TestDoctorAllGreen covers a fully configured profile: every check passes and
// the checks come in their documented order.
func TestDoctorAllGreen(t *testing.T) {
	a, _, st, _ := newTestApp(t)
	storeSession(t, st, config.Credentials{
		LoginToken: fakerepsol.ValidToken,
		GMID:       fakerepsol.ValidGMID,
	})
	mustSetAddress(t, a)

	report := a.Doctor(context.Background())
	if !report.OK {
		t.Fatalf("report.OK = false, checks = %+v", report.Checks)
	}

	want := []string{"config", "credentials", "session", "api", "address", "agency"}
	if len(report.Checks) != len(want) {
		t.Fatalf("len(checks) = %d, want %d: %+v", len(report.Checks), len(want), report.Checks)
	}
	for i, name := range want {
		if report.Checks[i].Name != name {
			t.Errorf("checks[%d].Name = %q, want %q", i, report.Checks[i].Name, name)
		}
		if !report.Checks[i].OK {
			t.Errorf("check %q failed: %+v", name, report.Checks[i])
		}
	}
}

// TestDoctorMissingAddress covers the dependency rule: without an address the
// address check fails, and the agency check is skipped because of it.
func TestDoctorMissingAddress(t *testing.T) {
	a, _, st, _ := newTestApp(t)
	storeSession(t, st, config.Credentials{
		LoginToken: fakerepsol.ValidToken,
		GMID:       fakerepsol.ValidGMID,
	})

	report := a.Doctor(context.Background())
	if report.OK {
		t.Fatal("report.OK = true, want false without an address")
	}

	address := requireCheck(t, report, "address")
	if address.OK {
		t.Errorf("address check = %+v, want a failure", address)
	}

	agency := requireCheck(t, report, "agency")
	if agency.OK {
		t.Errorf("agency check = %+v, want a failure", agency)
	}
	if agency.Detail != "skipped: address failed" {
		t.Errorf("agency detail = %q, want %q", agency.Detail, "skipped: address failed")
	}
}
