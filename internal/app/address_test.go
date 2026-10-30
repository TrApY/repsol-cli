package app

import (
	"context"
	"strings"
	"testing"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/config"
	"github.com/felipe9962/repsol-cli/internal/fakerepsol"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// mustSetAddress stores "Calle Falsa 1, Pueblo", the synthetic address the fake
// resolves without geocoding, and fails the test when SetAddress does.
func mustSetAddress(t *testing.T, a *App) {
	t.Helper()
	if _, err := a.SetAddress(context.Background(), SetAddressInput{Text: "Calle Falsa 1, Pueblo"}); err != nil {
		t.Fatalf("SetAddress: %v", err)
	}
}

// requireProfileAddress returns the address of the default test profile and
// fails the test when none is stored.
func requireProfileAddress(t *testing.T, st config.Store) repsolapi.Address {
	t.Helper()
	cfg, err := st.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	addr := cfg.Profiles[defaultTestProfile].Address
	if addr == nil {
		t.Fatal("no address is stored for the default profile")
	}
	return *addr
}

// requireNoProfileAddress asserts that no address was stored for the default
// profile.
func requireNoProfileAddress(t *testing.T, st config.Store) {
	t.Helper()
	cfg, err := st.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if addr := cfg.Profiles[defaultTestProfile].Address; addr != nil {
		t.Fatalf("stored address = %+v, want none", addr)
	}
}

func TestSetAddressRepsolResultUsedAsIs(t *testing.T) {
	ctx := context.Background()
	a, fake, st, _ := newTestApp(t)
	storeSession(t, st, config.Credentials{
		LoginToken: fakerepsol.ValidToken,
		GMID:       fakerepsol.ValidGMID,
	})

	view, err := a.SetAddress(ctx, SetAddressInput{
		Text:  "Calle Falsa 1, Pueblo",
		Floor: "2",
		Door:  "B",
	})
	if err != nil {
		t.Fatalf("SetAddress: %v", err)
	}

	if got := fake.Count("/places/v1/geocode"); got != 0 {
		t.Errorf("geocode calls = %d, want 0", got)
	}
	if got := fake.Count("/api/address-maps"); got != 0 {
		t.Errorf("address-maps calls = %d, want 0", got)
	}
	if got := fake.Count("/api/agency"); got != 1 {
		t.Errorf("agency calls = %d, want 1", got)
	}

	const fullAddress = "Calle Falsa 1, 28001 Pueblo, Madrid"
	if view.Profile != defaultTestProfile {
		t.Errorf("view.Profile = %q, want %q", view.Profile, defaultTestProfile)
	}
	if view.FullAddress != fullAddress {
		t.Errorf("view.FullAddress = %q, want %q", view.FullAddress, fullAddress)
	}
	if view.PostalCode != fakerepsol.PostalCode {
		t.Errorf("view.PostalCode = %q, want %q", view.PostalCode, fakerepsol.PostalCode)
	}
	if view.Locality != "Pueblo" {
		t.Errorf("view.Locality = %q, want %q", view.Locality, "Pueblo")
	}
	if view.Province != "Madrid" {
		t.Errorf("view.Province = %q, want %q", view.Province, "Madrid")
	}
	if view.Floor != "2" {
		t.Errorf("view.Floor = %q, want %q", view.Floor, "2")
	}
	if view.Door != "B" {
		t.Errorf("view.Door = %q, want %q", view.Door, "B")
	}
	if view.Details != "" {
		t.Errorf("view.Details = %q, want empty", view.Details)
	}
	if view.AgencyCode != fakerepsol.AgencyCode {
		t.Errorf("view.AgencyCode = %q, want %q", view.AgencyCode, fakerepsol.AgencyCode)
	}
	if view.Origin != "repsol" {
		t.Errorf("view.Origin = %q, want %q", view.Origin, "repsol")
	}
	if want := fullAddress + ", floor 2, door B"; view.Summary != want {
		t.Errorf("view.Summary = %q, want %q", view.Summary, want)
	}

	addr := requireProfileAddress(t, st)
	if addr.IsPeninsula {
		t.Error("stored IsPeninsula = true, want false")
	}
	if addr.Latitude != "" {
		t.Errorf("stored Latitude = %q, want empty", addr.Latitude)
	}
	if addr.AutocompleteContent != fullAddress {
		t.Errorf("stored AutocompleteContent = %q, want %q", addr.AutocompleteContent, fullAddress)
	}
	if addr.FullAddress != fullAddress {
		t.Errorf("stored FullAddress = %q, want %q", addr.FullAddress, fullAddress)
	}
	if addr.PostalCode != fakerepsol.PostalCode {
		t.Errorf("stored PostalCode = %q, want %q", addr.PostalCode, fakerepsol.PostalCode)
	}
	if addr.ProvinceCode != fakerepsol.ProvinceCode {
		t.Errorf("stored ProvinceCode = %q, want %q", addr.ProvinceCode, fakerepsol.ProvinceCode)
	}
	if addr.AgencyCode != fakerepsol.AgencyCode {
		t.Errorf("stored AgencyCode = %q, want %q", addr.AgencyCode, fakerepsol.AgencyCode)
	}
	if addr.Floor != "2" || addr.Door != "B" {
		t.Errorf("stored Floor/Door = %q/%q, want %q/%q", addr.Floor, addr.Door, "2", "B")
	}

	shown, err := a.ShowAddress()
	if err != nil {
		t.Fatalf("ShowAddress: %v", err)
	}
	if shown != view {
		t.Errorf("ShowAddress = %+v, want %+v", shown, view)
	}
}

func TestSetAddressGoogleGeocodes(t *testing.T) {
	ctx := context.Background()
	a, fake, st, _ := newTestApp(t)
	storeSession(t, st, config.Credentials{
		LoginToken: fakerepsol.ValidToken,
		GMID:       fakerepsol.ValidGMID,
	})

	view, err := a.SetAddress(ctx, SetAddressInput{Text: "Calle Ambigua", Pick: 2})
	if err != nil {
		t.Fatalf("SetAddress: %v", err)
	}

	if got := fake.Count("/places/v1/geocode"); got != 1 {
		t.Fatalf("geocode calls = %d, want 1", got)
	}
	query := fake.Requests("/places/v1/geocode")[0].Query
	if got := query.Get("placeId"); got != "place-b" {
		t.Errorf("geocode placeId = %q, want %q", got, "place-b")
	}
	if got := query.Get("number"); got != "7" {
		t.Errorf("geocode number = %q, want %q", got, "7")
	}
	if got := fake.Count("/api/address-maps"); got != 1 {
		t.Errorf("address-maps calls = %d, want 1", got)
	}

	if want := "Calle Ambigua 7, 28001 Pueblo, Madrid"; view.FullAddress != want {
		t.Errorf("view.FullAddress = %q, want %q", view.FullAddress, want)
	}
	if view.Origin != "google" {
		t.Errorf("view.Origin = %q, want %q", view.Origin, "google")
	}
	if view.AgencyCode != fakerepsol.AgencyCode {
		t.Errorf("view.AgencyCode = %q, want %q", view.AgencyCode, fakerepsol.AgencyCode)
	}

	addr := requireProfileAddress(t, st)
	if addr.Latitude != "40.4168" {
		t.Errorf("stored Latitude = %q, want %q", addr.Latitude, "40.4168")
	}
	if addr.Confidence == nil {
		t.Fatal("stored Confidence = nil, want the geocoder block")
	}
	if addr.Number != "7" {
		t.Errorf("stored Number = %q, want %q", addr.Number, "7")
	}
	if addr.AutocompleteContent != "Calle Ambigua 7, 28001 Pueblo, Madrid" {
		t.Errorf("stored AutocompleteContent = %q, want the chosen candidate", addr.AutocompleteContent)
	}
}

func TestSetAddressAmbiguous(t *testing.T) {
	ctx := context.Background()
	a, fake, st, _ := newTestApp(t)
	storeSession(t, st, config.Credentials{
		LoginToken: fakerepsol.ValidToken,
		GMID:       fakerepsol.ValidGMID,
	})

	_, err := a.SetAddress(ctx, SetAddressInput{Text: "Calle Ambigua"})
	typed := requireErrorCode(t, err, "ambiguous_address")
	if typed.Category != apperr.Usage {
		t.Errorf("category = %v, want %v", typed.Category, apperr.Usage)
	}
	for _, want := range []string{
		"1) Calle Ambigua 5, 28001 Pueblo, Madrid [google]",
		"2) Calle Ambigua 7, 28001 Pueblo, Madrid [google]",
	} {
		if !strings.Contains(typed.Message, want) {
			t.Errorf("message %q does not contain %q", typed.Message, want)
		}
	}
	if !strings.Contains(typed.Hint, "--pick") {
		t.Errorf("hint %q does not mention --pick", typed.Hint)
	}

	requireNoProfileAddress(t, st)
	if got := fake.Count("/places/v1/geocode"); got != 0 {
		t.Errorf("geocode calls = %d, want 0", got)
	}
	if got := fake.Count("/api/agency"); got != 0 {
		t.Errorf("agency calls = %d, want 0", got)
	}
}

func TestSetAddressRejections(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		pick     int
		code     string
		category apperr.Category
	}{
		{
			name:     "without a street number",
			text:     "Sin numero",
			code:     "address_needs_number",
			category: apperr.Usage,
		},
		{
			name:     "neutral needs a policy lookup",
			text:     "Neutral",
			code:     "address_not_supported",
			category: apperr.Usage,
		},
		{
			name:     "agency does not serve it",
			text:     "Lejos",
			code:     "address_not_served",
			category: apperr.Refused,
		},
		{
			name:     "pick outside the candidates",
			text:     "Ambigua",
			pick:     3,
			code:     "invalid_pick",
			category: apperr.Usage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			a, _, st, _ := newTestApp(t)
			storeSession(t, st, config.Credentials{
				LoginToken: fakerepsol.ValidToken,
				GMID:       fakerepsol.ValidGMID,
			})

			_, err := a.SetAddress(ctx, SetAddressInput{Text: tt.text, Pick: tt.pick})
			typed := requireErrorCode(t, err, tt.code)
			if typed.Category != tt.category {
				t.Errorf("category = %v, want %v", typed.Category, tt.category)
			}
			requireNoProfileAddress(t, st)
		})
	}

	t.Run("neutral message", func(t *testing.T) {
		ctx := context.Background()
		a, _, _, _ := newTestApp(t)

		_, err := a.SetAddress(ctx, SetAddressInput{Text: "Neutral"})
		typed := requireErrorCode(t, err, "address_not_supported")
		const want = "this address needs a policy lookup the CLI does not support; try a fuller address"
		if typed.Message != want {
			t.Errorf("message = %q, want %q", typed.Message, want)
		}
	})
}
