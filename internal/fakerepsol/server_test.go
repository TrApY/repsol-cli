package fakerepsol

import (
	"context"
	"testing"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/gigya"
	"github.com/felipe9962/repsol-cli/internal/httpx"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// stubSigner signs with a configurable signature, so a test can present the
// wrong one and expect the fake to reject it.
type stubSigner struct {
	signature string
}

// Sign returns the fixed signature.
func (s stubSigner) Sign(context.Context) (repsolapi.Auth, error) {
	return repsolapi.Auth{UID: UID, Signature: s.signature, Timestamp: "1700000000"}, nil
}

// TestFakeAgreesWithClients drives the fake through the real clients: what the
// clients send is what the fake must accept, and what the fake answers is what
// the clients must decode.
func TestFakeAgreesWithClients(t *testing.T) {
	ctx := context.Background()
	f := New()
	defer f.Close()
	hc := httpx.New("repsol-cli/test", nil)

	g := &gigya.Client{BaseURL: f.GigyaURL(), APIKey: gigya.APIKey, HTTP: hc, Now: time.Now}

	// 1. A token without its gmid is rejected.
	if _, _, err := g.AccountInfo(ctx, gigya.Session{LoginToken: ValidToken}); err == nil {
		t.Fatal("AccountInfo without gmid: want an error, got nil")
	} else if typed := apperr.From(err); typed.Category != apperr.Auth || typed.Code != gigya.CodeSessionRejected {
		t.Fatalf("AccountInfo without gmid: want %s/%s, got %s/%s",
			apperr.Auth, gigya.CodeSessionRejected, typed.Category, typed.Code)
	}

	// 2. The same token with its gmid returns the fake signature.
	sig, acct, err := g.AccountInfo(ctx, gigya.Session{LoginToken: ValidToken, GMID: ValidGMID})
	if err != nil {
		t.Fatalf("AccountInfo with gmid: %v", err)
	}
	if sig.UID != UID || sig.UIDSignature != Signature || sig.Timestamp != "1700000000" {
		t.Fatalf("AccountInfo signature = %+v, want UID %q, signature %q, timestamp 1700000000", sig, UID, Signature)
	}
	if acct.Email != Email || acct.FirstName != FirstName {
		t.Fatalf("AccountInfo account = %+v, want email %q, first name %q", acct, Email, FirstName)
	}

	// 3. Password login returns the renewed token.
	session, _, _, err := g.Login(ctx, BootGMID, Email, Password)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if session.LoginToken != RenewedToken {
		t.Fatalf("Login token = %q, want %q", session.LoginToken, RenewedToken)
	}

	// 4. Autocomplete returns both synthetic suggestions for the input.
	places := &repsolapi.PlacesClient{BaseURL: f.PlacesURL(), HTTP: hc}
	results, err := places.Autocomplete(ctx, "Calle Falsa 1, Pueblo")
	if err != nil {
		t.Fatalf("Autocomplete: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Autocomplete returned %d results, want 2", len(results))
	}
	if results[0].Origin != "repsol" || results[0].Number != "1" {
		t.Fatalf("Autocomplete[0] = origin %q number %q, want repsol/1", results[0].Origin, results[0].Number)
	}

	// 5. Agency resolution returns the fake agency.
	api := &repsolapi.Client{BaseURL: f.APIURL(), HTTP: hc, Signer: stubSigner{signature: Signature}}
	agency, err := api.Agency(ctx, results[0])
	if err != nil {
		t.Fatalf("Agency: %v", err)
	}
	if agency.AgencyCode != AgencyCode {
		t.Fatalf("Agency code = %q, want %q", agency.AgencyCode, AgencyCode)
	}

	// 6. History with the wrong signature is an auth failure.
	wrong := &repsolapi.Client{BaseURL: f.APIURL(), HTTP: hc, Signer: stubSigner{signature: "wrong-signature"}}
	if _, err := wrong.OrderHistory(ctx, 20, 0); err == nil {
		t.Fatal("OrderHistory with a wrong signature: want an error, got nil")
	} else if typed := apperr.From(err); typed.Category != apperr.Auth {
		t.Fatalf("OrderHistory with a wrong signature: want category %s, got %s (%v)", apperr.Auth, typed.Category, err)
	}

	// 7. Cart check with the right signer returns the default check.
	check, err := api.CheckCart(ctx, repsolapi.CartCheckRequest{
		AgencyCode: AgencyCode,
		MaterialID: "153",
		Quantity:   1,
		UnitPrice:  18.89,
	})
	if err != nil {
		t.Fatalf("CheckCart: %v", err)
	}
	if check.OrderType != "SOLICITUDE" || check.Status != "EE04" {
		t.Fatalf("CheckCart = %+v, want SOLICITUDE/EE04", check)
	}

	// 8. Every request reached the fake exactly once.
	if got := f.Count("/api/cart/check"); got != 1 {
		t.Fatalf("Count(/api/cart/check) = %d, want 1", got)
	}
}
