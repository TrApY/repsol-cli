package app

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/config"
	"github.com/felipe9962/repsol-cli/internal/fakerepsol"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// The write paths of the Waylet flow, and the two final writes a Waylet order
// must never use.
const (
	pathCart       = "/api/cart"
	pathWayletURL  = "/api/waylet-payment/url"
	pathByPayment  = "/api/order-history/payment-id"
	pathSolicitude = "/api/solicitude"
	pathOrder      = "/api/order"
)

// newWayletApp returns an order app whose cart check selects the order branch,
// the only one a Waylet payment can pay. Its sleep advances the test clock
// instead of waiting, and records every pause.
func newWayletApp(t *testing.T) (*App, *fakerepsol.Server, config.Store, *[]time.Duration) {
	t.Helper()
	a, fake, st, clock := newOrderApp(t)
	fake.SetCheck(repsolapi.CartCheck{Status: "EE04", Order: true, OrderType: "ORDER"})
	var pauses []time.Duration
	a.sleep = func(ctx context.Context, d time.Duration) error {
		pauses = append(pauses, d)
		clock.Advance(d)
		return ctx.Err()
	}
	return a, fake, st, &pauses
}

// requireNoWayletWrites asserts that nothing of the payment flow, and no final
// order write, reached the fake.
func requireNoWayletWrites(t *testing.T, fake *fakerepsol.Server) {
	t.Helper()
	for _, p := range []string{pathCart, pathWayletURL, pathSolicitude, pathOrder} {
		if got := fake.Count(p); got != 0 {
			t.Errorf("%s requests = %d, want 0", p, got)
		}
	}
}

func TestWayletAgencyWithoutWayletIsRefused(t *testing.T) {
	a, fake, _, _ := newWayletApp(t)
	fake.SetWayletPayment(false)

	for _, dryRun := range []bool{false, true} {
		_, err := a.PlaceOrder(context.Background(), PlaceOrderInput{Quantity: 1, Pay: "waylet", DryRun: dryRun})
		typed := requireErrorCode(t, err, "waylet_unavailable")
		if typed.Category != apperr.Refused {
			t.Errorf("dry_run=%v: category = %v, want %v", dryRun, typed.Category, apperr.Refused)
		}
		if !strings.Contains(typed.Hint, "--pay delivery") {
			t.Errorf("dry_run=%v: hint %q does not point at --pay delivery", dryRun, typed.Hint)
		}
	}
	requireNoWayletWrites(t, fake)
	if got := fake.Count("/api/cart/check"); got != 0 {
		t.Errorf("/api/cart/check requests = %d, want 0: the agency check comes first", got)
	}
}

func TestWayletSolicitudeCannotBePaidOnline(t *testing.T) {
	a, fake, _, _ := newWayletApp(t)
	fake.SetCheck(repsolapi.CartCheck{Status: "EE04", OrderType: "SOLICITUDE"})

	_, err := a.PlaceOrder(context.Background(), PlaceOrderInput{Quantity: 1, Pay: "waylet"})
	typed := requireErrorCode(t, err, "waylet_unavailable")
	if typed.Category != apperr.Refused {
		t.Errorf("category = %v, want %v", typed.Category, apperr.Refused)
	}
	if !strings.Contains(typed.Message, "request") {
		t.Errorf("message %q does not say the order is a request", typed.Message)
	}
	requireNoWayletWrites(t, fake)
}

func TestWayletDryRunCreatesNothing(t *testing.T) {
	a, fake, _, _ := newWayletApp(t)

	res, err := a.PlaceOrder(context.Background(), PlaceOrderInput{Quantity: 2, Pay: "waylet", DryRun: true})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if !res.DryRun || res.Payment != "waylet" || res.OrderType != "ORDER" {
		t.Errorf("result = %+v, want a waylet ORDER dry run", res)
	}
	if res.PaymentURL != "" || res.PaymentID != "" || res.PaymentStatus != "" || res.ID != "" {
		t.Errorf("dry run carries payment data: %+v", res)
	}
	if res.Total != 37.78 {
		t.Errorf("total = %v, want 37.78", res.Total)
	}
	if got := fake.Count("/api/cart/check"); got != 1 {
		t.Errorf("/api/cart/check requests = %d, want 1", got)
	}
	requireNoWayletWrites(t, fake)
}

