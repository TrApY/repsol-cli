package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/config"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// addressSetHint tells the user how to resolve and store an address.
const addressSetHint = "run \"repsol address set \\\"STREET NUMBER, TOWN\\\"\""

// postalCodePattern is the first five-digit run of a full address.
var postalCodePattern = regexp.MustCompile(`\d{5}`)

// SetAddressInput is the user input of an address resolution. Pick is the
// 1-based candidate to choose when the autocomplete returns several.
type SetAddressInput struct {
	// Text is the free-text address, such as "Calle Falsa 1, Pueblo".
	Text string
	// Floor is the floor of the delivery address.
	Floor string
	// Door is the door of the delivery address.
	Door string
	// Details are free-form delivery notes.
	Details string
	// Pick chooses the candidate when several match; zero means none chosen.
	Pick int
}

// AddressView is the stored address as the CLI and the MCP server print it.
type AddressView struct {
	// Profile is the profile the address belongs to.
	Profile string `json:"profile"`
	// Summary is the full address followed by the floor and door.
	Summary string `json:"summary"`
	// FullAddress is the address the service returned.
	FullAddress string `json:"full_address"`
	// PostalCode is the five-digit postal code of the full address.
	PostalCode string `json:"postal_code"`
	// Locality is the town or city.
	Locality string `json:"locality"`
	// Province is the name of the province.
	Province string `json:"province"`
	// Floor is the floor of the delivery address.
	Floor string `json:"floor,omitempty"`
	// Door is the door of the delivery address.
	Door string `json:"door,omitempty"`
	// Details are free-form delivery notes.
	Details string `json:"details,omitempty"`
	// AgencyCode is the agency the Repsol API resolved for the address.
	AgencyCode string `json:"agency_code"`
	// Origin is "repsol" or "google", which candidate the address came from.
	Origin string `json:"origin"`
	// Note says what the command did that the user did not ask for.
	Note string `json:"note,omitempty"`
}

// SetAddress resolves free text the way the web application does, stores the
// chosen address on the profile and returns its view.
func (a *App) SetAddress(ctx context.Context, in SetAddressInput) (AddressView, error) {
	results, err := a.places.Autocomplete(ctx, in.Text)
	if err != nil {
		return AddressView{}, err
	}

	candidates, neutralWithNumber := addressCandidates(results)
	if len(candidates) == 0 {
		if neutralWithNumber {
			return AddressView{}, apperr.New(apperr.Usage, "address_not_supported",
				"this address needs a policy lookup the CLI does not support; try a fuller address")
		}
		return AddressView{}, apperr.New(apperr.Usage, "address_needs_number",
			"no address with a street number matched; try a fuller address such as \"STREET NUMBER, TOWN\"")
	}

	if in.Pick == 0 && len(candidates) > 1 {
		return AddressView{}, ambiguousAddress(candidates)
	}
	if in.Pick < 0 || in.Pick > len(candidates) {
		return AddressView{}, invalidPick(in.Pick, len(candidates))
	}
	chosen := candidates[0]
	if in.Pick > 0 {
		chosen = candidates[in.Pick-1]
	}

	addr := chosen
	if strings.EqualFold(chosen.Origin, "google") {
		geocoded, err := a.places.Geocode(ctx, chosen.PlaceID, chosen.Number)
		if err != nil {
			return AddressView{}, err
		}
		if c := geocoded.Confidence; c != nil &&
			(c.NumberNotFound || c.AgencyNotFound || c.ServiceNotFound) {
			return AddressView{}, apperr.New(apperr.Refused, "address_not_served",
				"no agency serves this address").
				WithHint("try a fuller address or a different street number")
		}
		addr = geocoded

		// The mapping endpoint may have nothing to say. In the capture it
		// answered an empty body, so a failure must not reject the address.
		if mapped, err := a.api.AddressMaps(ctx, addr); err == nil {
			mergeAddressMaps(&addr, mapped)
		}
	}

	if addr.ProvinceCode == "" {
		return AddressView{}, apperr.New(apperr.Usage, "address_incomplete",
			"the address has no province; try a fuller address")
	}

	agencyCode, err := a.currentAgency(ctx, addr)
	if err != nil {
		return AddressView{}, err
	}
	addr.AgencyCode = agencyCode

	addr.PostalCode = postalCodePattern.FindString(addr.FullAddress)
	// The web's isPeninsula helper, !["35","38"].some(c => c !== code), is
	// false for every province, and the web sends that false. Repsol's history
	// reports true for the CLI's own order as for the web's, so it does not
	// echo this field: it is not one to "fix" (docs/adr/0003-waylet-payment.md).
	addr.IsPeninsula = false
	addr.Floor = in.Floor
	addr.Door = in.Door
	addr.Details = in.Details
	addr.AutocompleteContent = chosen.FullAddress

	var note string
	if in.Floor == "" && in.Door == "" {
		addr.Floor, addr.Door, note = a.floorAndDoorFromHistory(ctx, addr)
	}

	err = a.store.UpdateConfig(func(cfg *config.Config) error {
		if cfg.Profiles == nil {
			cfg.Profiles = make(map[string]config.Profile)
		}
		profile := cfg.Profiles[a.profile]
		profile.Address = &addr
		cfg.Profiles[a.profile] = profile
		return nil
	})
	if err != nil {
		return AddressView{}, err
	}
	view := a.addressView(addr)
	view.Note = note
	return view, nil
}

