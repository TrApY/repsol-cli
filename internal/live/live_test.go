//go:build live

// Package live holds the read-only checks that run against the real Repsol
// API. They are excluded from the normal build and only run with the "live"
// build tag.
//
// The suite never writes: it only reads the account, the history, the catalog
// and the delivery schedule. It is a check that the private API still answers
// the way the CLI expects, not a test of the order flow.
//
//	go test -tags live -run Live -v ./internal/live/
//
// It needs a session in the environment:
//
//	REPSOL_LOGIN_TOKEN=… REPSOL_GMID=… go test -tags live ./internal/live/
package live

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/felipe9962/repsol-cli/internal/gigya"
	"github.com/felipe9962/repsol-cli/internal/httpx"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// readOnlyTransport fails the test when the suite tries anything but a read.
// The only POST the live suite may send is the Gigya signature refresh.
type readOnlyTransport struct {
	base http.RoundTripper
	t    *testing.T
}

func (rt readOnlyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	allowed := req.Method == http.MethodGet ||
		(req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/accounts.getAccountInfo"))
	if !allowed {
		rt.t.Fatalf("the live suite tried to write: %s %s", req.Method, req.URL.Path)
	}
	return rt.base.RoundTrip(req)
}

// liveSigner signs the authenticated calls with the signature Gigya returns.
type liveSigner struct {
	client  *gigya.Client
	session gigya.Session
}

func (s liveSigner) Sign(ctx context.Context) (repsolapi.Auth, error) {
	sig, _, err := s.client.AccountInfo(ctx, s.session)
	if err != nil {
		return repsolapi.Auth{}, err
	}
	return repsolapi.Auth{UID: sig.UID, Signature: sig.UIDSignature, Timestamp: sig.Timestamp}, nil
}

// TestLiveReadOnly checks that the endpoints the CLI depends on still answer.
// It skips itself when no session is configured.
func TestLiveReadOnly(t *testing.T) {
	token := os.Getenv("REPSOL_LOGIN_TOKEN")
	gmid := os.Getenv("REPSOL_GMID")
	if token == "" || gmid == "" {
		t.Skip("set REPSOL_LOGIN_TOKEN and REPSOL_GMID to run the live checks")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	transport := &http.Client{
		Timeout: 30 * time.Second,
		Transport: readOnlyTransport{
			base: http.DefaultTransport,
			t:    t,
		},
	}
	httpClient := httpx.New("repsol-cli/live", nil)
	httpClient.HTTP = transport

	gigyaClient := &gigya.Client{
		BaseURL: gigya.DefaultBaseURL,
		APIKey:  gigya.APIKey,
		HTTP:    httpClient,
		Now:     time.Now,
	}
	session := gigya.Session{LoginToken: token, GMID: gmid}

	api := &repsolapi.Client{
		BaseURL: repsolapi.DefaultBaseURL,
		HTTP:    httpClient,
		Signer:  liveSigner{client: gigyaClient, session: session},
	}
	places := &repsolapi.PlacesClient{
		BaseURL: repsolapi.DefaultPlacesBaseURL,
		HTTP:    httpClient,
	}

	sig, account, err := gigyaClient.AccountInfo(ctx, session)
	if err != nil {
		t.Fatalf("account info failed: %v", err)
	}
	if sig.UID == "" || sig.UIDSignature == "" || sig.Timestamp == "" {
		t.Fatal("account info returned an incomplete signature")
	}
	t.Logf("session works for an account with a name: %t", account.FirstName != "")

	history, err := api.OrderHistory(ctx, 5, 0)
	if err != nil {
		t.Fatalf("order history failed: %v", err)
	}
	t.Logf("history entries read: %d", len(history))

	text := os.Getenv("REPSOL_LIVE_ADDRESS")
	if text == "" {
		text = "Calle de Alcala 1, Madrid"
	}
	results, err := places.Autocomplete(ctx, text)
	if err != nil {
		t.Fatalf("autocomplete failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("autocomplete returned no candidate")
	}

	address := results[0]
	if address.Origin == "google" && address.PlaceID != "" && address.Number != "" {
		geocoded, err := places.Geocode(ctx, address.PlaceID, address.Number)
		if err != nil {
			t.Fatalf("geocode failed: %v", err)
		}
		address = geocoded
	}

	agency, err := api.Agency(ctx, address)
	if err != nil {
		t.Fatalf("agency lookup failed: %v", err)
	}
	if agency.AgencyCode == "" {
		t.Fatal("agency lookup returned no agency code")
	}
	t.Logf("agency resolved: %t", agency.AgencyCode != "")

	catalog, err := api.Catalog(ctx, agency.AgencyCode)
	if err != nil {
		t.Fatalf("catalog failed: %v", err)
	}
	found := false
	for _, product := range catalog {
		if product.MaterialID == "153" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the catalog has no material 153")
	}
	t.Logf("catalog entries read: %d", len(catalog))

	schedules, err := api.DeliverySchedule(ctx, "153", address)
	if err != nil {
		t.Fatalf("delivery schedule failed: %v", err)
	}
	dates := 0
	for _, schedule := range schedules {
		dates += len(schedule.Dates)
	}
	if dates == 0 {
		t.Fatal("the delivery schedule has no dates")
	}
	t.Logf("delivery dates offered: %d", dates)
}
