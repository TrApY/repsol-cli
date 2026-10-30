package app

import (
	"context"
	"slices"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// ScheduleView is the delivery schedule of one product for the stored address.
type ScheduleView struct {
	// Product is the canonical alias, or empty for a material without one.
	Product string `json:"product"`
	// MaterialID is the numeric material id as the API names it.
	MaterialID string `json:"material_id"`
	// Mode is the schedule mode, empty when the API omits it.
	Mode string `json:"mode,omitempty"`
	// Warning reports whether the agency flags the schedule.
	Warning bool `json:"warning"`
	// Dates are the offered delivery dates, sorted ascending.
	Dates []string `json:"dates"`
}

// Schedule returns the delivery dates the agency of the stored address offers
// for a product. An alias, a numeric material id or an empty product selects
// the product; empty means butano.
func (a *App) Schedule(ctx context.Context, product string) (ScheduleView, error) {
	addr, err := a.loadAddress()
	if err != nil {
		return ScheduleView{}, err
	}
	info, err := ResolveProduct(product, addr.ProvinceCode)
	if err != nil {
		return ScheduleView{}, err
	}
	agencyCode, err := a.currentAgency(ctx, addr)
	if err != nil {
		return ScheduleView{}, err
	}
	addr.AgencyCode = agencyCode

	schedules, err := a.api.DeliverySchedule(ctx, info.MaterialID, addr)
	if err != nil {
		return ScheduleView{}, err
	}
	schedule, ok := scheduleFor(schedules, info.MaterialID)
	if !ok || schedule.Mode == "NO_AGENCY" {
		return ScheduleView{}, noScheduleDates(info.MaterialID)
	}

	dates := make([]string, 0, len(schedule.Dates))
	for _, d := range schedule.Dates {
		if d.Date != "" {
			dates = append(dates, d.Date)
		}
	}
	if len(dates) == 0 {
		return ScheduleView{}, noScheduleDates(info.MaterialID)
	}
	slices.Sort(dates)

	return ScheduleView{
		Product:    info.Alias,
		MaterialID: info.MaterialID,
		Mode:       schedule.Mode,
		Warning:    schedule.Warning,
		Dates:      dates,
	}, nil
}

// scheduleFor picks the entry of the requested material, or the first entry
// when the API answers with a different material.
func scheduleFor(schedules []repsolapi.Schedule, materialID string) (repsolapi.Schedule, bool) {
	for _, s := range schedules {
		if s.MaterialID == materialID {
			return s, true
		}
	}
	if len(schedules) > 0 {
		return schedules[0], true
	}
	return repsolapi.Schedule{}, false
}

// noScheduleDates builds the refusal for an agency that offers no date for a
// material.
func noScheduleDates(materialID string) *apperr.Error {
	return apperr.New(apperr.Refused, "no_delivery_dates",
		"the agency offers no delivery dates for material "+materialID).
		WithHint("try another product or check the address with \"repsol address show\"")
}
