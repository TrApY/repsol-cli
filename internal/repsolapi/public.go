package repsolapi

import (
	"bytes"
	"context"
	"strings"
)

const (
	healthPath      = "/health"
	agencyPath      = "/agency"
	catalogPath     = "/catalog"
	schedulePath    = "/deliverySchedule"
	addressMapsPath = "/address-maps"
)

// healthResponse is the body of the health endpoint.
type healthResponse struct {
	Status string `json:"status"`
}

// Health checks that the API answers.
func (c *Client) Health(ctx context.Context) error {
	const endpoint = healthPath
	resp, err := doGET(ctx, c.HTTP, c.BaseURL, endpoint)
	if err != nil {
		return err
	}
	_, err = decodeBody[healthResponse](resp, endpoint)
	return err
}

// Agency returns the agency that serves the address.
func (c *Client) Agency(ctx context.Context, a Address) (AgencyInfo, error) {
	const endpoint = agencyPath
	resp, err := doGET(ctx, c.HTTP, c.BaseURL, endpoint,
		param("streetCode", a.StreetCode),
		param("localityCode", a.LocalityCode),
		param("provinceCode", a.ProvinceCode),
		param("municipalityCode", a.MunicipalityCode),
		param("origin", strings.ToUpper(a.Origin)),
		param("inferredAgencyCode", a.AgencyCode),
		param("number", a.Number),
	)
	if err != nil {
		return AgencyInfo{}, err
	}
	return decodeBody[AgencyInfo](resp, endpoint)
}

// Catalog returns the products the agency sells. Entries without a material ID
// are packaging and not orderable.
func (c *Client) Catalog(ctx context.Context, agencyCode string) ([]Product, error) {
	const endpoint = catalogPath
	resp, err := doGET(ctx, c.HTTP, c.BaseURL, endpoint, param("agencyCode", agencyCode))
	if err != nil {
		return nil, err
	}
	return decodeBody[[]Product](resp, endpoint)
}

// DeliverySchedule returns the delivery schedule of a material for the address.
func (c *Client) DeliverySchedule(ctx context.Context, materialID string, a Address) ([]Schedule, error) {
	const endpoint = schedulePath
	resp, err := doGET(ctx, c.HTTP, c.BaseURL, endpoint,
		param("materialId", materialID),
		param("agencyCode", a.AgencyCode),
		param("provinceCode", a.ProvinceCode),
		param("localityCode", a.LocalityCode),
		param("streetTypeCode", a.StreetTypeCode),
		param("municipalityCode", a.MunicipalityCode),
		param("streetCode", a.StreetCode),
		param("number", a.Number),
	)
	if err != nil {
		return nil, err
	}
	return decodeBody[[]Schedule](resp, endpoint)
}

// AddressMaps asks the API to normalise a Google address into a Repsol one. The
// endpoint may answer with an empty body, which means no mapping; that is a
// zero Address rather than an error.
func (c *Client) AddressMaps(ctx context.Context, a Address) (Address, error) {
	const endpoint = addressMapsPath
	resp, err := doGET(ctx, c.HTTP, c.BaseURL, endpoint,
		param("province", a.Province),
		param("provinceCode", a.ProvinceCode),
		param("locality", a.Locality),
		param("streetType", a.StreetType),
		param("streetTypeCode", a.StreetTypeCode),
		param("street", a.Street),
		param("municipality", a.Municipality),
		param("postalCode", a.PostalCode),
	)
	if err != nil {
		return Address{}, err
	}
	if len(bytes.TrimSpace(resp.Body)) == 0 {
		return Address{}, nil
	}
	return decodeBody[Address](resp, endpoint)
}
