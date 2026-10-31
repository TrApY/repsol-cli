package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/config"
	"github.com/felipe9962/repsol-cli/internal/fakerepsol"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// newOrderApp returns an app with the synthetic session and address stored, so
// the order and history use cases can run.
func newOrderApp(t *testing.T) (*App, *fakerepsol.Server, config.Store, *testClock) {
	t.Helper()
	a, fake, st, clock := newTestApp(t)
	storeSession(t, st, config.Credentials{
		LoginToken: fakerepsol.ValidToken,
		GMID:       fakerepsol.ValidGMID,
	})
	mustSetAddress(t, a)
	return a, fake, st, clock
}

// requireFirstRequest returns the first request the fake recorded for path.
func requireFirstRequest(t *testing.T, fake *fakerepsol.Server, path string) fakerepsol.Recorded {
	t.Helper()
	reqs := fake.Requests(path)
	if len(reqs) == 0 {
		t.Fatalf("no request recorded for %s", path)
	}
	return reqs[0]
}

// pendingEntry is a history entry that is pending at the synthetic address.
func pendingEntry(reference, deliveryDate string) repsolapi.HistoryEntry {
	return repsolapi.HistoryEntry{
		ReferenceSAC: reference,
		DeliveryDate: deliveryDate,
		Agency:       repsolapi.HistoryAgency{AgencyCode: fakerepsol.AgencyCode},
		Address:      repsolapi.HistoryAddress{LocalityCode: fakerepsol.LocalityCode},
	}
}

