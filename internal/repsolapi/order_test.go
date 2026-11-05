package repsolapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// capturedRequest is one request recorded by a fake server.
type capturedRequest struct {
	method string
	path   string
	query  url.Values
	header http.Header
	body   []byte
}

// capture records the requests a fake server receives. Handlers run on another
// goroutine, so every access goes through the mutex.
type capture struct {
	mu   sync.Mutex
	reqs []capturedRequest
}

// record stores one request, body included.
func (c *capture) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reqs = append(c.reqs, capturedRequest{
		method: r.Method,
		path:   r.URL.Path,
		query:  r.URL.Query(),
		header: r.Header.Clone(),
		body:   body,
	})
}

// all returns a snapshot of the recorded requests.
func (c *capture) all() []capturedRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]capturedRequest(nil), c.reqs...)
}

// count returns how many requests the server received.
func (c *capture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.reqs)
}

// first returns the first recorded request for path.
func (c *capture) first(t *testing.T, path string) capturedRequest {
	t.Helper()
	for _, req := range c.all() {
		if req.path == path {
			return req
		}
	}
	t.Fatalf("no request to %s; the server received %d requests", path, c.count())
	return capturedRequest{}
}

// decodeObject decodes a JSON request body into a map.
func decodeObject(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode request body %s: %v", body, err)
	}
	return m
}

// testAddress is the synthetic address every write test sends.
func testAddress() Address {
	return Address{
		Province:         "Madrid",
		ProvinceCode:     "28",
		Locality:         "Móstoles",
		LocalityCode:     "28000000001",
		StreetType:       "Calle",
		StreetTypeCode:   "1",
		Street:           "Ñandú",
		Municipality:     "Móstoles",
		MunicipalityCode: "28000000001",
		Number:           "3",
		AgencyCode:       "0000000001",
		PlaceID:          "fixture-place",
		Latitude:         "40.4168",
		Longitude:        "-3.7038",
		Origin:           "google",
		FullAddress:      "Calle Ñandú 3, 28931 Móstoles, Madrid",
		PostalCode:       "28931",
	}
}

