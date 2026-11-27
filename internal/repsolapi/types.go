package repsolapi

import (
	"encoding/json"
	"fmt"
)

// FlexNumber is a number that the API sends either as a JSON number or as a
// JSON string, depending on the endpoint. It marshals an empty value as "" and
// numeric text as a bare number, so the wire name and the wire type survive a
// round trip.
type FlexNumber string

// UnmarshalJSON accepts a JSON number, a JSON string or null.
func (n *FlexNumber) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*n = ""
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*n = FlexNumber(s)
		return nil
	}
	if !isJSONNumber(string(data)) {
		return fmt.Errorf("repsolapi: %s is not a JSON number or string", data)
	}
	*n = FlexNumber(data)
	return nil
}

// MarshalJSON writes an empty value as "" and numeric text as a bare number.
func (n FlexNumber) MarshalJSON() ([]byte, error) {
	s := string(n)
	if s == "" {
		return []byte(`""`), nil
	}
	if isJSONNumber(s) {
		return []byte(s), nil
	}
	return json.Marshal(s)
}

// isJSONNumber reports whether s is a single, valid JSON number. The leading
// byte check rejects the literals json.Valid accepts but the API never sends,
// such as true.
func isJSONNumber(s string) bool {
	if s == "" {
		return false
	}
	if c := s[0]; c != '-' && (c < '0' || c > '9') {
		return false
	}
	return json.Valid([]byte(s))
}

// Confidence is the geocoder's confidence block. Every flag reports whether
// the geocoder could not confirm the matching part of the address.
type Confidence struct {
	NumberNotFound               bool `json:"number_not_found" toml:"number_not_found"`
	AgencyNotFound               bool `json:"agency_not_found" toml:"agency_not_found"`
	ServiceNotFound              bool `json:"service_not_found" toml:"service_not_found"`
	IsFromDiscardedIsland        bool `json:"is_from_discarded_island" toml:"is_from_discarded_island"`
	ReferencePolicyInWrongIsland bool `json:"reference_policy_in_wrong_island" toml:"reference_policy_in_wrong_island"`
}

// Address is one address as the address service and the Repsol API describe
// it: the autocomplete and geocode results, the stored profile address and the
// address sent with an order.
type Address struct {
	Province            string      `json:"province" toml:"province"`
	ProvinceCode        string      `json:"provinceCode" toml:"provinceCode"`
	Locality            string      `json:"locality" toml:"locality"`
	LocalityCode        string      `json:"localityCode" toml:"localityCode"`
	StreetType          string      `json:"streetType" toml:"streetType"`
	StreetTypeCode      string      `json:"streetTypeCode" toml:"streetTypeCode"`
	Street              string      `json:"street" toml:"street"`
	StreetCode          string      `json:"streetCode" toml:"streetCode"`
	Municipality        string      `json:"municipality" toml:"municipality"`
	MunicipalityCode    string      `json:"municipalityCode" toml:"municipalityCode"`
	Number              string      `json:"number" toml:"number"`
	AgencyCode          string      `json:"agencyCode" toml:"agencyCode"`
	ServiceCode         string      `json:"serviceCode" toml:"serviceCode"`
	PlaceID             string      `json:"placeId" toml:"placeId"`
	Latitude            FlexNumber  `json:"latitude" toml:"latitude"`
	Longitude           FlexNumber  `json:"longitude" toml:"longitude"`
	Origin              string      `json:"origin" toml:"origin"`
	Neutral             *bool       `json:"neutral,omitempty" toml:"neutral,omitempty"`
	FullAddress         string      `json:"fullAddress" toml:"fullAddress"`
	Confidence          *Confidence `json:"confidence,omitempty" toml:"confidence,omitempty"`
	CurrentFullAddress  *string     `json:"currentFullAddress,omitempty" toml:"currentFullAddress,omitempty"`
	CurrentStreet       *string     `json:"currentStreet,omitempty" toml:"currentStreet,omitempty"`
	Floor               string      `json:"floor" toml:"floor"`
	Door                string      `json:"door" toml:"door"`
	Details             string      `json:"details" toml:"details"`
	PostalCode          string      `json:"postalCode" toml:"postalCode"`
	IsPeninsula         bool        `json:"isPeninsula" toml:"isPeninsula"`
	AutocompleteContent string      `json:"autocompleteContent,omitempty" toml:"autocompleteContent"`
}

// AgencyInfo is the agency that serves an address.
type AgencyInfo struct {
	AgencyCode    string `json:"agencyCode" toml:"agencyCode"`
	OnlinePayment bool   `json:"onlinePayment" toml:"onlinePayment"`
	WayletPayment bool   `json:"wayletPayment" toml:"wayletPayment"`
	AllowsTip     bool   `json:"allowsTip" toml:"allowsTip"`
}

// Product is one catalog entry. Entries without a material ID are packaging
// and not orderable.
type Product struct {
	ID           string  `json:"id" toml:"id"`
	MaterialID   string  `json:"materialId" toml:"materialId"`
	Type         string  `json:"type" toml:"type"`
	Description  string  `json:"description" toml:"description"`
	Price        float64 `json:"price" toml:"price"`
	PricePerKilo float64 `json:"pricePerKilo" toml:"pricePerKilo"`
}

// DeliveryDate is one date the agency offers for a material.
type DeliveryDate struct {
	ID   string `json:"id" toml:"id"`
	Date string `json:"date" toml:"date"`
}

// Schedule is the delivery schedule of one material. Mode is optional and is
// empty when the API omits it.
type Schedule struct {
	MaterialID string         `json:"materialId" toml:"materialId"`
	Dates      []DeliveryDate `json:"dates" toml:"dates"`
	Mode       string         `json:"mode" toml:"mode"`
	Warning    bool           `json:"warning" toml:"warning"`
	Type       string         `json:"type" toml:"type"`
}
