package repsolapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// The endpoints of the Waylet payment flow.
const (
	temporaryOrderPath = "/cart"
	wayletURLPath      = "/waylet-payment/url"
	orderByPaymentPath = "/order-history/payment-id"
)

// TemporaryOrderRequest is the body of the cart the web saves before it sends
// the customer to pay. A nil ScheduleMode is sent as null.
type TemporaryOrderRequest struct {
	ScheduleMode   *string `json:"scheduleMode" toml:"scheduleMode"`
	AgencyCode     string  `json:"agencyCode" toml:"agencyCode"`
	UserGigyaUID   string  `json:"userGigyaUid" toml:"userGigyaUid"`
	Address        Address `json:"address" toml:"address"`
	MaterialID     string  `json:"materialId" toml:"materialId"`
	Quantity       int     `json:"quantity" toml:"quantity"`
	UnitPrice      float64 `json:"unitPrice" toml:"unitPrice"`
	Bill           bool    `json:"bill" toml:"bill"`
	Discount       int     `json:"discount" toml:"discount"`
	Tip            float64 `json:"tip" toml:"tip"`
	DeliveryDateID string  `json:"deliveryDateId" toml:"deliveryDateId"`
	DeliveryDate   string  `json:"deliveryDate" toml:"deliveryDate"`
}

// TemporaryOrder is the saved cart. PaymentID names the payment the cart is
// waiting for; it is the key of every later call of the flow.
type TemporaryOrder struct {
	PaymentID string `json:"paymentId" toml:"paymentId"`
}

// wayletURLRequest is the body of the Waylet URL request.
type wayletURLRequest struct {
	PaymentID string `json:"paymentId"`
}

// wayletURLResponse is the answer to the Waylet URL request.
type wayletURLResponse struct {
	RedirectionURL string `json:"wayletRedirectionPaymentUrl"`
}

// PaymentOrder is the order a payment produced. The API answers it only once
// the payment went through, and the web fails on an answer without an agency or
// an address, so those two together are the confirmation (see Confirmed). The
// external id is not part of it: the web reads it as externalId || "", so an
// order can be confirmed without one. Only the fields the CLI reports are
// decoded; the address and the agency are kept as pointers to tell an absent
// object from an empty one.
type PaymentOrder struct {
	ExternalID   FlexNumber     `json:"externalId" toml:"externalId"`
	DeliveryDate string         `json:"deliveryDate" toml:"deliveryDate"`
	Address      *PlacedAddress `json:"address" toml:"address"`
	Agency       *PlacedAgency  `json:"agency" toml:"agency"`
}

// Confirmed reports whether the answer is the order of a paid payment: it
// carries the agency and address objects the web needs to show the order, as
// the web's own check does, whether or not it has an external id. Any other
// non-empty object is not a confirmation.
func (o PaymentOrder) Confirmed() bool {
	return o.Agency != nil && o.Address != nil
}

// CreateTemporaryOrder saves the cart that a payment will turn into an order.
// It is authenticated and never retried, but it places nothing: no order exists
// until the payment link is paid. Its failures keep their own category.
// UserGigyaUID is filled from the signature.
func (c *Client) CreateTemporaryOrder(ctx context.Context, r TemporaryOrderRequest) (TemporaryOrder, error) {
	const endpoint = temporaryOrderPath
	auth, err := c.sign(ctx)
	if err != nil {
		return TemporaryOrder{}, err
	}
	r.UserGigyaUID = auth.UID
	header := authHeaders(auth)
	addTelemetry(header, c.Telemetry, endpoint)
	resp, err := doWrite(ctx, c.HTTP, c.BaseURL, endpoint, header, r)
	if err != nil {
		return TemporaryOrder{}, err
	}
	temp, err := decodeBody[TemporaryOrder](resp, endpoint)
	if err != nil {
		return TemporaryOrder{}, err
	}
	if temp.PaymentID == "" {
		return TemporaryOrder{}, apiError(apperr.APIChanged, "unexpected_response",
			"the cart answer carries no payment id", resp, endpoint)
	}
	return temp, nil
}

