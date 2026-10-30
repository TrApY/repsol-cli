package repsolapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// stubSigner is a Signer that always returns the same credentials, or err when
// it is set.
type stubSigner struct{ err error }

// Sign returns fixed credentials, or the configured error.
func (s stubSigner) Sign(context.Context) (Auth, error) {
	return Auth{UID: "U1", Signature: "S1", Timestamp: "1700000000"}, s.err
}

func TestHistoryAuthHeaders(t *testing.T) {
	body := fixtureBytes(t, "testdata/history.json")
	var rec capture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		writeJSONBody(w, body)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: testHTTP(t, srv), Signer: stubSigner{}}
	entries, err := c.OrderHistory(context.Background(), 20, 0)
	if err != nil {
		t.Fatalf("OrderHistory: %v", err)
	}

	reqs := rec.all()
	if len(reqs) != 1 {
		t.Fatalf("server saw %d requests, want 1", len(reqs))
	}
	req := reqs[0]
	if req.method != http.MethodGet {
		t.Errorf("method = %s, want GET", req.method)
	}
	if req.path != "/order-history" {
		t.Errorf("path = %s, want /order-history", req.path)
	}
	for key, want := range map[string]string{
		"uid":                "U1",
		"signature":          "S1",
		"signatureTimestamp": "1700000000",
		"ptb-gigya-user":     "U1",
	} {
		if got := req.header.Get(key); got != want {
			t.Errorf("header %s = %q, want %q", key, got, want)
		}
	}
	if got := req.header.Get("ptb-order-process-id"); got != "" {
		t.Errorf("ptb-order-process-id = %q, want no telemetry header on a read", got)
	}
	for key, want := range map[string]string{"limit": "20", "offset": "0"} {
		if got := req.query.Get(key); got != want {
			t.Errorf("query %s = %q, want %q", key, got, want)
		}
	}
	if got := req.query.Get("paramToAvoidCache"); got == "" {
		t.Error("paramToAvoidCache is empty, want the current Unix milliseconds")
	}

	if len(entries) != 2 {
		t.Fatalf("got %d history entries, want 2", len(entries))
	}
	h := entries[0]
	if h.ReferenceSAC != "10000001" {
		t.Errorf("ReferenceSAC = %q, want 10000001", h.ReferenceSAC)
	}
	if h.MaterialID != "153" {
		t.Errorf("MaterialID = %q, want 153", h.MaterialID)
	}
	if h.Agency.AgencyCode != "0000000001" {
		t.Errorf("Agency.AgencyCode = %q, want 0000000001", h.Agency.AgencyCode)
	}
	if h.Address.LocalityCode != "28000000001" {
		t.Errorf("Address.LocalityCode = %q, want 28000000001", h.Address.LocalityCode)
	}
	if h.Discount != nil {
		t.Errorf("Discount = %v, want nil", *h.Discount)
	}
	if h.PaymentMethod != nil {
		t.Errorf("PaymentMethod = %v, want nil", *h.PaymentMethod)
	}
	if h.Quantity != 1 {
		t.Errorf("Quantity = %d, want 1", h.Quantity)
	}
}

func TestSignerErrorStopsRequest(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeJSONBody(w, []byte(`[]`))
	}))
	defer srv.Close()

	signerErr := apperr.New(apperr.Auth, "not_logged_in", "x")
	c := &Client{BaseURL: srv.URL, HTTP: testHTTP(t, srv), Signer: stubSigner{err: signerErr}}
	_, err := c.OrderHistory(context.Background(), 20, 0)
	if err != signerErr {
		t.Fatalf("OrderHistory error = %v, want the signer error returned unchanged", err)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("server saw %d requests, want 0 when signing fails", got)
	}
}
