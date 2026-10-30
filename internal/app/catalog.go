package app

import (
	"context"

	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// CatalogItem is one orderable product of the profile's agency.
type CatalogItem struct {
	// Product is the canonical alias, or empty for a material without one.
	Product string `json:"product"`
	// MaterialID is the numeric material id as the API names it.
	MaterialID string `json:"material_id"`
	// Name is the product name the agency uses.
	Name string `json:"name"`
	// Price is the price of one bottle.
	Price float64 `json:"price"`
	// PricePerKilo is the price per kilogram when the agency publishes it.
	PricePerKilo float64 `json:"price_per_kilo,omitempty"`
	// MaxQuantity is the maximum number of bottles in one order.
	MaxQuantity int `json:"max_quantity"`
}

// Catalog returns the products the agency of the stored address sells. The
// catalog route lists packaging rows without a material id; they are dropped
// because they are not orderable.
func (a *App) Catalog(ctx context.Context) ([]CatalogItem, error) {
	addr, err := a.loadAddress()
	if err != nil {
		return nil, err
	}
	agencyCode, err := a.currentAgency(ctx, addr)
	if err != nil {
		return nil, err
	}
	products, err := a.api.Catalog(ctx, agencyCode)
	if err != nil {
		return nil, err
	}

	items := make([]CatalogItem, 0, len(products))
	for _, p := range products {
		if p.MaterialID == "" {
			continue
		}
		items = append(items, catalogItem(p))
	}
	return items, nil
}

// catalogItem maps one API product to its output shape.
func catalogItem(p repsolapi.Product) CatalogItem {
	return CatalogItem{
		Product:      AliasFor(p.MaterialID),
		MaterialID:   p.MaterialID,
		Name:         p.Type,
		Price:        p.Price,
		PricePerKilo: p.PricePerKilo,
		MaxQuantity:  maxQuantity(p.MaterialID),
	}
}
