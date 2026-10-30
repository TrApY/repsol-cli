package app

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// PendingLookback is how many history entries the pending guard reads before a
// write.
const PendingLookback = 20

// ShowLookback is how many history entries order show searches.
const ShowLookback = 100

// forceOrderHint explains when --force is acceptable. A cancelled delivery keeps
// showing as pending until its date passes, so the owner decides.
const forceOrderHint = "pass --force only if the owner explicitly wants another delivery; " +
	"a delivery cancelled by phone still shows as pending until its date"

// webFlowHint points at the only place an order the CLI cannot place can be
// completed.
const webFlowHint = "this order needs the web flow: https://pidetubombona.repsol.es/"

// The write branches the cart check selects.
const (
	orderTypeSolicitude = "SOLICITUDE"
	orderTypeOrder      = "ORDER"
)

// paymentOnDelivery is the payment method of every order the CLI places.
const paymentOnDelivery = "on_delivery"

// PlaceOrderInput is the user input of an order placement. Empty Product and
// zero Quantity are filled with the defaults by the caller; Date empty means the
// earliest offered date, and Pay empty means delivery.
type PlaceOrderInput struct {
	// Product is a product alias or a numeric material id.
	Product string
	// Quantity is the number of bottles.
	Quantity int
	// Date is the requested delivery date as YYYY-MM-DD, or empty.
	Date string
	// Pay is the payment method: empty or "delivery".
	Pay string
	// DryRun plans the order without writing it.
	DryRun bool
	// Force skips the pending-order guard.
	Force bool
}

// AgencyView is the agency of an order, as the CLI and the MCP server print it.
type AgencyView struct {
	// Name is the agency name.
	Name string `json:"name,omitempty"`
	// Phone is the agency phone.
	Phone string `json:"phone,omitempty"`
	// Code is the numeric agency code.
	Code string `json:"code,omitempty"`
}

// OrderResult is the outcome of an order placement, or of a dry run of one.
type OrderResult struct {
	// ID is the external id the write returned. It is empty on a dry run.
	ID string `json:"id,omitempty"`
	// DryRun reports whether the order was only planned.
	DryRun bool `json:"dry_run"`
	// OrderType is the write the cart check selected: SOLICITUDE or ORDER.
	OrderType string `json:"order_type"`
	// Product is the canonical alias, or empty for a material without one.
	Product string `json:"product"`
	// MaterialID is the numeric material id as the API names it.
	MaterialID string `json:"material_id"`
	// Quantity is the number of bottles.
	Quantity int `json:"quantity"`
	// UnitPrice is the price of one bottle.
	UnitPrice float64 `json:"unit_price"`
	// Total is the price of the whole order.
	Total float64 `json:"total"`
	// DeliveryDate is the promised delivery date as YYYY-MM-DD.
	DeliveryDate string `json:"delivery_date"`
	// Address is the delivery address summary.
	Address string `json:"address"`
	// Agency is the agency that will deliver the order.
	Agency AgencyView `json:"agency"`
	// Payment is always "on_delivery".
	Payment string `json:"payment"`
}