// assertWriteHeaders checks the five telemetry headers of one write request.
func assertWriteHeaders(t *testing.T, req capturedRequest) {
	t.Helper()
	processID := req.header.Get("ptb-order-process-id")
	if len(processID) != 21 || !regexp.MustCompile(`^[A-Za-z0-9_-]{21}$`).MatchString(processID) {
		t.Errorf("ptb-order-process-id = %q, want 21 URL-safe characters", processID)
	}
	userID := req.header.Get("ptb-app-generated-user-id")
	if !regexp.MustCompile(`^us:[A-Za-z0-9_-]{48}$`).MatchString(userID) {
		t.Errorf("ptb-app-generated-user-id = %q, want us: followed by 48 URL-safe characters", userID)
	}
	requestID := req.header.Get("ptb-appinsights-request")
	wantRequestID := regexp.MustCompile(`^rq:` + regexp.QuoteMeta(req.path) + `:[A-Za-z0-9_-]{21}$`)
	if !wantRequestID.MatchString(requestID) {
		t.Errorf("ptb-appinsights-request = %q, want a match for %s", requestID, wantRequestID)
	}
	for _, name := range []string{"ptb-appinsights-session", "ptb-appinsights-user"} {
		if got := req.header.Get(name); !regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`).MatchString(got) {
			t.Errorf("header %s = %q, want 22 URL-safe characters", name, got)
		}
	}
}

func TestWriteHeadersAndBodies(t *testing.T) {
	checkBody := []byte(`{"status":"EE04","onlinePolicy":false,"order":false,"orderType":"SOLICITUDE"}`)
	solicitudeBody := fixtureBytes(t, "testdata/solicitude.json")
	var rec capture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		switch r.URL.Path {
		case "/cart/check":
			writeJSONBody(w, checkBody)
		case "/solicitude":
			writeJSONBody(w, solicitudeBody)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{
		BaseURL:   srv.URL,
		HTTP:      testHTTP(t, srv),
		Signer:    stubSigner{},
		Telemetry: NewTelemetry(),
	}

	mode := "NO_SCHEDULE"
	check, err := c.CheckCart(context.Background(), CartCheckRequest{
		ScheduleMode:   &mode,
		AgencyCode:     "0000000001",
		Address:        testAddress(),
		MaterialID:     "153",
		Quantity:       1,
		UnitPrice:      18.89,
		DeliveryDateID: "1",
		DeliveryDate:   "2026-11-05",
	})
	if err != nil {
		t.Fatalf("CheckCart: %v", err)
	}
	if check.Status != "EE04" || check.OnlinePolicy || check.Order || check.OrderType != "SOLICITUDE" {
		t.Errorf("CartCheck = %#v", check)
	}

	placed, err := c.CreateSolicitude(context.Background(), SolicitudeRequest{
		Address:        testAddress(),
		MaterialID:     "153",
		Quantity:       1,
		UnitPrice:      18.89,
		Status:         "EE04",
		Bill:           false,
		Discount:       0,
		DeliveryDateID: "1",
		DeliveryDate:   "2026-11-05",
	})
	if err != nil {
		t.Fatalf("CreateSolicitude: %v", err)
	}
	if placed.ExternalID != "800000000000000001" {
		t.Errorf("ExternalID = %q, want 800000000000000001", placed.ExternalID)
	}

	reqs := rec.all()
	if len(reqs) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(reqs))
	}
	checkReq := rec.first(t, "/cart/check")
	solicitudeReq := rec.first(t, "/solicitude")
	for _, req := range []capturedRequest{checkReq, solicitudeReq} {
		if req.method != http.MethodPost {
			t.Errorf("%s method = %s, want POST", req.path, req.method)
		}
	}
	assertWriteHeaders(t, checkReq)
	assertWriteHeaders(t, solicitudeReq)

	if got, want := checkReq.header.Get("ptb-order-process-id"), solicitudeReq.header.Get("ptb-order-process-id"); got != want {
		t.Errorf("ptb-order-process-id changed between writes: %q then %q", got, want)
	}

	cart := decodeObject(t, checkReq.body)
	for key, want := range map[string]any{
		"scheduleMode":   "NO_SCHEDULE",
		"userGigyaUid":   "U1",
		"materialId":     "153",
		"quantity":       float64(1),
		"unitPrice":      18.89,
		"deliveryDateId": "1",
		"deliveryDate":   "2026-11-05",
	} {
		if got := cart[key]; got != want {
			t.Errorf("cart/check body %s = %#v, want %#v", key, got, want)
		}
	}
	addr, ok := cart["address"].(map[string]any)
	if !ok {
		t.Fatalf("cart/check body address = %#v, want an object", cart["address"])
	}
	if got := addr["latitude"]; got != float64(40.4168) {
		t.Errorf("cart/check body address.latitude = %#v, want 40.4168", got)
	}

	solicitude := decodeObject(t, solicitudeReq.body)
	for key, want := range map[string]any{
		"status":   "EE04",
		"bill":     false,
		"discount": float64(0),
	} {
		if got := solicitude[key]; got != want {
			t.Errorf("solicitude body %s = %#v, want %#v", key, got, want)
		}
	}
	if _, ok := solicitude["userGigyaUid"]; ok {
		t.Error("solicitude body contains userGigyaUid, want it omitted")
	}
}

func TestCartCheckNullScheduleMode(t *testing.T) {
	var rec capture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		writeJSONBody(w, []byte(`{"status":"EE04"}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: testHTTP(t, srv), Signer: stubSigner{}, Telemetry: NewTelemetry()}
	if _, err := c.CheckCart(context.Background(), CartCheckRequest{
		ScheduleMode:   nil,
		AgencyCode:     "0000000001",
		Address:        testAddress(),
		MaterialID:     "153",
		Quantity:       1,
		UnitPrice:      18.89,
		DeliveryDateID: "1",
		DeliveryDate:   "2026-11-05",
	}); err != nil {
		t.Fatalf("CheckCart: %v", err)
	}

	body := rec.first(t, "/cart/check").body
	if !strings.Contains(string(body), `"scheduleMode":null`) {
		t.Errorf("cart/check body %s does not contain \"scheduleMode\":null", body)
	}
}

