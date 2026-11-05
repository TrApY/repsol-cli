// Package fakerepsol is an in-process fake of the three backends the CLI talks
// to: Gigya, the Repsol API and the address service. It serves one httptest
// server and never reaches the network.
//
// Every value it returns is synthetic. Tests use it as the only source of test
// data, so its routes accept exactly what the real clients send and answer with
// the shapes those clients decode.
package fakerepsol

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"time"
	_ "time/tzdata" // makes Europe/Madrid available on every platform

	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// The synthetic values the fake serves. They are also the credentials tests
// sign in with.
const (
	// ValidToken is a login token the fake accepts.
	ValidToken = "fake-token"
	// ValidGMID is the device cookie ValidToken is bound to.
	ValidGMID = "fake-gmid"
	// BootGMID is the device cookie the bootstrap route sets.
	BootGMID = "fake-gmid-boot"
	// RenewedToken is the token a password login returns.
	RenewedToken = "fake-token-renewed"

	// UID is the Gigya user id of the fake account.
	UID = "fake-uid"
	// Signature is the UID signature the fake issues.
	Signature = "fake-signature"

	// Email is the account's email address.
	Email = "owner@example.com"
	// Password is the account's password.
	Password = "correct horse"
	// FirstName is the account's first name.
	FirstName = "Owner"
	// CaptchaEmail is an account whose login always asks for a captcha.
	CaptchaEmail = "captcha@example.com"

	// AgencyCode is the agency that serves the synthetic address.
	AgencyCode = "0000000001"
	// ProvinceCode is the province code of the synthetic address.
	ProvinceCode = "28"
	// LocalityCode is the locality code of the synthetic address.
	LocalityCode = "28000000001"
	// PostalCode is the postal code of the synthetic address.
	PostalCode = "28001"

	// ExternalID is the id a solicitude returns. The order route returns a
	// different id, derived from this one.
	ExternalID = "800000000000000001"
)

// orderExternalID is the id the order route returns.
const orderExternalID = "800000000000000002"

// maxRequestBody caps how much of a request body the fake keeps.
const maxRequestBody = 8 << 20

// Recorded is one request the fake received. Query, Header and Body are copies,
// so a test may inspect them after the fact.
type Recorded struct {
	Method string
	Query  url.Values
	Header http.Header
	Body   []byte
}

// route is one served endpoint.
type route struct {
	method string
	handle func(http.ResponseWriter, *http.Request)
}

// Server is the fake backend. The zero value is not usable; call New.
type Server struct {
	// URL is the base URL of the fake, such as http://127.0.0.1:12345.
	URL string

	ts     *httptest.Server
	routes map[string]route

	mu       sync.Mutex
	recorded map[string][]Recorded
	rejected map[string]bool
	failNext map[string]int
	replyNxt map[string]reply
	dropNext map[string]bool
	today    time.Time
	history  []repsolapi.HistoryEntry
	check    repsolapi.CartCheck
	agency   string
	writes   int
	held     map[string]*hold

	waylet         bool
	wayletPayAfter int
	payments       map[string]*payment
	paymentOrder   []string
	confirmedCount int
}

// reply is a canned answer armed for the next request to a path.
type reply struct {
	status int
	body   string
}

// hold parks the next request to a path until the test releases it.
type hold struct {
	entered chan struct{}
	release chan struct{}
}

// New starts the fake on its own loopback server. Close it when done.
func New() *Server {
	s := &Server{
		recorded: make(map[string][]Recorded),
		rejected: make(map[string]bool),
		failNext: make(map[string]int),
		replyNxt: make(map[string]reply),
		dropNext: make(map[string]bool),
		held:     make(map[string]*hold),
		check:    repsolapi.CartCheck{Status: "EE04", OrderType: "SOLICITUDE"},
		agency:   AgencyCode,
		waylet:   true,
		payments: make(map[string]*payment),
	}
	s.routes = map[string]route{
		"/gigya/accounts.getAccountInfo":  {http.MethodPost, s.handleAccountInfo},
		"/gigya/accounts.webSdkBootstrap": {http.MethodGet, s.handleBootstrap},
		"/gigya/accounts.login":           {http.MethodPost, s.handleLogin},

		"/places/v1/autocomplete": {http.MethodGet, s.handleAutocomplete},
		"/places/v1/geocode":      {http.MethodGet, s.handleGeocode},

		"/api/health":           {http.MethodGet, s.handleHealth},
		"/api/address-maps":     {http.MethodGet, s.handleAddressMaps},
		"/api/agency":           {http.MethodGet, s.handleAgency},
		"/api/catalog":          {http.MethodGet, s.handleCatalog},
		"/api/deliverySchedule": {http.MethodGet, s.handleDeliverySchedule},
		"/api/order-history":    {http.MethodGet, s.handleOrderHistory},

		"/api/cart/check": {http.MethodPost, s.handleCartCheck},
		"/api/solicitude": {http.MethodPost, s.handleSolicitude},
		"/api/order":      {http.MethodPost, s.handleOrder},

		"/api/cart":                     {http.MethodPost, s.handleCart},
		"/api/waylet-payment/url":       {http.MethodPost, s.handleWayletURL},
		"/api/order-history/payment-id": {http.MethodGet, s.handleOrderByPayment},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.serve)
	s.ts = httptest.NewServer(mux)
	s.URL = s.ts.URL
	return s
}

// Close shuts the fake down.
func (s *Server) Close() {
	s.ts.Close()
}