func TestWayletReturnsPaymentURL(t *testing.T) {
	a, fake, _, pauses := newWayletApp(t)

	res, err := a.PlaceOrder(context.Background(), PlaceOrderInput{Quantity: 2, Pay: "waylet"})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if res.Payment != "waylet" || res.OrderType != "ORDER" || res.PaymentStatus != "awaiting_payment" {
		t.Errorf("result = %+v, want a waylet ORDER awaiting payment", res)
	}
	if res.ID != "" {
		t.Errorf("ID = %q, want empty before the payment is confirmed", res.ID)
	}
	if res.PaymentID != "fake-payment-1" {
		t.Errorf("PaymentID = %q, want fake-payment-1", res.PaymentID)
	}
	u, err := url.Parse(res.PaymentURL)
	if err != nil || u.Scheme != "https" || u.Host != fakerepsol.WayletHost || u.Query().Get("paymentId") != res.PaymentID {
		t.Errorf("PaymentURL = %q, want the https Waylet page of %s", res.PaymentURL, res.PaymentID)
	}
	if res.Quantity != 2 || res.Total != 37.78 || res.DeliveryDate != "2026-11-04" {
		t.Errorf("result = %+v, want 2 bottles, 37.78 EUR, 2026-11-04", res)
	}

	// One cart, one URL, no lookup, and none of the final order writes.
	for p, want := range map[string]int{pathCart: 1, pathWayletURL: 1, pathByPayment: 0, pathSolicitude: 0, pathOrder: 0} {
		if got := fake.Count(p); got != want {
			t.Errorf("%s requests = %d, want %d", p, got, want)
		}
	}
	if len(*pauses) != 0 {
		t.Errorf("slept %v without --wait", *pauses)
	}

	var body repsolapi.TemporaryOrderRequest
	if err := json.Unmarshal(requireFirstRequest(t, fake, pathCart).Body, &body); err != nil {
		t.Fatalf("decoding the cart body: %v", err)
	}
	if body.UserGigyaUID != fakerepsol.UID || body.MaterialID != "153" || body.Quantity != 2 ||
		body.UnitPrice != 18.89 || body.DeliveryDateID != "1" || body.DeliveryDate != "2026-11-04" ||
		body.AgencyCode != fakerepsol.AgencyCode || body.Tip != 0 {
		t.Errorf("cart body = %+v", body)
	}
	if body.Address.LocalityCode != fakerepsol.LocalityCode {
		t.Errorf("cart address localityCode = %q, want %q", body.Address.LocalityCode, fakerepsol.LocalityCode)
	}
}

