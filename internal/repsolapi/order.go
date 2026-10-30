package repsolapi

import (
	"context"
	"errors"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// The write endpoints. None of them is retried.
const (
	cartCheckPath  = "/cart/check"
	solicitudePath = "/solicitude"
	orderPath      = "/order"
)

// CartCheckRequest is the body of the cart validation that decides which write
// places the order. A nil ScheduleMode is sent as null, as the web does when
// the schedule has no mode.
type CartCheckRequest struct {
	ScheduleMode   *string `json:"scheduleMode" toml:"scheduleMode"`
	AgencyCode     string  `json:"agencyCode" toml:"agencyCode"`
	UserGigyaUID   string  `json:"userGigyaUid" toml:"userGigyaUid"`
	Address        Address `json:"address" toml:"address"`
	MaterialID     string  `json:"materialId" toml:"materialId"`
	Quantity       int     `json:"quantity" toml:"quantity"`
	UnitPrice      float64 `json:"unitPrice" toml:"unitPrice"`
	DeliveryDateID string  `json:"deliveryDateId" toml:"deliveryDateId"`
	DeliveryDate   string  `json:"deliveryDate" toml:"deliveryDate"`
}

// CartCheck is the cart validation result. It says which write follows: a
// solicitude when OrderType is SOLICITUDE, an order when Order is true.
type CartCheck struct {
	Status       string `json:"status" toml:"status"`
	OnlinePolicy bool   `json:"onlinePolicy" toml:"onlinePolicy"`
	Order        bool   `json:"order" toml:"order"`
	OrderType    string `json:"orderType" toml:"orderType"`
}

// SolicitudeRequest is the body of a pay-on-delivery order request. Status is
// the one the cart check returned.
type SolicitudeRequest struct {
	Address        Address `json:"address" toml:"address"`
	MaterialID     string  `json:"materialId" toml:"materialId"`
	Quantity       int     `json:"quantity" toml:"quantity"`
	UnitPrice      float64 `json:"unitPrice" toml:"unitPrice"`
	Status         string  `json:"status" toml:"status"`
	Bill           bool    `json:"bill" toml:"bill"`
	Discount       int     `json:"discount" toml:"discount"`
	DeliveryDateID string  `json:"deliveryDateId" toml:"deliveryDateId"`
	DeliveryDate   string  `json:"deliveryDate" toml:"deliveryDate"`
}

// OrderRequest is the body of the order write the web uses when the cart check
// reports Order.
type OrderRequest struct {
	Address        Address `json:"address" toml:"address"`
	MaterialID     string  `json:"materialId" toml:"materialId"`
	Quantity       int     `json:"quantity" toml:"quantity"`
	UnitPrice      float64 `json:"unitPrice" toml:"unitPrice"`
	Bill           bool    `json:"bill" toml:"bill"`
	Discount       int     `json:"discount" toml:"discount"`
	DeliveryDateID string  `json:"deliveryDateId" toml:"deliveryDateId"`
	DeliveryDate   string  `json:"deliveryDate" toml:"deliveryDate"`
}

// PlacedAgency is the agency block of a placed order.
type PlacedAgency struct {
	Name        string `json:"name" toml:"name"`
	Phone       string `json:"phone" toml:"phone"`
	RepsolPhone string `json:"repsolPhone" toml:"repsolPhone"`
}

// PlacedAddress is the address summary of a placed order.
type PlacedAddress struct {
	Province     string `json:"province" toml:"province"`
	Locality     string `json:"locality" toml:"locality"`
	Municipality string `json:"municipality" toml:"municipality"`
	Street       string `json:"street" toml:"street"`
	Number       string `json:"number" toml:"number"`
	Door         string `json:"door" toml:"door"`
	Floor        string `json:"floor" toml:"floor"`
	PostalCode   string `json:"postalCode" toml:"postalCode"`
	StreetType   string `json:"streetType" toml:"streetType"`
}

// Placed is a placed order. ExternalID is the only reference the write
// returns; the history reports the different referenceSAC instead.
type Placed struct {
	ExternalID   string        `json:"externalId" toml:"externalId"`
	DeliveryDate string        `json:"deliveryDate" toml:"deliveryDate"`
	Material     string        `json:"material" toml:"material"`
	Quantity     int           `json:"quantity" toml:"quantity"`
	UnitPrice    float64       `json:"unitPrice" toml:"unitPrice"`
	Address      PlacedAddress `json:"address" toml:"address"`
	Agency       PlacedAgency  `json:"agency" toml:"agency"`
}

// CheckCart runs the cart validation that decides which write places the
// order. It is authenticated and never retried, but it places nothing, so its
// failures keep their own category. UserGigyaUID is filled from the signature.
func (c *Client) CheckCart(ctx context.Context, r CartCheckRequest) (CartCheck, error) {
	const endpoint = cartCheckPath
	auth, err := c.sign(ctx)
	if err != nil {
		return CartCheck{}, err
	}
	r.UserGigyaUID = auth.UID
	header := authHeaders(auth)
	addTelemetry(header, c.Telemetry, endpoint)
	resp, err := doWrite(ctx, c.HTTP, c.BaseURL, endpoint, header, r)
	if err != nil {
		return CartCheck{}, err
	}
	return decodeBody[CartCheck](resp, endpoint)
}

// CreateSolicitude places a pay-on-delivery order. A failure after the request
// leaves the process becomes the outcome-unknown error: the order may have been
// placed, so it is never retried.
func (c *Client) CreateSolicitude(ctx context.Context, r SolicitudeRequest) (Placed, error) {
	return c.create(ctx, solicitudePath, r)
}

// CreateOrder places an order. A failure after the request leaves the process
// becomes the outcome-unknown error: the order may have been placed, so it is
// never retried.
func (c *Client) CreateOrder(ctx context.Context, r OrderRequest) (Placed, error) {
	return c.create(ctx, orderPath, r)
}

// create performs one final order write. It checks the context before signing
// and again before sending, so a call that never left the process returns the
// plain context error. Any failure after sending is ambiguous and is converted
// to the outcome-unknown error.
func (c *Client) create(ctx context.Context, endpoint string, body any) (Placed, error) {
	if err := ctx.Err(); err != nil {
		return Placed{}, err
	}
	auth, err := c.sign(ctx)
	if err != nil {
		return Placed{}, err
	}
	if err := ctx.Err(); err != nil {
		return Placed{}, err
	}
	header := authHeaders(auth)
	addTelemetry(header, c.Telemetry, endpoint)
	resp, err := doWrite(ctx, c.HTTP, c.BaseURL, endpoint, header, body)
	if err != nil {
		return Placed{}, orderOutcomeError(err)
	}
	placed, err := decodeBody[Placed](resp, endpoint)
	if err != nil {
		return Placed{}, orderOutcomeError(err)
	}
	if placed.ExternalID == "" {
		return Placed{}, orderOutcomeError(nil)
	}
	return placed, nil
}

// orderOutcomeError converts an ambiguous write failure into the dedicated
// outcome-unknown error. A nil cause means the response carried no external id,
// which is just as ambiguous. A refusal, a rejected session or any other
// definite error proves the order was not placed and passes through unchanged.
func orderOutcomeError(cause error) error {
	if cause != nil &&
		!errors.Is(cause, context.Canceled) &&
		!errors.Is(cause, context.DeadlineExceeded) {
		var typed *apperr.Error
		if !errors.As(cause, &typed) ||
			(typed.Category != apperr.Network && typed.Category != apperr.APIChanged) {
			return cause
		}
	}
	return apperr.New(apperr.OutcomeUnknown, "order_outcome_unknown",
		"the order request failed after it was sent; it may or may not have been placed").
		WithHint(`run "repsol order last" and check before ordering again`)
}
