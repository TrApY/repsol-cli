package fakerepsol

import (
	"net/http"
	"strings"

	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// handleAutocomplete picks the synthetic suggestions by substring of the input,
// the way the real service matches free text.
func (s *Server) handleAutocomplete(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")

	results := []repsolapi.Address{}
	switch {
	case strings.Contains(input, "Falsa"):
		results = []repsolapi.Address{
			repsolAddress("Falsa", "1", "Calle Falsa 1, 28001 Pueblo, Madrid", false),
			repsolAddress("Falsa", "", "Calle Falsa, 28001 Pueblo, Madrid", true),
		}
	case strings.Contains(input, "Ambigua"):
		results = []repsolapi.Address{
			googleAddress("place-a", "Ambigua", "5", "Calle Ambigua 5, 28001 Pueblo, Madrid"),
			googleAddress("place-b", "Ambigua", "7", "Calle Ambigua 7, 28001 Pueblo, Madrid"),
		}
	case strings.Contains(input, "Neutral"):
		results = []repsolapi.Address{
			repsolAddress("Neutral", "3", "Calle Neutral 3, 28001 Pueblo, Madrid", true),
		}
	case strings.Contains(input, "Sin numero"):
		results = []repsolapi.Address{
			repsolAddress("Sin Numero", "", "Calle Sin Numero, 28001 Pueblo, Madrid", true),
		}
	case strings.Contains(input, "Lejos"):
		results = []repsolapi.Address{
			googleAddress("place-noagency", "Lejos", "9", "Calle Lejos 9, 28001 Pueblo, Madrid"),
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// handleGeocode resolves one suggestion into a full address with a confidence
// block. The no-agency place reports the agency as not found.
func (s *Server) handleGeocode(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	placeID := query.Get("placeId")
	number := query.Get("number")

	street := "Ambigua"
	if placeID == "place-noagency" {
		street = "Lejos"
	}
	currentFullAddress := ""
	currentStreet := ""

	writeJSON(w, http.StatusOK, repsolapi.Address{
		Province:           "Madrid",
		ProvinceCode:       ProvinceCode,
		Locality:           "Pueblo",
		LocalityCode:       LocalityCode,
		StreetType:         "Calle",
		Street:             street,
		Municipality:       "Pueblo",
		MunicipalityCode:   LocalityCode,
		Number:             number,
		AgencyCode:         AgencyCode,
		Latitude:           "40.4168",
		Longitude:          "-3.7038",
		Origin:             "google",
		FullAddress:        "Calle " + street + " " + number + ", " + PostalCode + " Pueblo, Madrid",
		PostalCode:         PostalCode,
		Confidence:         &repsolapi.Confidence{AgencyNotFound: placeID == "place-noagency"},
		CurrentFullAddress: &currentFullAddress,
		CurrentStreet:      &currentStreet,
	})
}

// repsolAddress is a suggestion from Repsol's own street database: every code
// is filled in and the coordinates are empty strings.
func repsolAddress(street, number, fullAddress string, neutral bool) repsolapi.Address {
	return repsolapi.Address{
		Province:         "Madrid",
		ProvinceCode:     ProvinceCode,
		Locality:         "Pueblo",
		LocalityCode:     LocalityCode,
		StreetType:       "Calle",
		StreetTypeCode:   "0001",
		Street:           street,
		StreetCode:       "0000000001",
		Municipality:     "Pueblo",
		MunicipalityCode: LocalityCode,
		Number:           number,
		AgencyCode:       AgencyCode,
		ServiceCode:      "1",
		Latitude:         "",
		Longitude:        "",
		Origin:           "repsol",
		Neutral:          boolPtr(neutral),
		FullAddress:      fullAddress,
	}
}

// googleAddress is a suggestion from Google: its codes are empty until the
// address is geocoded, and it carries no neutral flag.
func googleAddress(placeID, street, number, fullAddress string) repsolapi.Address {
	return repsolapi.Address{
		Province:    "Madrid",
		Locality:    "Pueblo",
		StreetType:  "Calle",
		Street:      street,
		Number:      number,
		PlaceID:     placeID,
		Origin:      "google",
		FullAddress: fullAddress,
	}
}
