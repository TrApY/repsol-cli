package repsolapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// testTemporaryOrder is the synthetic cart every payment test sends.
func testTemporaryOrder() TemporaryOrderRequest {
	mode := "NO_SCHEDULE"
	return TemporaryOrderRequest{
		ScheduleMode:   &mode,
		AgencyCode:     "0000000001",
		Address:        testAddress(),
		MaterialID:     "153",
		Quantity:       2,
		UnitPrice:      18.89,
		DeliveryDateID: "1",
		DeliveryDate:   "2026-11-05",
	}
}

func TestWayletWritesHeadersAndBodies(t *testing.T) {
	var rec capture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		switch r.URL.Path {
		case "/cart":
			writeJSONBody(w, []byte(`{"paymentId":"fixture-payment-1"}`))
		case "/waylet-payment/url":
			writeJSONBody(w, []byte(`{"wayletRedirectionPaymentUrl":"https://waylet.example.test/pay?id=1"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: testHTTP(t, srv), Signer: stubSigner{}, Telemetry: NewTelemetry()}

	temp, err := c.CreateTemporaryOrder(context.Background(), testTemporaryOrder())
	if err != nil {
		t.Fatalf("CreateTemporaryOrder: %v", err)
	}
	if temp.PaymentID != "fixture-payment-1" {
		t.Errorf("PaymentID = %q, want fixture-payment-1", temp.PaymentID)
	}
	link, err := c.WayletURL(context.Background(), temp.PaymentID)
	if err != nil {
		t.Fatalf("WayletURL: %v", err)
	}
	if link != "https://waylet.example.test/pay?id=1" {
		t.Errorf("WayletURL = %q", link)
	}

	cartReq := rec.first(t, "/cart")
	urlReq := rec.first(t, "/waylet-payment/url")
	for _, req := range []capturedRequest{cartReq, urlReq} {
		if req.method != http.MethodPost {
			t.Errorf("%s method = %s, want POST", req.path, req.method)
		}
		assertWriteHeaders(t, req)
	}

	cart := decodeObject(t, cartReq.body)
	for key, want := range map[string]any{
		"scheduleMode":   "NO_SCHEDULE",
		"agencyCode":     "0000000001",
		"userGigyaUid":   "U1",
		"materialId":     "153",
		"quantity":       float64(2),
		"unitPrice":      18.89,
		"bill":           false,
		"discount":       float64(0),
		"tip":            float64(0),
		"deliveryDateId": "1",
		"deliveryDate":   "2026-11-05",
	} {
		if got := cart[key]; got != want {
			t.Errorf("cart body %s = %#v, want %#v", key, got, want)
		}
	}
	if _, ok := cart["address"].(map[string]any); !ok {
		t.Errorf("cart body address = %#v, want an object", cart["address"])
	}

	if got := decodeObject(t, urlReq.body)["paymentId"]; got != "fixture-payment-1" {
		t.Errorf("waylet url body paymentId = %#v, want fixture-payment-1", got)
	}
}

func TestWayletWritesAreNeverRetried(t *testing.T) {
	calls := map[string]func(*Client) error{
		"/cart": func(c *Client) error {
			_, err := c.CreateTemporaryOrder(context.Background(), testTemporaryOrder())
			return err
		},
		"/waylet-payment/url": func(c *Client) error {
			_, err := c.WayletURL(context.Background(), "fixture-payment-1")
			return err
		},
	}
	for path, call := range calls {
		t.Run(path, func(t *testing.T) {
			var rec capture
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec.record(r)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer srv.Close()

			c := &Client{BaseURL: srv.URL, HTTP: testHTTP(t, srv), Signer: stubSigner{}, Telemetry: NewTelemetry()}
			requireAppError(t, call(c), apperr.Network, "server_error")
			if got := rec.count(); got != 1 {
				t.Errorf("server saw %d requests, want exactly 1", got)
			}
		})
	}
}

func TestWayletResponsesAreValidated(t *testing.T) {
	cases := []struct {
		name string
		path string
		body string
		call func(*Client) error
		code string
	}{
		{
			name: "cart_without_payment_id",
			body: `{"paymentId":""}`,
			call: func(c *Client) error {
				_, err := c.CreateTemporaryOrder(context.Background(), testTemporaryOrder())
				return err
			},
			code: "unexpected_response",
		},
		{
			name: "url_missing",
			body: `{}`,
			call: func(c *Client) error {
				_, err := c.WayletURL(context.Background(), "p")
				return err
			},
			code: "unexpected_response",
		},
		{
			name: "url_not_https",
			body: `{"wayletRedirectionPaymentUrl":"javascript:alert(1)"}`,
			call: func(c *Client) error {
				_, err := c.WayletURL(context.Background(), "p")
				return err
			},
			code: "unexpected_response",
		},
		{
			name: "url_plain_http",
			body: `{"wayletRedirectionPaymentUrl":"http://waylet.example.test/pay"}`,
			call: func(c *Client) error {
				_, err := c.WayletURL(context.Background(), "p")
				return err
			},
			code: "unexpected_response",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSONBody(w, []byte(tc.body))
			}))
			defer srv.Close()
			c := &Client{BaseURL: srv.URL, HTTP: testHTTP(t, srv), Signer: stubSigner{}, Telemetry: NewTelemetry()}
			requireAppError(t, tc.call(c), apperr.APIChanged, tc.code)
		})
	}
}

// confirmedBody is the lookup answer of a paid payment, with the fields the web
// needs.
const confirmedBody = `{"externalId":800000000000000009,"deliveryDate":"2026-11-05",` +
	`"address":{"street":"Calle Falsa","number":"1"},` +
	`"agency":{"name":"Agencia Test","phone":"900 000 000","agencyCode":"0000000001"}}`

func TestOrderByPaymentID(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		wantFound bool
		wantID    string
		wantDate  string
	}{
		{
			name:      "confirmed",
			status:    http.StatusOK,
			body:      confirmedBody,
			wantFound: true,
			wantID:    "800000000000000009",
			wantDate:  "2026-11-05",
		},
		{name: "not_found_yet", status: http.StatusNotFound},
		{name: "null_body", status: http.StatusOK, body: `null`},
		{name: "empty_body", status: http.StatusOK, body: ``},
		{name: "empty_object", status: http.StatusOK, body: `{}`},
		// The web reads the agency and the address of the answer and fails
		// without them, but it accepts an empty external id (externalId || ""),
		// so an answer with the two objects is a confirmation whatever its id.
		{name: "no_external_id", status: http.StatusOK,
			body:      `{"deliveryDate":"2026-11-05","address":{"street":"x"},"agency":{"name":"A"}}`,
			wantFound: true, wantDate: "2026-11-05"},
		{name: "empty_external_id", status: http.StatusOK,
			body:      `{"externalId":"","deliveryDate":"2026-11-05","address":{"street":"x"},"agency":{"name":"A"}}`,
			wantFound: true, wantDate: "2026-11-05"},
		{name: "null_external_id", status: http.StatusOK,
			body:      `{"externalId":null,"deliveryDate":"2026-11-05","address":{"street":"x"},"agency":{"name":"A"}}`,
			wantFound: true, wantDate: "2026-11-05"},
		{name: "no_agency", status: http.StatusOK,
			body: `{"externalId":8,"deliveryDate":"2026-11-05","address":{"street":"x"}}`},
		{name: "no_address", status: http.StatusOK,
			body: `{"externalId":8,"deliveryDate":"2026-11-05","agency":{"name":"A"}}`},
		// Any other failure of the lookup that is not an authentication
		// failure means "not yet" for the web, and so for the CLI.
		{name: "bad_request_yet", status: http.StatusBadRequest},
		{name: "conflict_yet", status: http.StatusConflict},
		{name: "unprocessable_yet", status: http.StatusUnprocessableEntity},
		{name: "too_many_requests_yet", status: http.StatusTooManyRequests},
		{name: "error_envelope_yet", status: http.StatusOK,
			body: `{"error":true,"message":"payment not found","errorCode":"PTB-404"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var rec capture
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec.record(r)
				if tc.status != http.StatusOK {
					w.WriteHeader(tc.status)
					return
				}
				writeJSONBody(w, []byte(tc.body))
			}))
			defer srv.Close()

			c := &Client{BaseURL: srv.URL, HTTP: testHTTP(t, srv), Signer: stubSigner{}}
			order, found, err := c.OrderByPaymentID(context.Background(), "fixture-payment-1")
			if err != nil {
				t.Fatalf("OrderByPaymentID: %v", err)
			}
			if found != tc.wantFound {
				t.Fatalf("found = %v, want %v", found, tc.wantFound)
			}
			if got := string(order.ExternalID); got != tc.wantID {
				t.Errorf("ExternalID = %q, want %q", got, tc.wantID)
			}
			if order.DeliveryDate != tc.wantDate {
				t.Errorf("DeliveryDate = %q, want %q", order.DeliveryDate, tc.wantDate)
			}

			req := rec.first(t, "/order-history/payment-id")
			if req.method != http.MethodGet {
				t.Errorf("method = %s, want GET", req.method)
			}
			if got := req.query.Get("paymentId"); got != "fixture-payment-1" {
				t.Errorf("paymentId query = %q, want fixture-payment-1", got)
			}
		})
	}
}

func TestOrderByPaymentIDReportsRealFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   apperr.Category
	}{
		{"unauthorized", http.StatusUnauthorized, apperr.Auth},
		{"forbidden", http.StatusForbidden, apperr.Blocked},
		{"server_error", http.StatusInternalServerError, apperr.Network},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			c := &Client{BaseURL: srv.URL, HTTP: testHTTP(t, srv), Signer: stubSigner{}}
			_, found, err := c.OrderByPaymentID(context.Background(), "p")
			if found {
				t.Errorf("found = true on a %d", tc.status)
			}
			if got := apperr.From(err).Category; got != tc.want {
				t.Errorf("category = %s, want %s (err: %v)", got, tc.want, err)
			}
		})
	}
}

func TestPollOrderByPaymentIDDoesNotRetry(t *testing.T) {
	var polls, lookups capture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("paymentId") == "poll" {
			polls.record(r)
		} else {
			lookups.record(r)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	h := testHTTP(t, srv)
	h.Backoff = func(int) time.Duration { return 0 }
	c := &Client{BaseURL: srv.URL, HTTP: h, Signer: stubSigner{}}

	// The wait loop is the retry of a payment wait, so one poll is one request.
	_, _, err := c.PollOrderByPaymentID(context.Background(), "poll")
	requireAppError(t, err, apperr.Network, "server_error")
	if got := len(polls.all()); got != 1 {
		t.Errorf("a poll sent %d requests, want exactly 1", got)
	}

	// The plain lookup keeps the read retries.
	_, _, err = c.OrderByPaymentID(context.Background(), "lookup")
	requireAppError(t, err, apperr.Network, "server_error")
	if got := len(lookups.all()); got != 3 {
		t.Errorf("a lookup sent %d requests, want its 3 attempts", got)
	}
}