// PlaceOrder validates the input, plans the order and, unless it is a dry run,
// places it. Every read happens before the write; the first failure stops the
// call, and a failed write is never retried.
func (a *App) PlaceOrder(ctx context.Context, in PlaceOrderInput) (OrderResult, error) {
	if err := validateOrderInput(in); err != nil {
		return OrderResult{}, err
	}

	addr, err := a.loadAddress()
	if err != nil {
		return OrderResult{}, err
	}
	info, err := ResolveProduct(in.Product, addr.ProvinceCode)
	if err != nil {
		return OrderResult{}, err
	}
	if err := validateQuantity(in.Quantity, info); err != nil {
		return OrderResult{}, err
	}

	agencyCode, err := a.currentAgency(ctx, addr)
	if err != nil {
		return OrderResult{}, err
	}
	addr.AgencyCode = agencyCode

	unitPrice, err := a.pricedMaterial(ctx, agencyCode, info.MaterialID)
	if err != nil {
		return OrderResult{}, err
	}

	schedule, err := a.deliverySchedule(ctx, info.MaterialID, addr)
	if err != nil {
		return OrderResult{}, err
	}
	date, err := chooseDeliveryDate(schedule, in.Date)
	if err != nil {
		return OrderResult{}, err
	}

	if !in.Force {
		if err := a.ensureNoPendingOrder(ctx, agencyCode, addr.LocalityCode); err != nil {
			return OrderResult{}, err
		}
	}

	var scheduleMode *string
	if schedule.Mode != "" {
		scheduleMode = &schedule.Mode
	}
	check, err := a.api.CheckCart(ctx, repsolapi.CartCheckRequest{
		ScheduleMode:   scheduleMode,
		AgencyCode:     agencyCode,
		Address:        addr,
		MaterialID:     info.MaterialID,
		Quantity:       in.Quantity,
		UnitPrice:      unitPrice,
		DeliveryDateID: date.ID,
		DeliveryDate:   date.Date,
	})
	if err != nil {
		return OrderResult{}, err
	}

	branch, err := orderBranch(check)
	if err != nil {
		return OrderResult{}, err
	}

	result := OrderResult{
		DryRun:       in.DryRun,
		OrderType:    branch,
		Product:      info.Alias,
		MaterialID:   info.MaterialID,
		Quantity:     in.Quantity,
		UnitPrice:    unitPrice,
		Total:        math.Round(unitPrice*float64(in.Quantity)*100) / 100,
		DeliveryDate: date.Date,
		Address:      addressSummary(addr),
		Agency:       AgencyView{Code: agencyCode},
		Payment:      paymentOnDelivery,
	}
	if in.DryRun {
		return result, nil
	}

	placed, err := a.writeOrder(ctx, branch, check, addr, info.MaterialID, in.Quantity, unitPrice, date)
	if err != nil {
		return OrderResult{}, err
	}

	result.ID = placed.ExternalID
	if placed.DeliveryDate != "" {
		result.DeliveryDate = placed.DeliveryDate
	}
	result.Agency = AgencyView{
		Name:  placed.Agency.Name,
		Phone: placed.Agency.Phone,
		Code:  agencyCode,
	}
	return result, nil
}

// validateOrderInput checks the payment method and the shape of the date before
// any request is sent.
func validateOrderInput(in PlaceOrderInput) error {
	if in.Pay != "" && in.Pay != "delivery" {
		return apperr.New(apperr.Usage, "unsupported_payment",
			fmt.Sprintf("unsupported payment method %q: orders are paid on delivery", in.Pay)).
			WithHint("use --pay delivery")
	}
	if in.Date != "" {
		if _, err := time.Parse(time.DateOnly, in.Date); err != nil {
			return apperr.New(apperr.Usage, "invalid_date",
				fmt.Sprintf("invalid date %q: use YYYY-MM-DD", in.Date)).
				WithHint("run \"repsol schedule list\" to see the offered dates")
		}
	}
	return nil
}

// validateQuantity checks the requested quantity against the per-order maximum
// of the material.
func validateQuantity(quantity int, info ProductInfo) error {
	if quantity < 1 {
		return apperr.New(apperr.Usage, "invalid_quantity",
			fmt.Sprintf("quantity %d must be between 1 and %d for material %s",
				quantity, info.MaxQty, info.MaterialID))
	}
	if quantity > info.MaxQty {
		return apperr.New(apperr.Usage, "invalid_quantity",
			fmt.Sprintf("quantity %d is above the maximum of %d for material %s",
				quantity, info.MaxQty, info.MaterialID)).
			WithHint("lower --qty or split the order")
	}
	return nil
}

// pricedMaterial returns the catalog price of a material for an agency. A
// material the catalog does not list is refused, because the agency cannot sell
// it.
func (a *App) pricedMaterial(ctx context.Context, agencyCode, materialID string) (float64, error) {
	products, err := a.api.Catalog(ctx, agencyCode)
	if err != nil {
		return 0, err
	}
	for _, p := range products {
		if p.MaterialID == materialID {
			return p.Price, nil
		}
	}
	return 0, apperr.New(apperr.Refused, "product_unavailable",
		fmt.Sprintf("the agency does not sell material %s", materialID)).
		WithHint("run \"repsol catalog list\" to see the available products")
}

// deliverySchedule returns the schedule of a material for the address. An agency
// that does not deliver there offers no dates, which is a refusal.
func (a *App) deliverySchedule(ctx context.Context, materialID string, addr repsolapi.Address) (repsolapi.Schedule, error) {
	schedules, err := a.api.DeliverySchedule(ctx, materialID, addr)
	if err != nil {
		return repsolapi.Schedule{}, err
	}
	schedule, ok := scheduleFor(schedules, materialID)
	if !ok || schedule.Mode == "NO_AGENCY" {
		return repsolapi.Schedule{}, noScheduleDates(materialID)
	}
	return schedule, nil
}

