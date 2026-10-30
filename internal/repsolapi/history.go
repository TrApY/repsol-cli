package repsolapi

import (
	"context"
	"strconv"
	"time"
)

// orderHistoryPath is the endpoint of the past orders.
const orderHistoryPath = "/order-history"

// HistoryAgency is the agency block of one history entry.
type HistoryAgency struct {
	Name        string `json:"name" toml:"name"`
	Phone       string `json:"phone" toml:"phone"`
	RepsolPhone string `json:"repsolPhone" toml:"repsolPhone"`
	AgencyCode  string `json:"agencyCode" toml:"agencyCode"`
}

// HistoryAddress is the partial address block of one history entry. The API
// keeps the text in Details and often leaves Street and Number empty, so a
// history address cannot rebuild the address object an order needs.
type HistoryAddress struct {
	Province         string `json:"province" toml:"province"`
	ProvinceCode     string `json:"provinceCode" toml:"provinceCode"`
	Locality         string `json:"locality" toml:"locality"`
	LocalityCode     string `json:"localityCode" toml:"localityCode"`
	StreetType       string `json:"streetType" toml:"streetType"`
	Street           string `json:"street" toml:"street"`
	StreetTypeCode   string `json:"streetTypeCode" toml:"streetTypeCode"`
	StreetCode       string `json:"streetCode" toml:"streetCode"`
	Municipality     string `json:"municipality" toml:"municipality"`
	MunicipalityCode string `json:"municipalityCode" toml:"municipalityCode"`
	Number           string `json:"number" toml:"number"`
	Floor            string `json:"floor" toml:"floor"`
	Door             string `json:"door" toml:"door"`
	PostalCode       string `json:"postalCode" toml:"postalCode"`
	Details          string `json:"details" toml:"details"`
}

// HistoryEntry is one past order. The API reports no status: a delivery is
// pending when DeliveryDate is today or later in Europe/Madrid.
type HistoryEntry struct {
	CreatedAt     string         `json:"createdAt" toml:"createdAt"`
	DeliveryDate  string         `json:"deliveryDate" toml:"deliveryDate"`
	ReferenceSAC  string         `json:"referenceSAC" toml:"referenceSAC"`
	MaterialID    string         `json:"materialId" toml:"materialId"`
	Quantity      int            `json:"quantity" toml:"quantity"`
	UnitPrice     float64        `json:"unitPrice" toml:"unitPrice"`
	Discount      *float64       `json:"discount" toml:"discount"`
	PaymentMethod *string        `json:"paymentMethod" toml:"paymentMethod"`
	Bill          bool           `json:"bill" toml:"bill"`
	Agency        HistoryAgency  `json:"agency" toml:"agency"`
	Address       HistoryAddress `json:"address" toml:"address"`
}

// OrderHistory returns the newest orders first. limit and offset are sent as
// given, and paramToAvoidCache carries the current Unix milliseconds.
func (c *Client) OrderHistory(ctx context.Context, limit, offset int) ([]HistoryEntry, error) {
	const endpoint = orderHistoryPath
	auth, err := c.sign(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := c.signedGET(ctx, auth, endpoint,
		param("limit", strconv.Itoa(limit)),
		param("offset", strconv.Itoa(offset)),
		param("paramToAvoidCache", strconv.FormatInt(time.Now().UnixMilli(), 10)),
	)
	if err != nil {
		return nil, err
	}
	return decodeBody[[]HistoryEntry](resp, endpoint)
}