// historyScan is how many past orders address set reads to find the floor and
// door of an address; it is the most the history answers in one page.
const historyScan = 100

// floorAndDoorFromHistory returns the floor and door of the newest order in the
// history that went to the same street, number and postal code as addr, and a
// note that says where they came from. It returns nothing when no such order
// has a floor or a door, and when the history cannot be read (no session, or
// the call failed): it is a convenience, and the address is stored without it.
func (a *App) floorAndDoorFromHistory(ctx context.Context, addr repsolapi.Address) (floor, door, note string) {
	history, err := a.api.OrderHistory(ctx, historyScan, 0)
	if err != nil {
		return "", "", ""
	}
	for _, entry := range history {
		if !sameDelivery(entry.Address, addr) {
			continue
		}
		floor, door = strings.TrimSpace(entry.Address.Floor), strings.TrimSpace(entry.Address.Door)
		if floor == "" && door == "" {
			// The newest order here had neither: that is the answer.
			return "", "", ""
		}
		var parts []string
		if floor != "" {
			parts = append(parts, "floor "+floor)
		}
		if door != "" {
			parts = append(parts, "door "+door)
		}
		return floor, door, fmt.Sprintf("no --floor or --door given: took %s from the newest order in the history at this address (reference %s); pass --floor and --door to change them",
			strings.Join(parts, ", "), entry.ReferenceSAC)
	}
	return "", "", ""
}

// sameDelivery reports whether a history address is the street, number and
// postal code of addr. History addresses can be partial, so an address without
// a street, a number or a postal code matches nothing.
func sameDelivery(h repsolapi.HistoryAddress, addr repsolapi.Address) bool {
	street, number, postal := strings.TrimSpace(addr.Street), strings.TrimSpace(addr.Number), strings.TrimSpace(addr.PostalCode)
	return street != "" && number != "" && postal != "" &&
		strings.EqualFold(strings.TrimSpace(h.Street), street) &&
		strings.EqualFold(strings.TrimSpace(h.Number), number) &&
		strings.TrimSpace(h.PostalCode) == postal
}

// ShowAddress returns the stored address of the profile. It is a usage error
// when the profile has none.
func (a *App) ShowAddress() (AddressView, error) {
	addr, err := a.loadAddress()
	if err != nil {
		return AddressView{}, err
	}
	return a.addressView(addr), nil
}

// loadAddress returns the stored address of the profile, or a usage error
// naming the command that stores one.
func (a *App) loadAddress() (repsolapi.Address, error) {
	cfg, err := a.store.LoadConfig()
	if err != nil {
		return repsolapi.Address{}, err
	}
	profile, ok := cfg.Profiles[a.profile]
	if !ok || profile.Address == nil {
		return repsolapi.Address{}, apperr.New(apperr.Usage, "no_address",
			fmt.Sprintf("no delivery address is stored for profile %q", a.profile)).
			WithHint(addressSetHint)
	}
	return *profile.Address, nil
}

// currentAgency resolves the agency that serves addr. An empty agency code
// means Repsol does not deliver there.
func (a *App) currentAgency(ctx context.Context, addr repsolapi.Address) (string, error) {
	info, err := a.agencyInfo(ctx, addr)
	if err != nil {
		return "", err
	}
	return info.AgencyCode, nil
}