func TestPlaceOrderSolicitude(t *testing.T) {
	ctx := context.Background()
	a, fake, _, _ := newOrderApp(t)

	paths := []string{
		"/api/agency",
		"/api/catalog",
		"/api/deliverySchedule",
		"/api/order-history",
		"/api/cart/check",
		"/api/solicitude",
	}
	before := make(map[string]int, len(paths))
	for _, p := range paths {
		before[p] = fake.Count(p)
	}

	res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}

	for _, p := range paths {
		if got := fake.Count(p) - before[p]; got != 1 {
			t.Errorf("%s requests during PlaceOrder = %d, want 1", p, got)
		}
	}
	if got := fake.Count("/api/order"); got != 0 {
		t.Errorf("/api/order requests = %d, want 0", got)
	}

	if res.ID != fakerepsol.ExternalID {
		t.Errorf("res.ID = %q, want %q", res.ID, fakerepsol.ExternalID)
	}
	if res.DryRun {
		t.Error("res.DryRun = true, want false")
	}
	if res.OrderType != "SOLICITUDE" {
		t.Errorf("res.OrderType = %q, want %q", res.OrderType, "SOLICITUDE")
	}
	if res.Product != "butano" {
		t.Errorf("res.Product = %q, want %q", res.Product, "butano")
	}
	if res.MaterialID != "153" {
		t.Errorf("res.MaterialID = %q, want %q", res.MaterialID, "153")
	}
	if res.Quantity != 1 {
		t.Errorf("res.Quantity = %d, want 1", res.Quantity)
	}
	if res.UnitPrice != 18.89 {
		t.Errorf("res.UnitPrice = %v, want 18.89", res.UnitPrice)
	}
	if res.Total != 18.89 {
		t.Errorf("res.Total = %v, want 18.89", res.Total)
	}
	if res.DeliveryDate != "2026-11-04" {
		t.Errorf("res.DeliveryDate = %q, want %q", res.DeliveryDate, "2026-11-04")
	}
	const wantAddress = "Calle Falsa 1, 28001 Pueblo, Madrid"
	if res.Address != wantAddress {
		t.Errorf("res.Address = %q, want %q", res.Address, wantAddress)
	}
	if res.Agency.Name != "Agencia Test" {
		t.Errorf("res.Agency.Name = %q, want %q", res.Agency.Name, "Agencia Test")
	}
	if res.Agency.Phone != "900 000 000" {
		t.Errorf("res.Agency.Phone = %q, want %q", res.Agency.Phone, "900 000 000")
	}
	if res.Agency.Code != fakerepsol.AgencyCode {
		t.Errorf("res.Agency.Code = %q, want %q", res.Agency.Code, fakerepsol.AgencyCode)
	}
	if res.Payment != "on_delivery" {
		t.Errorf("res.Payment = %q, want %q", res.Payment, "on_delivery")
	}

	var body repsolapi.SolicitudeRequest
	if err := json.Unmarshal(requireFirstRequest(t, fake, "/api/solicitude").Body, &body); err != nil {
		t.Fatalf("decoding the solicitude body: %v", err)
	}
	if body.Status != "EE04" {
		t.Errorf("solicitude status = %q, want %q", body.Status, "EE04")
	}
	if body.MaterialID != "153" {
		t.Errorf("solicitude materialId = %q, want %q", body.MaterialID, "153")
	}
	if body.DeliveryDateID != "1" {
		t.Errorf("solicitude deliveryDateId = %q, want %q", body.DeliveryDateID, "1")
	}
	if body.DeliveryDate != "2026-11-04" {
		t.Errorf("solicitude deliveryDate = %q, want %q", body.DeliveryDate, "2026-11-04")
	}
	if body.UnitPrice != 18.89 {
		t.Errorf("solicitude unitPrice = %v, want 18.89", body.UnitPrice)
	}
	if body.Quantity != 1 {
		t.Errorf("solicitude quantity = %d, want 1", body.Quantity)
	}
	if body.Bill {
		t.Error("solicitude bill = true, want false")
	}
	if body.Discount != 0 {
		t.Errorf("solicitude discount = %d, want 0", body.Discount)
	}
	if body.Address.AgencyCode != fakerepsol.AgencyCode {
		t.Errorf("solicitude address agencyCode = %q, want %q", body.Address.AgencyCode, fakerepsol.AgencyCode)
	}
	if body.Address.LocalityCode != fakerepsol.LocalityCode {
		t.Errorf("solicitude address localityCode = %q, want %q", body.Address.LocalityCode, fakerepsol.LocalityCode)
	}

	var check repsolapi.CartCheckRequest
	if err := json.Unmarshal(requireFirstRequest(t, fake, "/api/cart/check").Body, &check); err != nil {
		t.Fatalf("decoding the cart check body: %v", err)
	}
	if check.ScheduleMode == nil || *check.ScheduleMode != "NO_SCHEDULE" {
		t.Errorf("cart check scheduleMode = %v, want %q", check.ScheduleMode, "NO_SCHEDULE")
	}
	if check.DeliveryDateID != "1" {
		t.Errorf("cart check deliveryDateId = %q, want %q", check.DeliveryDateID, "1")
	}
	if check.AgencyCode != fakerepsol.AgencyCode {
		t.Errorf("cart check agencyCode = %q, want %q", check.AgencyCode, fakerepsol.AgencyCode)
	}
}

func TestPlaceOrderDryRunWritesNothing(t *testing.T) {
	ctx := context.Background()
	a, fake, _, _ := newOrderApp(t)

	res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, DryRun: true})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}

	if got := fake.Count("/api/cart/check"); got != 1 {
		t.Errorf("/api/cart/check requests = %d, want 1", got)
	}
	if got := fake.Count("/api/solicitude"); got != 0 {
		t.Errorf("/api/solicitude requests = %d, want 0", got)
	}
	if got := fake.Count("/api/order"); got != 0 {
		t.Errorf("/api/order requests = %d, want 0", got)
	}
	if !res.DryRun {
		t.Error("res.DryRun = false, want true")
	}
	if res.OrderType != "SOLICITUDE" {
		t.Errorf("res.OrderType = %q, want %q", res.OrderType, "SOLICITUDE")
	}
	if res.ID != "" {
		t.Errorf("res.ID = %q, want empty", res.ID)
	}
	if res.DeliveryDate != "2026-11-04" {
		t.Errorf("res.DeliveryDate = %q, want %q", res.DeliveryDate, "2026-11-04")
	}
	if res.Payment != "on_delivery" {
		t.Errorf("res.Payment = %q, want %q", res.Payment, "on_delivery")
	}
}

