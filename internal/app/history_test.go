package app

import (
	"context"
	"testing"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/fakerepsol"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

func TestHistoryViews(t *testing.T) {
	ctx := context.Background()

	t.Run("list after a placed order", func(t *testing.T) {
		a, _, _, _ := newOrderApp(t)
		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1}); err != nil {
			t.Fatalf("PlaceOrder: %v", err)
		}

		orders, err := a.ListOrders(ctx, 10)
		if err != nil {
			t.Fatalf("ListOrders: %v", err)
		}
		if len(orders) != 1 {
			t.Fatalf("len(orders) = %d, want 1", len(orders))
		}

		o := orders[0]
		if o.Reference != "20000001" {
			t.Errorf("orders[0].Reference = %q, want %q", o.Reference, "20000001")
		}
		if !o.Pending {
			t.Error("orders[0].Pending = false, want true")
		}
		if o.CreatedAt != "2026-11-01T00:00:00+01:00" {
			t.Errorf("orders[0].CreatedAt = %q, want the RFC 3339 form", o.CreatedAt)
		}
		if o.DeliveryDate != "2026-11-04" {
			t.Errorf("orders[0].DeliveryDate = %q, want %q", o.DeliveryDate, "2026-11-04")
		}
		if o.Product != "butano" {
			t.Errorf("orders[0].Product = %q, want %q", o.Product, "butano")
		}
		if o.MaterialID != "153" {
			t.Errorf("orders[0].MaterialID = %q, want %q", o.MaterialID, "153")
		}
		if o.Quantity != 1 {
			t.Errorf("orders[0].Quantity = %d, want 1", o.Quantity)
		}
		if o.UnitPrice != 18.89 {
			t.Errorf("orders[0].UnitPrice = %v, want 18.89", o.UnitPrice)
		}
		if o.Total != 18.89 {
			t.Errorf("orders[0].Total = %v, want 18.89", o.Total)
		}
		const wantAddress = "Calle Falsa 1, Pueblo"
		if o.Address != wantAddress {
			t.Errorf("orders[0].Address = %q, want %q", o.Address, wantAddress)
		}
		if o.Agency.Name != "Agencia Test" {
			t.Errorf("orders[0].Agency.Name = %q, want %q", o.Agency.Name, "Agencia Test")
		}
		if o.Agency.Phone != "900 000 000" {
			t.Errorf("orders[0].Agency.Phone = %q, want %q", o.Agency.Phone, "900 000 000")
		}
		if o.Agency.Code != fakerepsol.AgencyCode {
			t.Errorf("orders[0].Agency.Code = %q, want %q", o.Agency.Code, fakerepsol.AgencyCode)
		}
	})

	t.Run("show", func(t *testing.T) {
		a, _, _, _ := newOrderApp(t)
		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1}); err != nil {
			t.Fatalf("PlaceOrder: %v", err)
		}

		shown, err := a.ShowOrder(ctx, "20000001")
		if err != nil {
			t.Fatalf("ShowOrder: %v", err)
		}
		if shown.Reference != "20000001" {
			t.Errorf("ShowOrder reference = %q, want %q", shown.Reference, "20000001")
		}
		if !shown.Pending {
			t.Error("ShowOrder pending = false, want true")
		}

		_, err = a.ShowOrder(ctx, "1234")
		typed := requireErrorCode(t, err, "invalid_reference")
		if typed.Category != apperr.Usage {
			t.Errorf("category = %v, want %v", typed.Category, apperr.Usage)
		}

		_, err = a.ShowOrder(ctx, "99999999")
		typed = requireErrorCode(t, err, "order_not_found")
		if typed.Category != apperr.NotFound {
			t.Errorf("category = %v, want %v", typed.Category, apperr.NotFound)
		}
	})

	t.Run("last", func(t *testing.T) {
		a, _, _, _ := newOrderApp(t)

		_, err := a.LastOrder(ctx)
		typed := requireErrorCode(t, err, "no_orders")
		if typed.Category != apperr.NotFound {
			t.Errorf("category = %v, want %v", typed.Category, apperr.NotFound)
		}

		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1}); err != nil {
			t.Fatalf("PlaceOrder: %v", err)
		}
		last, err := a.LastOrder(ctx)
		if err != nil {
			t.Fatalf("LastOrder: %v", err)
		}
		if last.Reference != "20000001" {
			t.Errorf("LastOrder reference = %q, want %q", last.Reference, "20000001")
		}
		if !last.Pending {
			t.Error("LastOrder pending = false, want true")
		}
	})

	t.Run("solicitude-style entry", func(t *testing.T) {
		a, fake, _, _ := newOrderApp(t)
		fake.SetHistory([]repsolapi.HistoryEntry{{
			CreatedAt:    "2026-10-28T09:15:00.000+0100",
			DeliveryDate: "2026-11-06",
			ReferenceSAC: "10000007",
			MaterialID:   "153",
			Quantity:     2,
			UnitPrice:    18.89,
			Agency: repsolapi.HistoryAgency{
				Name:       "Agencia Test",
				Phone:      "900 000 000",
				AgencyCode: fakerepsol.AgencyCode,
			},
			Address: repsolapi.HistoryAddress{
				Locality:     "Pueblo",
				LocalityCode: fakerepsol.LocalityCode,
				Details:      "Calle Falsa 1",
			},
		}})

		orders, err := a.ListOrders(ctx, 10)
		if err != nil {
			t.Fatalf("ListOrders: %v", err)
		}
		if len(orders) != 1 {
			t.Fatalf("len(orders) = %d, want 1", len(orders))
		}

		o := orders[0]
		const wantAddress = "Calle Falsa 1, Pueblo"
		if o.Address != wantAddress {
			t.Errorf("orders[0].Address = %q, want %q", o.Address, wantAddress)
		}
		if o.Total != 37.78 {
			t.Errorf("orders[0].Total = %v, want 37.78", o.Total)
		}
		if o.CreatedAt != "2026-10-28T09:15:00+01:00" {
			t.Errorf("orders[0].CreatedAt = %q, want the RFC 3339 form", o.CreatedAt)
		}
	})

	t.Run("limit out of range", func(t *testing.T) {
		a, _, _, _ := newOrderApp(t)

		_, err := a.ListOrders(ctx, 0)
		typed := requireErrorCode(t, err, "invalid_limit")
		if typed.Category != apperr.Usage {
			t.Errorf("category = %v, want %v", typed.Category, apperr.Usage)
		}
	})
}