// agencyInfo resolves the agency that serves addr with the payment methods it
// accepts. An empty agency code means Repsol does not deliver there.
func (a *App) agencyInfo(ctx context.Context, addr repsolapi.Address) (repsolapi.AgencyInfo, error) {
	info, err := a.api.Agency(ctx, addr)
	if err != nil {
		return repsolapi.AgencyInfo{}, err
	}
	if info.AgencyCode == "" {
		return repsolapi.AgencyInfo{}, apperr.New(apperr.Refused, "no_delivery",
			"no agency delivers to this address").
			WithHint("check the address with \"repsol address show\"")
	}
	return info, nil
}

// addressView renders a stored address for output.
func (a *App) addressView(addr repsolapi.Address) AddressView {
	return AddressView{
		Profile:     a.profile,
		Summary:     addressSummary(addr),
		FullAddress: addr.FullAddress,
		PostalCode:  addr.PostalCode,
		Locality:    addr.Locality,
		Province:    addr.Province,
		Floor:       addr.Floor,
		Door:        addr.Door,
		Details:     addr.Details,
		AgencyCode:  addr.AgencyCode,
		Origin:      addr.Origin,
	}
}

// addressSummary is the full address followed by the floor and door when they
// are set.
func addressSummary(addr repsolapi.Address) string {
	summary := addr.FullAddress
	if addr.Floor != "" {
		summary += ", floor " + addr.Floor
	}
	if addr.Door != "" {
		summary += ", door " + addr.Door
	}
	return summary
}

// addressCandidates returns the suggestions with a street number that are not
// neutral, and whether a neutral suggestion did carry a number.
func addressCandidates(results []repsolapi.Address) ([]repsolapi.Address, bool) {
	candidates := make([]repsolapi.Address, 0, len(results))
	neutralWithNumber := false
	for _, r := range results {
		if r.Number == "" {
			continue
		}
		neutral := r.Neutral != nil && *r.Neutral
		if neutral {
			neutralWithNumber = true
			continue
		}
		candidates = append(candidates, r)
	}
	return candidates, neutralWithNumber
}

// mergeAddressMaps copies the non-empty string fields of the address mapping
// over addr. The endpoint may answer with an empty body, so the merge never
// clears a field.
func mergeAddressMaps(dst *repsolapi.Address, src repsolapi.Address) {
	if src.Province != "" {
		dst.Province = src.Province
	}
	if src.ProvinceCode != "" {
		dst.ProvinceCode = src.ProvinceCode
	}
	if src.Locality != "" {
		dst.Locality = src.Locality
	}
	if src.LocalityCode != "" {
		dst.LocalityCode = src.LocalityCode
	}
	if src.StreetType != "" {
		dst.StreetType = src.StreetType
	}
	if src.StreetTypeCode != "" {
		dst.StreetTypeCode = src.StreetTypeCode
	}
	if src.Street != "" {
		dst.Street = src.Street
	}
	if src.StreetCode != "" {
		dst.StreetCode = src.StreetCode
	}
	if src.Municipality != "" {
		dst.Municipality = src.Municipality
	}
	if src.MunicipalityCode != "" {
		dst.MunicipalityCode = src.MunicipalityCode
	}
	if src.Number != "" {
		dst.Number = src.Number
	}
	if src.AgencyCode != "" {
		dst.AgencyCode = src.AgencyCode
	}
	if src.ServiceCode != "" {
		dst.ServiceCode = src.ServiceCode
	}
	if src.PlaceID != "" {
		dst.PlaceID = src.PlaceID
	}
	if src.Origin != "" {
		dst.Origin = src.Origin
	}
	if src.FullAddress != "" {
		dst.FullAddress = src.FullAddress
	}
}

// ambiguousAddress lists the candidates the user must choose from. The message
// is multi-line and names each candidate by index, address and origin.
func ambiguousAddress(candidates []repsolapi.Address) *apperr.Error {
	var b strings.Builder
	b.WriteString("more than one address matched:")
	for i, c := range candidates {
		fmt.Fprintf(&b, "\n%d) %s [%s]", i+1, c.FullAddress, c.Origin)
	}
	return apperr.New(apperr.Usage, "ambiguous_address", b.String()).
		WithHint("re-run with --pick N")
}

// invalidPick reports a 1-based candidate index outside the candidates.
func invalidPick(pick, count int) *apperr.Error {
	return apperr.New(apperr.Usage, "invalid_pick",
		fmt.Sprintf("--pick %d is out of range: choose a number between 1 and %d", pick, count)).
		WithHint("re-run with --pick N")
}
