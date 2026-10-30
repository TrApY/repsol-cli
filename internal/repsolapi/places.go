package repsolapi

import (
	"context"

	"github.com/felipe9962/repsol-cli/internal/httpx"
)

const (
	autocompletePath = "/v1/autocomplete"
	geocodePath      = "/v1/geocode"
	// islandToExclude lists the Canary Islands the address service must not
	// suggest: peninsular delivery is priced separately.
	islandToExclude = "elhierro,lagomera,lapalma,fuerteventura,lanzarote,lagraciosa"
)

// PlacesClient talks to the address service. The zero value is not usable;
// BaseURL and HTTP must be set.
type PlacesClient struct {
	// BaseURL is the address service base URL, without a trailing slash.
	BaseURL string
	// HTTP is the shared transport.
	HTTP *httpx.Client
}

// autocompleteResponse is the envelope of the autocomplete endpoint.
type autocompleteResponse struct {
	Results []Address `json:"results"`
}

// Autocomplete returns the address suggestions for a free-text input.
func (p *PlacesClient) Autocomplete(ctx context.Context, input string) ([]Address, error) {
	const endpoint = autocompletePath
	resp, err := doGET(ctx, p.HTTP, p.BaseURL, endpoint,
		param("input", input),
		param("maxResults", "5"),
		param("neutralAddress", "true"),
		param("canarias", "true"),
		param("islandToExclude", islandToExclude),
	)
	if err != nil {
		return nil, err
	}
	out, err := decodeBody[autocompleteResponse](resp, endpoint)
	if err != nil {
		return nil, err
	}
	return out.Results, nil
}

// Geocode resolves one autocomplete suggestion, identified by its place ID and
// street number, into a full address with a confidence block.
func (p *PlacesClient) Geocode(ctx context.Context, placeID, number string) (Address, error) {
	const endpoint = geocodePath
	resp, err := doGET(ctx, p.HTTP, p.BaseURL, endpoint,
		param("placeId", placeID),
		param("number", number),
		param("canarias", "true"),
		param("islandToExclude", islandToExclude),
	)
	if err != nil {
		return Address{}, err
	}
	return decodeBody[Address](resp, endpoint)
}