func TestWayletWaitConfirmsThePayment(t *testing.T) {
	a, fake, _, pauses := newWayletApp(t)
	fake.SetWayletPayAfter(3)

	var linkAt []int
	res, err := a.PlaceOrder(context.Background(), PlaceOrderInput{
		Quantity: 1,
		Pay:      "waylet",
		Wait:     15 * time.Minute,
		OnPaymentLink: func(l PaymentLink) {
			linkAt = append(linkAt, fake.Count(pathByPayment))
			if l.ID != "fake-payment-1" || !strings.HasPrefix(l.URL, "https://"+fakerepsol.WayletHost) {
				t.Errorf("link = %+v", l)
			}
		},
	})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}

	if len(linkAt) != 1 || linkAt[0] != 0 {
		t.Errorf("OnPaymentLink calls with lookups so far = %v, want exactly one call before the first lookup", linkAt)
	}
	if res.PaymentStatus != "confirmed" {
		t.Errorf("PaymentStatus = %q, want confirmed", res.PaymentStatus)
	}
	if res.ID != "800000000000000003" {
		t.Errorf("ID = %q, want the id of the paid order", res.ID)
	}
	if res.DeliveryDate != "2026-11-04" || res.Agency.Name != "Agencia Test" || res.Agency.Code != fakerepsol.AgencyCode {
		t.Errorf("result = %+v, want the delivery date and agency of the paid order", res)
	}
	if res.PaymentURL == "" || res.PaymentID != "fake-payment-1" {
		t.Errorf("result lost the payment link: %+v", res)
	}
	if got := fake.Count(pathByPayment); got != 3 {
		t.Errorf("%s requests = %d, want 3", pathByPayment, got)
	}
	if len(*pauses) != 2 || (*pauses)[0] != PaymentPollInterval {
		t.Errorf("pauses = %v, want two of %v", *pauses, PaymentPollInterval)
	}
	// Waiting only reads: no cart or URL was requested twice, and nothing was
	// written as an order by the CLI itself.
	for p, want := range map[string]int{pathCart: 1, pathWayletURL: 1, pathSolicitude: 0, pathOrder: 0} {
		if got := fake.Count(p); got != want {
			t.Errorf("%s requests = %d, want %d", p, got, want)
		}
	}
}

func TestWayletWaitTimesOut(t *testing.T) {
	a, fake, _, pauses := newWayletApp(t)

	_, err := a.PlaceOrder(context.Background(), PlaceOrderInput{
		Quantity: 1,
		Pay:      "waylet",
		Wait:     time.Minute,
	})
	typed := requireErrorCode(t, err, "payment_timeout")
	if typed.Category != apperr.Network || !typed.Retryable {
		t.Errorf("category = %v retryable = %v, want a retryable network error", typed.Category, typed.Retryable)
	}
	if !strings.Contains(typed.Hint, "repsol order payment fake-payment-1") {
		t.Errorf("hint %q does not name the lookup command and the payment id", typed.Hint)
	}

	polls := fake.Count(pathByPayment)
	if polls < 2 || polls > int(time.Minute/PaymentPollInterval)+2 {
		t.Errorf("%s requests = %d, want about one per %v for a minute", pathByPayment, polls, PaymentPollInterval)
	}
	if len(*pauses) != polls-1 && len(*pauses) != polls {
		t.Errorf("pauses = %d for %d lookups", len(*pauses), polls)
	}
	// A timeout never writes again.
	for p, want := range map[string]int{pathCart: 1, pathWayletURL: 1, pathSolicitude: 0, pathOrder: 0} {
		if got := fake.Count(p); got != want {
			t.Errorf("%s requests = %d, want %d", p, got, want)
		}
	}
}

func TestWayletWaitInterrupted(t *testing.T) {
	a, fake, _, _ := newWayletApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a.sleep = func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}

	_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", Wait: time.Hour})
	typed := requireErrorCode(t, err, "interrupted")
	if typed.Category != apperr.Interrupted {
		t.Errorf("category = %v, want %v", typed.Category, apperr.Interrupted)
	}
	if !strings.Contains(typed.Hint, "repsol order payment fake-payment-1") {
		t.Errorf("hint %q does not tell how to check the payment", typed.Hint)
	}
	if got := fake.Count(pathCart); got != 1 {
		t.Errorf("%s requests = %d, want 1", pathCart, got)
	}
}