func TestPendingGuardBlocks(t *testing.T) {
	ctx := context.Background()
	a, fake, _, _ := newOrderApp(t)
	fake.SetHistory([]repsolapi.HistoryEntry{pendingEntry("10000009", "2026-11-03")})

	_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1})
	typed := requireErrorCode(t, err, "pending_order")
	if typed.Category != apperr.Refused {
		t.Errorf("category = %v, want %v", typed.Category, apperr.Refused)
	}
	const want = "order 10000009 for 2026-11-03 is still pending at this address"
	if typed.Message != want {
		t.Errorf("message = %q, want %q", typed.Message, want)
	}
	if !strings.Contains(typed.Hint, "--force") {
		t.Errorf("hint %q does not mention --force", typed.Hint)
	}
	if got := fake.Count("/api/cart/check"); got != 0 {
		t.Errorf("/api/cart/check requests = %d, want 0", got)
	}
	if got := fake.Count("/api/solicitude"); got != 0 {
		t.Errorf("/api/solicitude requests = %d, want 0", got)
	}

	res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Force: true})
	if err != nil {
		t.Fatalf("PlaceOrder with --force: %v", err)
	}
	if res.ID != fakerepsol.ExternalID {
		t.Errorf("res.ID = %q, want %q", res.ID, fakerepsol.ExternalID)
	}
	if got := fake.Count("/api/solicitude"); got != 1 {
		t.Errorf("/api/solicitude requests = %d, want 1", got)
	}
}

func TestPlaceOrderRefusesConcurrentOrder(t *testing.T) {
	ctx := context.Background()
	a, fake, _, _ := newOrderApp(t)

	entered, release := fake.Hold("/api/solicitude")
	t.Cleanup(release)

	type outcome struct {
		res OrderResult
		err error
	}
	first := make(chan outcome, 1)
	go func() {
		res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1})
		first <- outcome{res, err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the first order never reached /api/solicitude")
	}

	checks := fake.Count("/api/cart/check")
	for _, in := range []PlaceOrderInput{
		{Quantity: 1},
		{Quantity: 1, Force: true},
	} {
		_, err := a.PlaceOrder(ctx, in)
		typed := requireErrorCode(t, err, "order_in_progress")
		if typed.Category != apperr.Refused {
			t.Errorf("force=%v: category = %v, want %v", in.Force, typed.Category, apperr.Refused)
		}
		if !strings.Contains(typed.Hint, "repsol order last") {
			t.Errorf("force=%v: hint %q does not mention repsol order last", in.Force, typed.Hint)
		}
	}
	if got := fake.Count("/api/cart/check"); got != checks {
		t.Errorf("/api/cart/check requests = %d, want %d: a refused order reached the cart check", got, checks)
	}

	// A dry run places nothing, so it does not wait for the lock.
	if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, DryRun: true}); err != nil {
		t.Fatalf("dry run during an order: %v", err)
	}

	release()
	got := <-first
	if got.err != nil {
		t.Fatalf("first order: %v", got.err)
	}
	if got.res.ID != fakerepsol.ExternalID {
		t.Errorf("first order id = %q, want %q", got.res.ID, fakerepsol.ExternalID)
	}
	if n := fake.Count("/api/solicitude"); n != 1 {
		t.Errorf("/api/solicitude requests = %d, want 1", n)
	}

	// The lock is released when the order returns.
	if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Force: true}); err != nil {
		t.Fatalf("order after the first one finished: %v", err)
	}
}