func TestWriteOutcomeUnknown(t *testing.T) {
	successBody := []byte(`{"externalId":"800000000000000001"}`)
	cases := []struct {
		name   string
		handle func(http.ResponseWriter, *http.Request)
	}{
		{
			name: "http_502",
			handle: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			},
		},
		{
			name: "garbled_body",
			handle: func(w http.ResponseWriter, r *http.Request) {
				writeJSONBody(w, []byte("<html>"))
			},
		},
		{
			name: "empty_external_id",
			handle: func(w http.ResponseWriter, r *http.Request) {
				writeJSONBody(w, []byte(`{"externalId":""}`))
			},
		},
		{
			name: "timeout",
			handle: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(200 * time.Millisecond)
				writeJSONBody(w, successBody)
			},
		},
		{
			name: "cancelled_context",
			handle: func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(200 * time.Millisecond)
				writeJSONBody(w, successBody)
			},
		},
	}
	writes := []struct {
		name string
		call func(context.Context, *Client) (Placed, error)
	}{
		{
			name: "solicitude",
			call: func(ctx context.Context, c *Client) (Placed, error) {
				return c.CreateSolicitude(ctx, SolicitudeRequest{
					Address:        testAddress(),
					MaterialID:     "153",
					Quantity:       1,
					UnitPrice:      18.89,
					Status:         "EE04",
					DeliveryDateID: "1",
					DeliveryDate:   "2026-11-05",
				})
			},
		},
		{
			name: "order",
			call: func(ctx context.Context, c *Client) (Placed, error) {
				return c.CreateOrder(ctx, OrderRequest{
					Address:        testAddress(),
					MaterialID:     "153",
					Quantity:       1,
					UnitPrice:      18.89,
					Bill:           false,
					Discount:       0,
					DeliveryDateID: "1",
					DeliveryDate:   "2026-11-05",
				})
			},
		},
	}

	for _, write := range writes {
		write := write
		t.Run(write.name, func(t *testing.T) {
			for _, tc := range cases {
				tc := tc
				t.Run(tc.name, func(t *testing.T) {
					var rec capture
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						rec.record(r)
						tc.handle(w, r)
					}))
					defer srv.Close()

					c := &Client{
						BaseURL:   srv.URL,
						HTTP:      testHTTP(t, srv),
						Signer:    stubSigner{},
						Telemetry: NewTelemetry(),
					}
					ctx := context.Background()
					switch tc.name {
					case "timeout":
						c.HTTP.HTTP.Timeout = 50 * time.Millisecond
					case "cancelled_context":
						var cancel context.CancelFunc
						ctx, cancel = context.WithCancel(ctx)
						timer := time.AfterFunc(20*time.Millisecond, cancel)
						defer timer.Stop()
						defer cancel()
					}

					_, err := write.call(ctx, c)
					e := requireAppError(t, err, apperr.OutcomeUnknown, "order_outcome_unknown")
					if !strings.Contains(e.Hint, "repsol order last") {
						t.Errorf("hint = %q, want it to mention repsol order last", e.Hint)
					}
					if got := rec.count(); got != 1 {
						t.Errorf("server saw %d requests, want exactly 1", got)
					}
				})
			}
		})
	}
}