// chooseDeliveryDate returns the requested date when it is offered, and the
// earliest offered date otherwise.
func chooseDeliveryDate(schedule repsolapi.Schedule, want string) (repsolapi.DeliveryDate, error) {
	dates := make([]repsolapi.DeliveryDate, 0, len(schedule.Dates))
	for _, d := range schedule.Dates {
		if d.Date != "" {
			dates = append(dates, d)
		}
	}
	if len(dates) == 0 {
		return repsolapi.DeliveryDate{}, noScheduleDates(schedule.MaterialID)
	}
	slices.SortFunc(dates, func(a, b repsolapi.DeliveryDate) int {
		return strings.Compare(a.Date, b.Date)
	})

	if want == "" {
		return dates[0], nil
	}
	if i := slices.IndexFunc(dates, func(d repsolapi.DeliveryDate) bool {
		return d.Date == want
	}); i >= 0 {
		return dates[i], nil
	}

	offered := make([]string, 0, len(dates))
	for _, d := range dates {
		offered = append(offered, d.Date)
	}
	return repsolapi.DeliveryDate{}, apperr.New(apperr.Refused, "date_unavailable",
		fmt.Sprintf("the agency does not offer %s; offered: %s", want, strings.Join(offered, ", "))).
		WithHint("run \"repsol schedule list\" to see the offered dates")
}

// ensureNoPendingOrder refuses an order when the history holds a pending
// delivery for the same agency and locality.
func (a *App) ensureNoPendingOrder(ctx context.Context, agencyCode, localityCode string) error {
	entries, err := a.api.OrderHistory(ctx, PendingLookback, 0)
	if err != nil {
		return err
	}
	today := a.today()
	for _, entry := range entries {
		if pendingAtAddress(entry, today, agencyCode, localityCode) {
			return apperr.New(apperr.Refused, "pending_order",
				fmt.Sprintf("order %s for %s is still pending at this address",
					entry.ReferenceSAC, entry.DeliveryDate)).
				WithHint(forceOrderHint)
		}
	}
	return nil
}

// today returns the current date in Europe/Madrid, the zone the delivery day is
// measured in.
func (a *App) today() string {
	return a.now().In(madrid).Format(time.DateOnly)
}

// pendingAtAddress reports whether the history entry is a delivery that is today
// or later in Madrid for the same agency and locality.
func pendingAtAddress(entry repsolapi.HistoryEntry, today, agencyCode, localityCode string) bool {
	if entry.DeliveryDate < today {
		return false
	}
	return entry.Agency.AgencyCode == agencyCode && entry.Address.LocalityCode == localityCode
}

// orderBranch decides which write the cart check selects. The online-policy
// answer goes first, because it refuses the order whatever the order type says.
func orderBranch(check repsolapi.CartCheck) (string, error) {
	switch {
	case check.OnlinePolicy:
		return "", apperr.New(apperr.Refused, "online_policy_required",
			"this order can only be placed on the Repsol web").
			WithHint(webFlowHint)
	case check.OrderType == orderTypeSolicitude:
		return orderTypeSolicitude, nil
	case check.Order:
		return orderTypeOrder, nil
	default:
		return "", apperr.New(apperr.Refused, "unsupported_order_type",
			fmt.Sprintf("the cart check returned an unsupported order type %q", check.OrderType)).
			WithHint(webFlowHint)
	}
}

// writeOrder performs the single write the cart check selected. It is never
// retried: the API client turns an ambiguous failure into the outcome-unknown
// error.
func (a *App) writeOrder(
	ctx context.Context,
	branch string,
	check repsolapi.CartCheck,
	addr repsolapi.Address,
	materialID string,
	quantity int,
	unitPrice float64,
	date repsolapi.DeliveryDate,
) (repsolapi.Placed, error) {
	if branch == orderTypeSolicitude {
		return a.api.CreateSolicitude(ctx, repsolapi.SolicitudeRequest{
			Address:        addr,
			MaterialID:     materialID,
			Quantity:       quantity,
			UnitPrice:      unitPrice,
			Status:         check.Status,
			Bill:           false,
			Discount:       0,
			DeliveryDateID: date.ID,
			DeliveryDate:   date.Date,
		})
	}
	return a.api.CreateOrder(ctx, repsolapi.OrderRequest{
		Address:        addr,
		MaterialID:     materialID,
		Quantity:       quantity,
		UnitPrice:      unitPrice,
		Bill:           false,
		Discount:       0,
		DeliveryDateID: date.ID,
		DeliveryDate:   date.Date,
	})
}
