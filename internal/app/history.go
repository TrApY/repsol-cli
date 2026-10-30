package app

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// historyTimeLayout is the layout the API writes createdAt with.
const historyTimeLayout = "2006-01-02T15:04:05.000-0700"

// referencePattern is the shape of a history referenceSAC: eight digits.
var referencePattern = regexp.MustCompile(`^\d{8}$`)

// OrderSummary is one entry of the order history.
type OrderSummary struct {
	// Reference is the eight-digit referenceSAC the history reports.
	Reference string `json:"reference"`
	// CreatedAt is the creation time as RFC 3339, or the raw value when it does
	// not parse.
	CreatedAt string `json:"created_at"`
	// DeliveryDate is the promised delivery date as YYYY-MM-DD.
	DeliveryDate string `json:"delivery_date"`
	// Product is the canonical alias, or empty for a material without one.
	Product string `json:"product,omitempty"`
	// MaterialID is the numeric material id as the API names it.
	MaterialID string `json:"material_id"`
	// Quantity is the number of bottles.
	Quantity int `json:"quantity"`
	// UnitPrice is the price of one bottle.
	UnitPrice float64 `json:"unit_price"`
	// Total is the price of the whole order.
	Total float64 `json:"total"`
	// Address is the delivery address summary the history carries.
	Address string `json:"address"`
	// Agency is the agency of the order.
	Agency AgencyView `json:"agency"`
	// Pending reports whether the delivery is today or later in Madrid for the
	// stored address. The history has no status field.
	Pending bool `json:"pending"`
}

// ListOrders returns the newest history entries, at most limit of them.
func (a *App) ListOrders(ctx context.Context, limit int) ([]OrderSummary, error) {
	if limit < 1 || limit > ShowLookback {
		return nil, apperr.New(apperr.Usage, "invalid_limit",
			fmt.Sprintf("limit %d is outside 1..%d", limit, ShowLookback)).
			WithHint("choose a limit between 1 and 100")
	}

	entries, err := a.api.OrderHistory(ctx, limit, 0)
	if err != nil {
		return nil, err
	}

	addr, hasAddress := a.storedAddress()
	today := a.today()
	orders := make([]OrderSummary, 0, len(entries))
	for _, entry := range entries {
		orders = append(orders, orderSummary(entry, today, addr, hasAddress))
	}
	return orders, nil
}

// LastOrder returns the newest history entry. It is a not-found error when the
// profile has no orders.
func (a *App) LastOrder(ctx context.Context) (OrderSummary, error) {
	orders, err := a.ListOrders(ctx, 1)
	if err != nil {
		return OrderSummary{}, err
	}
	if len(orders) == 0 {
		return OrderSummary{}, apperr.New(apperr.NotFound, "no_orders",
			"the order history is empty").
			WithHint("run \"repsol order create\" to place one")
	}
	return orders[0], nil
}

// ShowOrder returns the history entry with the given referenceSAC, searched in
// the newest ShowLookback entries.
func (a *App) ShowOrder(ctx context.Context, reference string) (OrderSummary, error) {
	if !referencePattern.MatchString(reference) {
		return OrderSummary{}, apperr.New(apperr.Usage, "invalid_reference",
			fmt.Sprintf("order reference %q is not eight digits", reference)).
			WithHint("run \"repsol order list\" to see the references")
	}

	orders, err := a.ListOrders(ctx, ShowLookback)
	if err != nil {
		return OrderSummary{}, err
	}
	for _, order := range orders {
		if order.Reference == reference {
			return order, nil
		}
	}
	return OrderSummary{}, apperr.New(apperr.NotFound, "order_not_found",
		fmt.Sprintf("no order with reference %s in the newest %d entries", reference, ShowLookback)).
		WithHint("run \"repsol order list\" to see the references")
}

// storedAddress returns the address of the profile and whether there is one.
// History views keep working without a stored address, so its errors are not
// reported here.
func (a *App) storedAddress() (repsolapi.Address, bool) {
	addr, err := a.loadAddress()
	if err != nil {
		return repsolapi.Address{}, false
	}
	return addr, true
}

// orderSummary maps one history entry to its output shape. Pending uses the
// stored address; without one only the delivery date counts, because the
// history address cannot be matched to a place.
func orderSummary(entry repsolapi.HistoryEntry, today string, addr repsolapi.Address, hasAddress bool) OrderSummary {
	pending := entry.DeliveryDate >= today
	if hasAddress {
		pending = pendingAtAddress(entry, today, addr.AgencyCode, addr.LocalityCode)
	}
	return OrderSummary{
		Reference:    entry.ReferenceSAC,
		CreatedAt:    historyTime(entry.CreatedAt),
		DeliveryDate: entry.DeliveryDate,
		Product:      AliasFor(entry.MaterialID),
		MaterialID:   entry.MaterialID,
		Quantity:     entry.Quantity,
		UnitPrice:    entry.UnitPrice,
		Total:        math.Round(entry.UnitPrice*float64(entry.Quantity)*100) / 100,
		Address:      historyAddress(entry.Address),
		Agency: AgencyView{
			Name:  entry.Agency.Name,
			Phone: entry.Agency.Phone,
			Code:  entry.Agency.AgencyCode,
		},
		Pending: pending,
	}
}

// historyTime prints a history timestamp as RFC 3339. A value the API sent in
// another shape is kept as it came.
func historyTime(raw string) string {
	parsed, err := time.Parse(historyTimeLayout, raw)
	if err != nil {
		return raw
	}
	return parsed.Format(time.RFC3339)
}

// historyAddress renders the partial address of a history entry. The API keeps
// the text in Details and often leaves the street empty.
func historyAddress(addr repsolapi.HistoryAddress) string {
	street := strings.TrimSpace(addr.Details)
	if addr.Street != "" {
		street = joinNonEmpty(" ", addr.StreetType, addr.Street, addr.Number)
	}
	return joinNonEmpty(", ", street, addr.Locality)
}

// joinNonEmpty joins the parts that are not blank with sep.
func joinNonEmpty(sep string, parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, sep)
}