// GigyaURL returns the base URL of the fake Gigya.
func (s *Server) GigyaURL() string {
	return s.URL + "/gigya"
}

// APIURL returns the base URL of the fake Repsol API.
func (s *Server) APIURL() string {
	return s.URL + "/api"
}

// PlacesURL returns the base URL of the fake address service.
func (s *Server) PlacesURL() string {
	return s.URL + "/places"
}

// RejectToken makes the fake reject a login token. The zero value accepts
// ValidToken and RenewedToken.
func (s *Server) RejectToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rejected[token] = true
}

// SetToday sets the date the delivery schedule counts from. Without it the
// fake uses the current date in Europe/Madrid.
func (s *Server) SetToday(d time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.today = d
}

// SetHistory replaces the order history. The history is returned in the order
// given, newest first, and writes prepend to it.
func (s *Server) SetHistory(h []repsolapi.HistoryEntry) {
	history := make([]repsolapi.HistoryEntry, len(h))
	copy(history, h)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = history
}

// SetCheck replaces the cart check answer.
func (s *Server) SetCheck(c repsolapi.CartCheck) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.check = c
}

// SetAgency replaces the agency code the agency route answers. An empty code
// models an address the agency does not serve.
func (s *Server) SetAgency(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agency = code
}

// FailNext arms one failure: the next request to path gets status and an empty
// body.
func (s *Server) FailNext(path string, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNext[path] = status
}

// ReplyNext arms one canned answer: the next request to path gets status and
// body as JSON. It models answers FailNext cannot, such as the error envelope
// the API sends with HTTP 200.
func (s *Server) ReplyNext(path string, status int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replyNxt[path] = reply{status: status, body: body}
}

// DropNext arms one transport failure: the next request to path is received and
// recorded, then its connection is closed without an answer. The client sees a
// failure after it sent the request, which is the case a write must never
// retry.
func (s *Server) DropNext(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropNext[path] = true
}

// Hold parks the next request to path after it is recorded and before it is
// answered. entered is closed once that request arrives; release lets it go on
// and may be called more than once.
func (s *Server) Hold(path string) (entered <-chan struct{}, release func()) {
	h := &hold{entered: make(chan struct{}), release: make(chan struct{})}
	s.mu.Lock()
	s.held[path] = h
	s.mu.Unlock()
	var once sync.Once
	return h.entered, func() { once.Do(func() { close(h.release) }) }
}

// Requests returns the recorded requests for path, in arrival order. Path is a
// full server path such as "/api/solicitude".
func (s *Server) Requests(path string) []Recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.recorded[path]
	out := make([]Recorded, len(stored))
	copy(out, stored)
	return out
}

// Count returns how many requests path received.
func (s *Server) Count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.recorded[path])
}

// serve records every request, applies an armed failure, and dispatches the
// route. The body is buffered so the route can read it again.
func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(io.LimitReader(r.Body, maxRequestBody))
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	s.record(r, body)

	if h := s.takeHold(r.URL.Path); h != nil {
		close(h.entered)
		select {
		case <-h.release:
		case <-r.Context().Done():
			return
		}
	}

	if s.takeDrop(r.URL.Path) {
		dropConnection(w)
		return
	}
	if status, ok := s.takeFailure(r.URL.Path); ok {
		w.WriteHeader(status)
		return
	}
	if rep, ok := s.takeReply(r.URL.Path); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(rep.status)
		_, _ = w.Write([]byte(rep.body))
		return
	}

	rt, ok := s.routes[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method != rt.method {
		w.Header().Set("Allow", rt.method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rt.handle(w, r)
}

// record stores a copy of one request.
func (s *Server) record(r *http.Request, body []byte) {
	rec := Recorded{
		Method: r.Method,
		Query:  r.URL.Query(),
		Header: r.Header.Clone(),
		Body:   append([]byte(nil), body...),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recorded[r.URL.Path] = append(s.recorded[r.URL.Path], rec)
}

// takeHold consumes an armed hold for path, if there is one.
func (s *Server) takeHold(path string) *hold {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.held[path]
	delete(s.held, path)
	return h
}

// takeFailure consumes an armed failure for path, if there is one.
func (s *Server) takeFailure(path string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status, ok := s.failNext[path]
	if ok {
		delete(s.failNext, path)
	}
	return status, ok
}

// takeReply consumes an armed canned answer for path, if there is one.
func (s *Server) takeReply(path string) (reply, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rep, ok := s.replyNxt[path]
	if ok {
		delete(s.replyNxt, path)
	}
	return rep, ok
}

// takeDrop consumes an armed connection drop for path, if there is one.
func (s *Server) takeDrop(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ok := s.dropNext[path]
	delete(s.dropNext, path)
	return ok
}

// dropConnection closes the connection of a request without answering it.
func dropConnection(w http.ResponseWriter) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_ = conn.Close()
}

// todayDate returns the fake's current date in Europe/Madrid, at midnight.
func (s *Server) todayDate() time.Time {
	s.mu.Lock()
	d := s.today
	s.mu.Unlock()

	loc := madrid
	if d.IsZero() {
		d = time.Now()
	}
	d = d.In(loc)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
}

// madrid is Europe/Madrid, or UTC if the zone database is missing.
var madrid = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// writeJSON writes one JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// boolPtr returns a pointer to b, for the optional wire fields.
func boolPtr(b bool) *bool {
	return &b
}