func TestWriteRejections(t *testing.T) {
	cases := []struct {
		name     string
		handle   func(http.ResponseWriter, *http.Request)
		category apperr.Category
		code     string
	}{
		{
			name: "body_error",
			handle: func(w http.ResponseWriter, r *http.Request) {
				writeJSONBody(w, []byte(`{"error":true,"errorCode":"X","message":"no"}`))
			},
			category: apperr.Refused,
			code:     "api_error",
		},
		{
			name: "unauthorized",
			handle: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			},
			category: apperr.Auth,
			code:     "unauthorized",
		},
	}
	writes := []struct {
		name string
		call func(context.Context, *Client) (Placed, error)
	}{
		{
			name: "solicitude",
			call: func(ctx context.Context, c *Client) (Placed, error) {
				return c.CreateSolicitude(ctx, SolicitudeRequest{
					Address:        testAddress(),
					MaterialID:     "153",
					Quantity:       1,
					UnitPrice:      18.89,
					Status:         "EE04",
					DeliveryDateID: "1",
					DeliveryDate:   "2026-11-05",
				})
			},
		},
		{
			name: "order",
			call: func(ctx context.Context, c *Client) (Placed, error) {
				return c.CreateOrder(ctx, OrderRequest{
					Address:        testAddress(),
					MaterialID:     "153",
					Quantity:       1,
					UnitPrice:      18.89,
					Bill:           false,
					Discount:       0,
					DeliveryDateID: "1",
					DeliveryDate:   "2026-11-05",
				})
			},
		},
	}

	for _, write := range writes {
		write := write
		t.Run(write.name, func(t *testing.T) {
			for _, tc := range cases {
				tc := tc
				t.Run(tc.name, func(t *testing.T) {
					var rec capture
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						rec.record(r)
						tc.handle(w, r)
					}))
					defer srv.Close()

					c := &Client{
						BaseURL:   srv.URL,
						HTTP:      testHTTP(t, srv),
						Signer:    stubSigner{},
						Telemetry: NewTelemetry(),
					}
					_, err := write.call(context.Background(), c)
					requireAppError(t, err, tc.category, tc.code)
					if got := rec.count(); got != 1 {
						t.Errorf("server saw %d requests, want exactly 1", got)
					}
				})
			}
		})
	}
}

func TestCheckCartNotRetried(t *testing.T) {
	var rec capture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	mode := "NO_SCHEDULE"
	c := &Client{BaseURL: srv.URL, HTTP: testHTTP(t, srv), Signer: stubSigner{}, Telemetry: NewTelemetry()}
	_, err := c.CheckCart(context.Background(), CartCheckRequest{
		ScheduleMode:   &mode,
		AgencyCode:     "0000000001",
		Address:        testAddress(),
		MaterialID:     "153",
		Quantity:       1,
		UnitPrice:      18.89,
		DeliveryDateID: "1",
		DeliveryDate:   "2026-11-05",
	})
	requireAppError(t, err, apperr.Network, "server_error")
	if got := rec.count(); got != 1 {
		t.Errorf("server saw %d requests, want exactly 1", got)
	}
}

func TestCartCheckDecodesTheOnlinePaymentFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONBody(w, []byte(`{"status":"EE04","order":true,"orderType":"ORDER",`+
			`"billable":true,"blockedPolicy":true,"isCalendarDeliveryWithoutDate":true,"inspectionExpired":true}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: testHTTP(t, srv), Signer: stubSigner{}, Telemetry: NewTelemetry()}

	check, err := c.CheckCart(context.Background(), CartCheckRequest{
		AgencyCode: "0000000001", Address: testAddress(), MaterialID: "153", Quantity: 1,
		UnitPrice: 18.89, DeliveryDateID: "1", DeliveryDate: "2026-11-05",
	})
	if err != nil {
		t.Fatalf("CheckCart: %v", err)
	}
	if !check.Billable || !check.BlockedPolicy || !check.IsCalendarDeliveryWithoutDate || !check.InspectionExpired {
		t.Errorf("CartCheck = %#v, want the four online-payment fields decoded", check)
	}
}