// WayletURL asks for the Waylet page that pays one temporary order. It is
// authenticated and never retried. The URL must be an https URL: the CLI hands
// it to the owner to open, so anything else is refused as a changed API.
func (c *Client) WayletURL(ctx context.Context, paymentID string) (string, error) {
	const endpoint = wayletURLPath
	auth, err := c.sign(ctx)
	if err != nil {
		return "", err
	}
	header := authHeaders(auth)
	addTelemetry(header, c.Telemetry, endpoint)
	resp, err := doWrite(ctx, c.HTTP, c.BaseURL, endpoint, header, wayletURLRequest{PaymentID: paymentID})
	if err != nil {
		return "", err
	}
	out, err := decodeBody[wayletURLResponse](resp, endpoint)
	if err != nil {
		return "", err
	}
	if u, err := url.Parse(out.RedirectionURL); err != nil || u.Scheme != "https" || u.Host == "" {
		return "", apiError(apperr.APIChanged, "unexpected_response",
			"the Waylet answer carries no https payment URL", resp, endpoint)
	}
	return out.RedirectionURL, nil
}

// WayletPaymentID returns the id a Waylet payment is looked up under, read from
// the payment URL: the part of the token query parameter before its first
// colon. The URL looks like
//
//	https://open.waylet.es?token=W00001623288:1796519223591:<hash>&id=<id>
//
// and the id is W00001623288, while the cart answered 000001623288 and the
// lookup answers 404 to that one. It returns "" when the URL carries no such
// token, and the caller falls back to the id of the cart.
func WayletPaymentID(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	id, _, _ := strings.Cut(u.Query().Get("token"), ":")
	return strings.TrimSpace(id)
}

// OrderByPaymentID looks up the order a payment produced. It is a safe read
// and keeps the read retries. found is false while the payment is not
// confirmed: the web treats any failure of this call as "not yet" and asks
// again, and the CLI does the same for a 404 and every other 4xx but an
// authentication failure (401, 403), for the error envelope the API sends with
// HTTP 200, and for a null body, an empty object or an answer that is not a
// confirmation (see PaymentOrder.Confirmed). Every other failure is reported.
func (c *Client) OrderByPaymentID(ctx context.Context, paymentID string) (PaymentOrder, bool, error) {
	return c.lookupPayment(ctx, paymentID, true)
}

// PollOrderByPaymentID is OrderByPaymentID for a payment wait: it sends one
// request and never retries, because the wait loop is the retry and has its own
// deadline, which three attempts of the transport timeout would overrun.
func (c *Client) PollOrderByPaymentID(ctx context.Context, paymentID string) (PaymentOrder, bool, error) {
	return c.lookupPayment(ctx, paymentID, false)
}

// lookupPayment is the lookup behind OrderByPaymentID and PollOrderByPaymentID.
func (c *Client) lookupPayment(ctx context.Context, paymentID string, retry bool) (PaymentOrder, bool, error) {
	const endpoint = orderByPaymentPath
	auth, err := c.sign(ctx)
	if err != nil {
		return PaymentOrder{}, false, err
	}
	resp, err := c.signedRead(ctx, auth, endpoint, retry, param("paymentId", paymentID))
	if err != nil {
		if notYet(err) {
			return PaymentOrder{}, false, nil
		}
		return PaymentOrder{}, false, err
	}
	body := bytes.TrimSpace(resp.Body)
	if len(body) == 0 || string(body) == "null" || string(body) == "{}" {
		return PaymentOrder{}, false, nil
	}
	order, err := decodeBody[PaymentOrder](resp, endpoint)
	if err != nil {
		if notYet(err) {
			return PaymentOrder{}, false, nil
		}
		return PaymentOrder{}, false, err
	}
	if !order.Confirmed() {
		return PaymentOrder{}, false, nil
	}
	return order, true, nil
}

// notYet reports whether a failure of the payment lookup means "the payment is
// not confirmed yet": a 4xx that is no authentication failure, or the error
// envelope of a 200 answer. A 401 or 403 is a real failure, and so is a
// transport or server failure, which the caller handles on its own.
func notYet(err error) bool {
	var typed *apperr.Error
	if !errors.As(err, &typed) {
		return false
	}
	switch {
	case typed.Code == "api_error":
		return true
	case typed.HTTPStatus == http.StatusUnauthorized, typed.HTTPStatus == http.StatusForbidden:
		return false
	default:
		return typed.HTTPStatus >= 400 && typed.HTTPStatus < 500
	}
}