func TestPendingGuardScope(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name  string
		entry repsolapi.HistoryEntry
	}{
		{
			name: "another locality",
			entry: repsolapi.HistoryEntry{
				ReferenceSAC: "10000010",
				DeliveryDate: "2026-11-03",
				Agency:       repsolapi.HistoryAgency{AgencyCode: fakerepsol.AgencyCode},
				Address:      repsolapi.HistoryAddress{LocalityCode: "99999999999"},
			},
		},
		{
			name: "another agency",
			entry: repsolapi.HistoryEntry{
				ReferenceSAC: "10000011",
				DeliveryDate: "2026-11-03",
				Agency:       repsolapi.HistoryAgency{AgencyCode: "0000000002"},
				Address:      repsolapi.HistoryAddress{LocalityCode: fakerepsol.LocalityCode},
			},
		},
		{
			name: "yesterday",
			entry: repsolapi.HistoryEntry{
				ReferenceSAC: "10000012",
				DeliveryDate: "2026-10-31",
				Agency:       repsolapi.HistoryAgency{AgencyCode: fakerepsol.AgencyCode},
				Address:      repsolapi.HistoryAddress{LocalityCode: fakerepsol.LocalityCode},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, fake, _, _ := newOrderApp(t)
			fake.SetHistory([]repsolapi.HistoryEntry{tt.entry})

			if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1}); err != nil {
				t.Fatalf("PlaceOrder: %v", err)
			}
			if got := fake.Count("/api/solicitude"); got != 1 {
				t.Errorf("/api/solicitude requests = %d, want 1", got)
			}
		})
	}
}

func TestPendingGuardUsesMadridDate(t *testing.T) {
	ctx := context.Background()
	// 23:30 UTC on 2026-11-04 is 00:30 on 2026-11-05 in Europe/Madrid.
	madridMidnight := time.Date(2026, 11, 4, 23, 30, 0, 0, time.UTC)

	t.Run("yesterday in Madrid is not pending", func(t *testing.T) {
		a, fake, _, clock := newOrderApp(t)
		clock.t = madridMidnight
		fake.SetHistory([]repsolapi.HistoryEntry{pendingEntry("10000013", "2026-11-04")})

		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1}); err != nil {
			t.Fatalf("PlaceOrder: %v", err)
		}
		if got := fake.Count("/api/solicitude"); got != 1 {
			t.Errorf("/api/solicitude requests = %d, want 1", got)
		}
	})

	t.Run("today in Madrid is pending", func(t *testing.T) {
		a, fake, _, clock := newOrderApp(t)
		clock.t = madridMidnight
		fake.SetHistory([]repsolapi.HistoryEntry{pendingEntry("10000014", "2026-11-05")})

		_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1})
		typed := requireErrorCode(t, err, "pending_order")
		if typed.Category != apperr.Refused {
			t.Errorf("category = %v, want %v", typed.Category, apperr.Refused)
		}
		if !strings.Contains(typed.Message, "10000014") {
			t.Errorf("message %q does not name the pending order", typed.Message)
		}
		if got := fake.Count("/api/solicitude"); got != 0 {
			t.Errorf("/api/solicitude requests = %d, want 0", got)
		}
	})
}

func TestOnlinePolicyRefused(t *testing.T) {
	ctx := context.Background()
	a, fake, _, _ := newOrderApp(t)
	fake.SetCheck(repsolapi.CartCheck{Status: "EE04", OnlinePolicy: true, OrderType: "SOLICITUDE"})

	_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1})
	typed := requireErrorCode(t, err, "online_policy_required")
	if typed.Category != apperr.Refused {
		t.Errorf("category = %v, want %v", typed.Category, apperr.Refused)
	}
	if !strings.Contains(typed.Hint, "https://pidetubombona.repsol.es/") {
		t.Errorf("hint %q does not point at the web flow", typed.Hint)
	}
	if got := fake.Count("/api/cart/check"); got != 1 {
		t.Errorf("/api/cart/check requests = %d, want 1", got)
	}
	if got := fake.Count("/api/solicitude"); got != 0 {
		t.Errorf("/api/solicitude requests = %d, want 0", got)
	}
	if got := fake.Count("/api/order"); got != 0 {
		t.Errorf("/api/order requests = %d, want 0", got)
	}
}

