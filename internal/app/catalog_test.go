package app

import (
	"context"
	"slices"
	"testing"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/fakerepsol"
)

func TestCatalogFiltersPackaging(t *testing.T) {
	ctx := context.Background()

	t.Run("with a stored address", func(t *testing.T) {
		a, fake, _, _ := newTestApp(t)
		mustSetAddress(t, a)

		items, err := a.Catalog(ctx)
		if err != nil {
			t.Fatalf("Catalog: %v", err)
		}
		if len(items) != 5 {
			t.Fatalf("len(items) = %d, want 5", len(items))
		}

		first := items[0]
		if first.Product != "butano" {
			t.Errorf("items[0].Product = %q, want %q", first.Product, "butano")
		}
		if first.MaterialID != "153" {
			t.Errorf("items[0].MaterialID = %q, want %q", first.MaterialID, "153")
		}
		if first.Name != "Butano 12.5kg" {
			t.Errorf("items[0].Name = %q, want %q", first.Name, "Butano 12.5kg")
		}
		if first.Price != 18.89 {
			t.Errorf("items[0].Price = %v, want %v", first.Price, 18.89)
		}
		if first.MaxQuantity != 9 {
			t.Errorf("items[0].MaxQuantity = %d, want %d", first.MaxQuantity, 9)
		}

		aliases := map[string]string{
			"153": "butano",
			"154": "butano-ligera",
			"239": "propano",
			"210": "propano-35",
			"230": "autogas",
		}
		for _, item := range items {
			if item.MaterialID == "" {
				t.Error("a catalog item without a material id survived the packaging filter")
				continue
			}
			if want := aliases[item.MaterialID]; item.Product != want {
				t.Errorf("material %s has product %q, want %q", item.MaterialID, item.Product, want)
			}
		}
		if items[3].MaxQuantity != 24 {
			t.Errorf("items[3].MaxQuantity = %d, want 24", items[3].MaxQuantity)
		}

		if got := fake.Count("/api/catalog"); got != 1 {
			t.Errorf("catalog calls = %d, want 1", got)
		}
		query := fake.Requests("/api/catalog")[0].Query
		if got := query.Get("agencyCode"); got != fakerepsol.AgencyCode {
			t.Errorf("catalog agencyCode = %q, want %q", got, fakerepsol.AgencyCode)
		}
	})

	t.Run("without a stored address", func(t *testing.T) {
		a, fake, _, _ := newTestApp(t)

		_, err := a.Catalog(ctx)
		typed := requireErrorCode(t, err, "no_address")
		if typed.Category != apperr.Usage {
			t.Errorf("category = %v, want %v", typed.Category, apperr.Usage)
		}
		if got := fake.Count("/api/catalog"); got != 0 {
			t.Errorf("catalog calls = %d, want 0", got)
		}
	})
}

func TestScheduleSortedAndRefusals(t *testing.T) {
	ctx := context.Background()

	t.Run("dates are sorted", func(t *testing.T) {
		a, fake, _, _ := newTestApp(t)
		mustSetAddress(t, a)

		view, err := a.Schedule(ctx, "")
		if err != nil {
			t.Fatalf("Schedule: %v", err)
		}

		want := []string{"2026-11-04", "2026-11-06"}
		if !slices.Equal(view.Dates, want) {
			t.Errorf("view.Dates = %v, want %v", view.Dates, want)
		}
		if view.Product != "butano" {
			t.Errorf("view.Product = %q, want %q", view.Product, "butano")
		}
		if view.MaterialID != "153" {
			t.Errorf("view.MaterialID = %q, want %q", view.MaterialID, "153")
		}
		if view.Mode != "NO_SCHEDULE" {
			t.Errorf("view.Mode = %q, want %q", view.Mode, "NO_SCHEDULE")
		}
		if view.Warning {
			t.Error("view.Warning = true, want false")
		}

		if got := fake.Count("/api/deliverySchedule"); got != 1 {
			t.Errorf("deliverySchedule calls = %d, want 1", got)
		}
		query := fake.Requests("/api/deliverySchedule")[0].Query
		if got := query.Get("materialId"); got != "153" {
			t.Errorf("deliverySchedule materialId = %q, want %q", got, "153")
		}
	})

	t.Run("no agency", func(t *testing.T) {
		a, fake, _, _ := newTestApp(t)
		mustSetAddress(t, a)
		fake.SetAgency("")

		_, err := a.Schedule(ctx, "")
		typed := requireErrorCode(t, err, "no_delivery")
		if typed.Category != apperr.Refused {
			t.Errorf("category = %v, want %v", typed.Category, apperr.Refused)
		}
		if got := fake.Count("/api/deliverySchedule"); got != 0 {
			t.Errorf("deliverySchedule calls = %d, want 0", got)
		}
	})
}