func TestWayletWritesAreNotRetried(t *testing.T) {
	t.Run("cart", func(t *testing.T) {
		a, fake, _, _ := newWayletApp(t)
		fake.FailNext(pathCart, 502)

		_, err := a.PlaceOrder(context.Background(), PlaceOrderInput{Quantity: 1, Pay: "waylet", Wait: time.Minute})
		if err == nil {
			t.Fatal("PlaceOrder: want an error")
		}
		if got := apperr.From(err).Category; got == apperr.OutcomeUnknown {
			t.Errorf("category = %v: a failed cart places nothing, so it is not an unknown order outcome", got)
		}
		if fake.Count(pathCart) != 1 || fake.Count(pathWayletURL) != 0 || fake.Count(pathByPayment) != 0 {
			t.Errorf("cart=%d url=%d lookups=%d, want 1, 0, 0",
				fake.Count(pathCart), fake.Count(pathWayletURL), fake.Count(pathByPayment))
		}
	})
	t.Run("url", func(t *testing.T) {
		a, fake, _, _ := newWayletApp(t)
		fake.FailNext(pathWayletURL, 502)

		_, err := a.PlaceOrder(context.Background(), PlaceOrderInput{Quantity: 1, Pay: "waylet", Wait: time.Minute})
		if err == nil {
			t.Fatal("PlaceOrder: want an error")
		}
		if fake.Count(pathCart) != 1 || fake.Count(pathWayletURL) != 1 || fake.Count(pathByPayment) != 0 {
			t.Errorf("cart=%d url=%d lookups=%d, want 1, 1, 0",
				fake.Count(pathCart), fake.Count(pathWayletURL), fake.Count(pathByPayment))
		}
	})
}

func TestWayletPendingOrderBlocksAnotherOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("pending delivery blocks the payment", func(t *testing.T) {
		a, fake, _, _ := newWayletApp(t)
		fake.SetHistory([]repsolapi.HistoryEntry{pendingEntry("10000009", "2026-11-03")})

		_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		requireErrorCode(t, err, "pending_order")
		requireNoWayletWrites(t, fake)

		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", Force: true}); err != nil {
			t.Fatalf("PlaceOrder with --force: %v", err)
		}
		if got := fake.Count(pathCart); got != 1 {
			t.Errorf("%s requests = %d, want 1", pathCart, got)
		}
	})

	t.Run("a paid order blocks the next one", func(t *testing.T) {
		a, fake, _, _ := newWayletApp(t)
		fake.SetWayletPayAfter(1)

		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", Wait: time.Minute}); err != nil {
			t.Fatalf("first order: %v", err)
		}
		for _, pay := range []string{"waylet", "delivery"} {
			_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: pay})
			requireErrorCode(t, err, "pending_order")
		}
		if fake.Count(pathCart) != 1 || fake.Count(pathSolicitude) != 0 || fake.Count(pathOrder) != 0 {
			t.Errorf("a second order reached a write: cart=%d solicitude=%d order=%d",
				fake.Count(pathCart), fake.Count(pathSolicitude), fake.Count(pathOrder))
		}
	})

	t.Run("a waiting order holds the profile lock", func(t *testing.T) {
		a, fake, _, _ := newWayletApp(t)
		fake.SetWayletPayAfter(2)
		entered, release := fake.Hold(pathByPayment)
		t.Cleanup(release)

		type outcome struct {
			res OrderResult
			err error
		}
		first := make(chan outcome, 1)
		go func() {
			res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", Wait: time.Hour})
			first <- outcome{res, err}
		}()
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("the first order never reached the payment lookup")
		}

		carts := fake.Count(pathCart)
		for _, in := range []PlaceOrderInput{
			{Quantity: 1, Pay: "waylet"},
			{Quantity: 1, Pay: "waylet", Force: true},
			{Quantity: 1, Pay: "delivery", Force: true},
		} {
			_, err := a.PlaceOrder(ctx, in)
			requireErrorCode(t, err, "order_in_progress")
		}
		if got := fake.Count(pathCart); got != carts {
			t.Errorf("%s requests = %d, want %d: a refused order created a cart", pathCart, got, carts)
		}
		// A dry run takes no lock.
		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", DryRun: true}); err != nil {
			t.Fatalf("dry run during a waiting order: %v", err)
		}

		release()
		got := <-first
		if got.err != nil {
			t.Fatalf("first order: %v", got.err)
		}
		if got.res.PaymentStatus != "confirmed" {
			t.Errorf("first order status = %q, want confirmed", got.res.PaymentStatus)
		}
	})
}