func TestOrderBranch(t *testing.T) {
	ctx := context.Background()
	a, fake, _, _ := newOrderApp(t)
	fake.SetCheck(repsolapi.CartCheck{Status: "EE01", Order: true, OrderType: "ORDER"})

	res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}

	if got := fake.Count("/api/order"); got != 1 {
		t.Errorf("/api/order requests = %d, want 1", got)
	}
	if got := fake.Count("/api/solicitude"); got != 0 {
		t.Errorf("/api/solicitude requests = %d, want 0", got)
	}
	if res.OrderType != "ORDER" {
		t.Errorf("res.OrderType = %q, want %q", res.OrderType, "ORDER")
	}
	if res.ID == "" {
		t.Error("res.ID is empty, want the id the order route returned")
	}
}

func TestWriteFailureIsOutcomeUnknown(t *testing.T) {
	ctx := context.Background()
	a, fake, _, _ := newOrderApp(t)
	fake.FailNext("/api/solicitude", 502)

	_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1})
	if err == nil {
		t.Fatal("PlaceOrder succeeded, want an outcome-unknown error")
	}
	typed := requireErrorCode(t, err, "order_outcome_unknown")
	if typed.Category != apperr.OutcomeUnknown {
		t.Errorf("category = %v, want %v", typed.Category, apperr.OutcomeUnknown)
	}
	if got := apperr.ExitCode(err); got != 9 {
		t.Errorf("exit code = %d, want 9", got)
	}
	if got := fake.Count("/api/solicitude"); got != 1 {
		t.Errorf("/api/solicitude requests = %d, want 1", got)
	}
}

func TestOrderInputValidation(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		in       PlaceOrderInput
		code     string
		category apperr.Category
		contains string
	}{
		{
			name:     "quantity above the maximum",
			in:       PlaceOrderInput{Quantity: 10},
			code:     "invalid_quantity",
			category: apperr.Usage,
			contains: "maximum of 9",
		},
		{
			name:     "quantity below one",
			in:       PlaceOrderInput{Quantity: 0},
			code:     "invalid_quantity",
			category: apperr.Usage,
		},
		{
			name: "the larger bottles accept 24",
			in:   PlaceOrderInput{Product: "propano", Quantity: 24, DryRun: true},
		},
		{
			name:     "date not offered",
			in:       PlaceOrderInput{Quantity: 1, Date: "2030-01-01"},
			code:     "date_unavailable",
			category: apperr.Refused,
			contains: "2026-11-04",
		},
		{
			name:     "unsupported payment",
			in:       PlaceOrderInput{Pay: "card"},
			code:     "unsupported_payment",
			category: apperr.Usage,
		},
		{
			name:     "malformed date",
			in:       PlaceOrderInput{Date: "5/11/2026"},
			code:     "invalid_date",
			category: apperr.Usage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, fake, _, _ := newOrderApp(t)

			res, err := a.PlaceOrder(ctx, tt.in)
			if tt.code == "" {
				if err != nil {
					t.Fatalf("PlaceOrder: %v", err)
				}
				if !res.DryRun {
					t.Error("res.DryRun = false, want true")
				}
				if got := fake.Count("/api/solicitude"); got != 0 {
					t.Errorf("/api/solicitude requests = %d, want 0", got)
				}
				return
			}

			typed := requireErrorCode(t, err, tt.code)
			if typed.Category != tt.category {
				t.Errorf("category = %v, want %v", typed.Category, tt.category)
			}
			if tt.contains != "" && !strings.Contains(typed.Message+" "+typed.Hint, tt.contains) {
				t.Errorf("message %q and hint %q do not mention %q", typed.Message, typed.Hint, tt.contains)
			}
			if got := fake.Count("/api/cart/check"); got != 0 {
				t.Errorf("/api/cart/check requests = %d, want 0", got)
			}
		})
	}
}