func TestWayletOptionsAreValidated(t *testing.T) {
	cases := []struct {
		name string
		in   PlaceOrderInput
		code string
	}{
		{"wait needs waylet", PlaceOrderInput{Wait: time.Minute}, "wait_requires_waylet"},
		{"wait needs waylet with delivery", PlaceOrderInput{Pay: "delivery", Wait: time.Minute}, "wait_requires_waylet"},
		{"negative wait", PlaceOrderInput{Pay: "waylet", Wait: -time.Second}, "invalid_wait"},
		{"unknown method", PlaceOrderInput{Pay: "card"}, "unsupported_payment"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, fake, _, _ := newWayletApp(t)
			agencyBefore := fake.Count("/api/agency")
			tc.in.Quantity = 1
			_, err := a.PlaceOrder(context.Background(), tc.in)
			typed := requireErrorCode(t, err, tc.code)
			if typed.Category != apperr.Usage {
				t.Errorf("category = %v, want %v", typed.Category, apperr.Usage)
			}
			requireNoWayletWrites(t, fake)
			if got := fake.Count("/api/agency") - agencyBefore; got != 0 {
				t.Errorf("/api/agency requests = %d, want 0: bad input stops before any request", got)
			}
		})
	}
}

func TestPaymentStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("not paid yet", func(t *testing.T) {
		a, fake, _, _ := newWayletApp(t)
		res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("PlaceOrder: %v", err)
		}

		view, err := a.PaymentStatus(ctx, res.PaymentID, 0)
		if err != nil {
			t.Fatalf("PaymentStatus: %v", err)
		}
		if view.Status != "awaiting_payment" || view.ID != "" || view.PaymentID != res.PaymentID {
			t.Errorf("view = %+v, want awaiting_payment for %s", view, res.PaymentID)
		}
		if got := fake.Count(pathByPayment); got != 1 {
			t.Errorf("%s requests = %d, want 1", pathByPayment, got)
		}
	})

	t.Run("paid", func(t *testing.T) {
		a, fake, _, _ := newWayletApp(t)
		res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("PlaceOrder: %v", err)
		}
		if !fake.ConfirmWaylet(res.PaymentID) {
			t.Fatal("the fake did not know the payment")
		}

		view, err := a.PaymentStatus(ctx, res.PaymentID, 0)
		if err != nil {
			t.Fatalf("PaymentStatus: %v", err)
		}
		if view.Status != "confirmed" || view.ID != "800000000000000003" || view.DeliveryDate != "2026-11-04" {
			t.Errorf("view = %+v, want the confirmed order", view)
		}
		if view.Agency == nil || view.Agency.Name != "Agencia Test" {
			t.Errorf("agency = %+v, want Agencia Test", view.Agency)
		}
	})

	t.Run("wait for a payment made later", func(t *testing.T) {
		a, fake, _, _ := newWayletApp(t)
		res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("PlaceOrder: %v", err)
		}
		fake.SetWayletPayAfter(3)

		view, err := a.PaymentStatus(ctx, res.PaymentID, 15*time.Minute)
		if err != nil {
			t.Fatalf("PaymentStatus: %v", err)
		}
		if view.Status != "confirmed" {
			t.Errorf("status = %q, want confirmed", view.Status)
		}
	})

	t.Run("wait that expires", func(t *testing.T) {
		a, _, _, _ := newWayletApp(t)
		res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("PlaceOrder: %v", err)
		}
		_, err = a.PaymentStatus(ctx, res.PaymentID, 30*time.Second)
		requireErrorCode(t, err, "payment_timeout")
	})

	t.Run("bad input", func(t *testing.T) {
		a, fake, _, _ := newWayletApp(t)
		for _, id := range []string{"", "  "} {
			_, err := a.PaymentStatus(ctx, id, 0)
			requireErrorCode(t, err, "invalid_payment_id")
		}
		_, err := a.PaymentStatus(ctx, "p", -time.Second)
		requireErrorCode(t, err, "invalid_wait")
		if got := fake.Count(pathByPayment); got != 0 {
			t.Errorf("%s requests = %d, want 0", pathByPayment, got)
		}
	})
}
