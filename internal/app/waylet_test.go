package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
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
	a, fake, st, _, pauses := newWayletAppClock(t)
	return a, fake, st, pauses
}

// newWayletAppClock is newWayletApp that also returns the test clock.
func newWayletAppClock(t *testing.T) (*App, *fakerepsol.Server, config.Store, *testClock, *[]time.Duration) {
	t.Helper()
	a, fake, st, clock := newOrderApp(t)
	fake.SetCheck(repsolapi.CartCheck{Status: "EE04", Order: true, OrderType: "ORDER"})
	var pauses []time.Duration
	a.sleep = func(ctx context.Context, d time.Duration) error {
		pauses = append(pauses, d)
		clock.Advance(d)
		return ctx.Err()
	}
	return a, fake, st, clock, &pauses
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
	// Creating the order again would create a second payment, so the error
	// must not invite a retry of the command.
	if typed.Category != apperr.Network || typed.Retryable {
		t.Errorf("category = %v retryable = %v, want a network error that is not retryable", typed.Category, typed.Retryable)
	}
	if !strings.Contains(typed.Hint, "repsol order payment fake-payment-1 --wait") {
		t.Errorf("hint %q does not name the lookup command, the payment id and --wait", typed.Hint)
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
		// A dry run takes no lock. It still reports the unresolved payment
		// the waiting order started, as a real order would meet it.
		_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", DryRun: true})
		requireErrorCode(t, err, "payment_pending")

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

// requirePendingPayment asserts the payment the profile still tracks, and
// returns whether there is one.
func requirePendingPayment(t *testing.T, st config.Store, want string) {
	t.Helper()
	got, found, err := st.LoadPendingPayment(defaultTestProfile)
	if err != nil {
		t.Fatalf("LoadPendingPayment: %v", err)
	}
	switch {
	case want == "" && found:
		t.Errorf("pending payment = %q, want none", got.PaymentID)
	case want != "" && !found:
		t.Errorf("no pending payment, want %q", want)
	case want != "" && got.PaymentID != want:
		t.Errorf("pending payment = %q, want %q", got.PaymentID, want)
	}
}

func TestWayletSecondCreateWhileUnpaidIsRefused(t *testing.T) {
	ctx := context.Background()

	// Without --wait the first command returns as soon as it prints the link
	// and releases the lock, and Repsol has no order until the owner pays. The
	// history cannot see that payment, so the CLI has to remember it.
	for _, pay := range []string{"waylet", "delivery"} {
		t.Run("second order "+pay, func(t *testing.T) {
			a, fake, st, _ := newWayletApp(t)
			first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
			if err != nil {
				t.Fatalf("first order: %v", err)
			}
			requirePendingPayment(t, st, first.PaymentID)

			_, err = a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: pay})
			typed := requireErrorCode(t, err, "payment_pending")
			if typed.Category != apperr.Refused || typed.Retryable {
				t.Errorf("category = %v retryable = %v, want a refusal that is not retryable", typed.Category, typed.Retryable)
			}
			for _, want := range []string{"repsol order payment " + first.PaymentID + " --wait", "--abandon-payment"} {
				if !strings.Contains(typed.Hint, want) {
					t.Errorf("hint %q does not mention %q", typed.Hint, want)
				}
			}
			if fake.Count(pathCart) != 1 || fake.Count(pathWayletURL) != 1 ||
				fake.Count(pathSolicitude) != 0 || fake.Count(pathOrder) != 0 {
				t.Errorf("the refused order reached a write: cart=%d url=%d solicitude=%d order=%d",
					fake.Count(pathCart), fake.Count(pathWayletURL), fake.Count(pathSolicitude), fake.Count(pathOrder))
			}
			requirePendingPayment(t, st, first.PaymentID)
		})
	}

	t.Run("abandoning starts a new payment", func(t *testing.T) {
		a, fake, st, _ := newWayletApp(t)
		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"}); err != nil {
			t.Fatalf("first order: %v", err)
		}
		second, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", AbandonPayment: true})
		if err != nil {
			t.Fatalf("order with --abandon-payment: %v", err)
		}
		if second.PaymentID != "fake-payment-2" || fake.Count(pathCart) != 2 {
			t.Errorf("payment = %q carts = %d, want a second payment", second.PaymentID, fake.Count(pathCart))
		}
		// The profile tracks the newest payment.
		requirePendingPayment(t, st, "fake-payment-2")
	})

	t.Run("an order that abandons the payment stops the tracking of it", func(t *testing.T) {
		a, _, st, _ := newWayletApp(t)
		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"}); err != nil {
			t.Fatalf("first order: %v", err)
		}
		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery", AbandonPayment: true}); err != nil {
			t.Fatalf("order that abandons the payment: %v", err)
		}
		requirePendingPayment(t, st, "")
	})

	t.Run("a dry run reports it and keeps it", func(t *testing.T) {
		a, _, st, _ := newWayletApp(t)
		first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("first order: %v", err)
		}
		_, err = a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", DryRun: true})
		requireErrorCode(t, err, "payment_pending")
		requirePendingPayment(t, st, first.PaymentID)
	})

	t.Run("a paid payment stays remembered while its delivery is pending", func(t *testing.T) {
		a, fake, st, _ := newWayletApp(t)
		first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("first order: %v", err)
		}
		if !fake.ConfirmWaylet(first.PaymentID) {
			t.Fatal("the fake did not know the payment")
		}

		_, err = a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		requireErrorCode(t, err, "pending_order")
		// The history is only a second witness: the payment itself keeps
		// refusing until the delivery is behind us.
		requirePendingPayment(t, st, first.PaymentID)
		if got := fake.Count(pathCart); got != 1 {
			t.Errorf("%s requests = %d, want 1", pathCart, got)
		}
	})

	t.Run("a paid payment of a delivered order lets the next one start", func(t *testing.T) {
		a, fake, st, clock, _ := newWayletAppClock(t)
		first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("first order: %v", err)
		}
		fake.ConfirmWaylet(first.PaymentID)
		fake.SetHistory(nil)               // the paid order was delivered long ago
		clock.Advance(10 * 24 * time.Hour) // after its delivery date, 2026-11-04
		second, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("second order: %v", err)
		}
		requirePendingPayment(t, st, second.PaymentID)
	})
}

func TestWayletPaidPaymentBlocksWithoutTheHistory(t *testing.T) {
	ctx := context.Background()
	otherAgency := pendingEntry("10000009", "2026-11-04")
	otherAgency.Agency.AgencyCode = "9999999999"
	otherLocality := pendingEntry("10000009", "2026-11-04")
	otherLocality.Address.LocalityCode = "99999"

	// The history may lag behind a payment that was just paid, or list the order
	// under an agency or a locality that does not match the address. The paid
	// payment itself must refuse the next order, naming the order and its date.
	histories := map[string][]repsolapi.HistoryEntry{
		"history without the order": nil,
		"other agency":              {otherAgency},
		"other locality":            {otherLocality},
	}
	for name, history := range histories {
		t.Run(name, func(t *testing.T) {
			a, fake, st, _ := newWayletApp(t)
			first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
			if err != nil {
				t.Fatalf("first order: %v", err)
			}
			fake.ConfirmWaylet(first.PaymentID)
			fake.SetHistory(history)

			for _, in := range []PlaceOrderInput{
				{Quantity: 1, Pay: "waylet"},
				{Quantity: 1, Pay: "delivery"},
				{Quantity: 1, Pay: "waylet", DryRun: true},
				{Quantity: 1, Pay: "waylet"}, // and again: nothing was forgotten
			} {
				_, err := a.PlaceOrder(ctx, in)
				typed := requireErrorCode(t, err, "pending_order")
				if typed.Category != apperr.Refused {
					t.Errorf("category = %v, want %v", typed.Category, apperr.Refused)
				}
				for _, want := range []string{"800000000000000003", "2026-11-04"} {
					if !strings.Contains(typed.Message, want) {
						t.Errorf("message %q does not name %q", typed.Message, want)
					}
				}
			}
			if fake.Count(pathCart) != 1 || fake.Count(pathSolicitude) != 0 || fake.Count(pathOrder) != 0 {
				t.Errorf("a refused order reached a write: cart=%d solicitude=%d order=%d",
					fake.Count(pathCart), fake.Count(pathSolicitude), fake.Count(pathOrder))
			}
			requirePendingPayment(t, st, first.PaymentID)
		})
	}
}

func TestWayletLookupFailureIsNotAnUnpaidPayment(t *testing.T) {
	ctx := context.Background()
	failures := map[string]struct {
		arm       func(*App, *fakerepsol.Server)
		retryable bool
	}{
		"forbidden":    {func(_ *App, f *fakerepsol.Server) { f.FailNext(pathByPayment, 403) }, false},
		"server error": {func(_ *App, f *fakerepsol.Server) { f.FailNext(pathByPayment, 503) }, true},
		"dropped": {func(a *App, f *fakerepsol.Server) {
			// A new connection each time: the Go transport would otherwise
			// repeat a read on its own when a reused connection is closed.
			a.api.HTTP.HTTP.Transport = &http.Transport{DisableKeepAlives: true}
			f.DropNext(pathByPayment)
		}, true},
	}
	for name, tc := range failures {
		t.Run(name, func(t *testing.T) {
			a, fake, st, _ := newWayletApp(t)
			a.api.HTTP.Attempts = 1 // one failing answer is the whole lookup
			first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
			if err != nil {
				t.Fatalf("first order: %v", err)
			}
			tc.arm(a, fake)

			_, err = a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery"})
			typed := apperr.From(err)
			if typed.Category != apperr.Refused {
				t.Fatalf("error = %v, want a refusal", err)
			}
			if strings.Contains(typed.Message, "not paid yet") {
				t.Errorf("message %q says the payment is unpaid, but the lookup failed", typed.Message)
			}
			if !strings.Contains(typed.Message, "could not check") {
				t.Errorf("message %q does not say the payment could not be checked", typed.Message)
			}
			if !strings.Contains(typed.Hint, "repsol order payment "+first.PaymentID) {
				t.Errorf("hint %q does not say how to check the payment", typed.Hint)
			}
			if typed.Retryable != tc.retryable {
				t.Errorf("retryable = %v, want %v", typed.Retryable, tc.retryable)
			}
			if fake.Count(pathSolicitude) != 0 || fake.Count(pathOrder) != 0 {
				t.Errorf("the refused order was written")
			}
			requirePendingPayment(t, st, first.PaymentID)
		})
	}
}

func TestWayletStateSurvivesAFailedWaitAndClosesOnConfirmation(t *testing.T) {
	ctx := context.Background()

	t.Run("timeout", func(t *testing.T) {
		a, _, st, _ := newWayletApp(t)
		_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", Wait: time.Minute})
		requireErrorCode(t, err, "payment_timeout")
		requirePendingPayment(t, st, "fake-payment-1")
		_, err = a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery"})
		requireErrorCode(t, err, "payment_pending")
	})

	t.Run("interrupted", func(t *testing.T) {
		a, _, st, _ := newWayletApp(t)
		cctx, cancel := context.WithCancel(ctx)
		defer cancel()
		a.sleep = func(ctx context.Context, d time.Duration) error { cancel(); return ctx.Err() }
		_, err := a.PlaceOrder(cctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", Wait: time.Hour})
		requireErrorCode(t, err, "interrupted")
		requirePendingPayment(t, st, "fake-payment-1")
	})

	t.Run("confirmed wait keeps it while the delivery is pending", func(t *testing.T) {
		a, fake, st, _ := newWayletApp(t)
		fake.SetWayletPayAfter(2)
		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", Wait: time.Hour}); err != nil {
			t.Fatalf("PlaceOrder: %v", err)
		}
		// The paid order is delivered on 2026-11-04. Until then the payment
		// stays remembered, so the next order is refused even if the history
		// has not caught up.
		requirePendingPayment(t, st, "fake-payment-1")
		fake.SetHistory(nil)
		_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery"})
		requireErrorCode(t, err, "pending_order")
	})

	t.Run("confirmed wait of a delivered order closes it", func(t *testing.T) {
		a, fake, st, clock, _ := newWayletAppClock(t)
		fake.SetWayletPayAfter(2)
		// The wait runs after the delivery date, as when a payment is made on
		// the same day the order is due.
		fake.SetToday(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC))
		clock.Advance(30 * 24 * time.Hour)
		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", Wait: time.Hour}); err != nil {
			t.Fatalf("PlaceOrder: %v", err)
		}
		requirePendingPayment(t, st, "")
	})

	t.Run("order payment keeps it while the delivery is pending", func(t *testing.T) {
		a, fake, st, _ := newWayletApp(t)
		res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("PlaceOrder: %v", err)
		}
		if view, err := a.PaymentStatus(ctx, res.PaymentID, 0); err != nil || view.Status != "awaiting_payment" {
			t.Fatalf("PaymentStatus = %+v, %v; want awaiting_payment", view, err)
		}
		requirePendingPayment(t, st, res.PaymentID)

		fake.ConfirmWaylet(res.PaymentID)
		if view, err := a.PaymentStatus(ctx, res.PaymentID, 0); err != nil || view.Status != "confirmed" {
			t.Fatalf("PaymentStatus = %+v, %v; want confirmed", view, err)
		}
		requirePendingPayment(t, st, res.PaymentID)
	})

	t.Run("order payment of a delivered order closes it", func(t *testing.T) {
		a, fake, st, clock, _ := newWayletAppClock(t)
		res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("PlaceOrder: %v", err)
		}
		fake.ConfirmWaylet(res.PaymentID)
		clock.Advance(10 * 24 * time.Hour) // past the delivery date, 2026-11-04
		if view, err := a.PaymentStatus(ctx, res.PaymentID, 0); err != nil || view.Status != "confirmed" {
			t.Fatalf("PaymentStatus = %+v, %v; want confirmed", view, err)
		}
		requirePendingPayment(t, st, "")
	})

	t.Run("order payment of another id leaves it", func(t *testing.T) {
		a, fake, st, _ := newWayletApp(t)
		res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("PlaceOrder: %v", err)
		}
		fake.ConfirmWaylet(res.PaymentID)
		if _, err := a.PaymentStatus(ctx, "some-other-payment", 0); err != nil {
			t.Fatalf("PaymentStatus: %v", err)
		}
		requirePendingPayment(t, st, res.PaymentID)
	})
}

func TestWayletURLFailureAfterSendIsOutcomeUnknown(t *testing.T) {
	ctx := context.Background()
	arm := map[string]func(*fakerepsol.Server){
		"server error": func(f *fakerepsol.Server) { f.FailNext(pathWayletURL, 502) },
		"dropped":      func(f *fakerepsol.Server) { f.DropNext(pathWayletURL) },
		"bad answer": func(f *fakerepsol.Server) {
			f.ReplyNext(pathWayletURL, 200, `{"wayletRedirectionPaymentUrl":"javascript:alert(1)"}`)
		},
	}
	for name, fail := range arm {
		t.Run(name, func(t *testing.T) {
			a, fake, st, _ := newWayletApp(t)
			fail(fake)

			_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
			typed := requireErrorCode(t, err, "payment_outcome_unknown")
			if typed.Category != apperr.OutcomeUnknown || typed.Retryable {
				t.Errorf("category = %v retryable = %v, want outcome_unknown and not retryable", typed.Category, typed.Retryable)
			}
			if strings.Contains(typed.Message+typed.Hint, "no payment was started") {
				t.Errorf("%q / %q say no payment was started, which nobody knows", typed.Message, typed.Hint)
			}
			if !strings.Contains(typed.Message+" "+typed.Hint, "nothing has been charged") ||
				!strings.Contains(typed.Message+" "+typed.Hint, "may have been created") {
				t.Errorf("%q / %q must say that nothing was charged but the payment may exist", typed.Message, typed.Hint)
			}
			if !strings.Contains(typed.Hint, "repsol order payment fake-payment-1") {
				t.Errorf("hint %q does not name the lookup of the payment", typed.Hint)
			}
			if fake.Count(pathCart) != 1 || fake.Count(pathWayletURL) != 1 {
				t.Errorf("cart=%d url=%d, want 1 and 1: nothing is retried", fake.Count(pathCart), fake.Count(pathWayletURL))
			}

			// The payment may exist, so the next order is held back.
			requirePendingPayment(t, st, "fake-payment-1")
			_, err = a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
			requireErrorCode(t, err, "payment_pending")
		})
	}

	t.Run("a refusal is not ambiguous", func(t *testing.T) {
		a, fake, st, _ := newWayletApp(t)
		fake.FailNext(pathWayletURL, 400)
		_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		typed := apperr.From(err)
		if typed.Category == apperr.OutcomeUnknown {
			t.Errorf("a 400 is a definite answer, got %v", typed.Category)
		}
		if !strings.Contains(typed.Hint, "no payment was started") {
			t.Errorf("hint %q should say no payment was started", typed.Hint)
		}
		requirePendingPayment(t, st, "")
	})
}

func TestWayletTransportTimeoutAfterSending(t *testing.T) {
	ctx := context.Background()
	shortTimeout := func(a *App) { a.api.HTTP.HTTP.Timeout = 200 * time.Millisecond }

	t.Run("cart", func(t *testing.T) {
		a, fake, st, _ := newWayletApp(t)
		shortTimeout(a)
		_, release := fake.Hold(pathCart)
		defer release()

		_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		typed := apperr.From(err)
		if typed.Category != apperr.Network || typed.Code != "timeout" {
			t.Fatalf("error = %v (%s/%s), want a network timeout", err, typed.Category, typed.Code)
		}
		if typed.Category == apperr.OutcomeUnknown {
			t.Errorf("a cart is no order: it must not be an unknown order outcome")
		}
		if fake.Count(pathCart) != 1 || fake.Count(pathWayletURL) != 0 {
			t.Errorf("cart=%d url=%d, want 1 and 0: the cart is not retried and no link is asked for",
				fake.Count(pathCart), fake.Count(pathWayletURL))
		}
		// No payment id came back, so there is no payment to track or to pay.
		requirePendingPayment(t, st, "")
	})

	t.Run("url", func(t *testing.T) {
		a, fake, st, _ := newWayletApp(t)
		shortTimeout(a)
		_, release := fake.Hold(pathWayletURL)
		defer release()

		_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		typed := requireErrorCode(t, err, "payment_outcome_unknown")
		if typed.Category != apperr.OutcomeUnknown || typed.Retryable {
			t.Errorf("category = %v retryable = %v, want outcome_unknown and not retryable", typed.Category, typed.Retryable)
		}
		if fake.Count(pathCart) != 1 || fake.Count(pathWayletURL) != 1 {
			t.Errorf("cart=%d url=%d, want 1 and 1", fake.Count(pathCart), fake.Count(pathWayletURL))
		}
		requirePendingPayment(t, st, "fake-payment-1")
	})
}

func TestWayletOnlinePaymentFollowsTheCartCheck(t *testing.T) {
	ctx := context.Background()
	// The web offers an online payment only when the cart check is an order
	// that is not billable, not blocked by the policy and not a calendar
	// delivery without a date, and it keeps an expired inspection to payment on
	// delivery.
	cases := []struct {
		name  string
		check repsolapi.CartCheck
		want  string
	}{
		{"billable", repsolapi.CartCheck{Status: "EE04", Order: true, OrderType: "ORDER", Billable: true}, "billed"},
		{"blocked policy", repsolapi.CartCheck{Status: "EE04", Order: true, OrderType: "ORDER", BlockedPolicy: true}, "policy"},
		{"calendar without a date", repsolapi.CartCheck{Status: "EE04", Order: true, OrderType: "ORDER", IsCalendarDeliveryWithoutDate: true}, "calendar"},
		{"inspection expired", repsolapi.CartCheck{Status: "EE04", Order: true, OrderType: "ORDER", InspectionExpired: true}, "inspection"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, dryRun := range []bool{true, false} {
				a, fake, st, _ := newWayletApp(t)
				fake.SetCheck(tc.check)

				_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", DryRun: dryRun})
				typed := requireErrorCode(t, err, "waylet_unavailable")
				if typed.Category != apperr.Refused || !strings.Contains(typed.Message, tc.want) {
					t.Errorf("dry_run=%v: %v/%q, want a refusal mentioning %q", dryRun, typed.Category, typed.Message, tc.want)
				}
				if !strings.Contains(typed.Hint, "--pay delivery") {
					t.Errorf("hint %q does not point at --pay delivery", typed.Hint)
				}
				requireNoWayletWrites(t, fake)
				requirePendingPayment(t, st, "")
			}

			// The same cart check does not stop a delivery order.
			a, fake, _, _ := newWayletApp(t)
			fake.SetCheck(tc.check)
			res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery"})
			if err != nil {
				t.Fatalf("delivery order: %v", err)
			}
			if res.Payment != "on_delivery" || fake.Count(pathOrder) != 1 {
				t.Errorf("result = %+v, /order = %d, want a delivery order placed", res, fake.Count(pathOrder))
			}
		})
	}
}

func TestWayletWaitToleratesNotYetAnswers(t *testing.T) {
	ctx := context.Background()
	// The web asks again on any failure of the lookup; only an authentication
	// failure is a real one.
	notYet := map[string]func(*fakerepsol.Server){
		"bad request":    func(f *fakerepsol.Server) { f.FailNext(pathByPayment, 400) },
		"unprocessable":  func(f *fakerepsol.Server) { f.FailNext(pathByPayment, 422) },
		"error envelope": func(f *fakerepsol.Server) { f.ReplyNext(pathByPayment, 200, `{"error":true,"message":"not yet"}`) },
		"server error":   func(f *fakerepsol.Server) { f.FailNext(pathByPayment, 503) },
	}
	for name, arm := range notYet {
		t.Run(name, func(t *testing.T) {
			a, fake, _, pauses := newWayletApp(t)
			fake.SetWayletPayAfter(2)
			arm(fake)

			res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", Wait: 15 * time.Minute})
			if err != nil {
				t.Fatalf("PlaceOrder: %v", err)
			}
			if res.PaymentStatus != "confirmed" || len(*pauses) < 1 {
				t.Errorf("status = %q after %d pauses, want confirmed after asking again", res.PaymentStatus, len(*pauses))
			}
		})
	}

	t.Run("order payment without wait says awaiting", func(t *testing.T) {
		for name, arm := range notYet {
			a, fake, _, _ := newWayletApp(t)
			res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
			if err != nil {
				t.Fatalf("%s: PlaceOrder: %v", name, err)
			}
			arm(fake)
			view, err := a.PaymentStatus(ctx, res.PaymentID, 0)
			if err != nil || view.Status != "awaiting_payment" {
				t.Errorf("%s: view = %+v, err = %v; want awaiting_payment", name, view, err)
			}
		}
	})

	for status, category := range map[int]apperr.Category{401: apperr.Auth, 403: apperr.Blocked} {
		t.Run(fmt.Sprintf("%d ends the wait", status), func(t *testing.T) {
			a, fake, _, _ := newWayletApp(t)
			fake.SetWayletPayAfter(5)
			fake.FailNext(pathByPayment, status)

			_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", Wait: 15 * time.Minute})
			typed := apperr.From(err)
			if typed.Category != category {
				t.Fatalf("category = %v (%v), want %v", typed.Category, err, category)
			}
			if got := fake.Count(pathByPayment); got != 1 {
				t.Errorf("%s requests = %d, want 1", pathByPayment, got)
			}
		})
	}
}

func TestWayletWaitRespectsTheTotalDeadline(t *testing.T) {
	ctx := context.Background()
	const wait = 300 * time.Millisecond

	// A lookup that never answers must not hold the wait past its deadline: the
	// transport timeout and its retries are longer than the whole wait.
	for name, run := range map[string]func(a *App) error{
		"order create --wait": func(a *App) error {
			_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", Wait: wait})
			return err
		},
		"order payment --wait": func(a *App) error {
			_, err := a.PaymentStatus(ctx, "fake-payment-1", wait)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			a, fake, _, _ := newWayletApp(t)
			a.api.HTTP.HTTP.Timeout = 3 * time.Second
			_, release := fake.Hold(pathByPayment)
			defer release()

			start := time.Now()
			err := run(a)
			elapsed := time.Since(start)

			requireErrorCode(t, err, "payment_timeout")
			if elapsed > 2*time.Second {
				t.Errorf("the wait took %v, want about %v", elapsed, wait)
			}
		})
	}
}

// noIDConfirmation is the lookup answer of a paid payment that has an agency and
// an address but no external id. The web accepts it (externalId || ""), so the
// CLI must read it as a confirmation too.
const noIDConfirmation = `{"deliveryDate":"2026-11-04","address":{"street":"Calle Falsa"},` +
	`"agency":{"name":"Agencia Test","phone":"900 000 000"}}`

func TestWayletConfirmationWithoutExternalID(t *testing.T) {
	ctx := context.Background()

	t.Run("order create --wait", func(t *testing.T) {
		a, fake, _, _ := newWayletApp(t)
		fake.ReplyNext(pathByPayment, 200, noIDConfirmation)

		res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", Wait: time.Minute})
		if err != nil {
			t.Fatalf("PlaceOrder: %v", err)
		}
		if res.PaymentStatus != "confirmed" || res.ID != "" {
			t.Errorf("status = %q id = %q, want a confirmed payment without an id", res.PaymentStatus, res.ID)
		}
		if res.Agency.Name != "Agencia Test" {
			t.Errorf("agency = %+v, want the agency of the answer", res.Agency)
		}
	})

	t.Run("order payment", func(t *testing.T) {
		a, fake, _, _ := newWayletApp(t)
		res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("PlaceOrder: %v", err)
		}
		fake.ReplyNext(pathByPayment, 200, noIDConfirmation)

		view, err := a.PaymentStatus(ctx, res.PaymentID, 0)
		if err != nil {
			t.Fatalf("PaymentStatus: %v", err)
		}
		if view.Status != "confirmed" || view.ID != "" || view.DeliveryDate != "2026-11-04" {
			t.Errorf("view = %+v, want a confirmed payment without an id", view)
		}
	})
}

// refuseDialsOnce wraps the transport of the app's client so that the request to
// path finds its connection refused: the request never leaves the machine.
func refuseDialsOnce(a *App, path string) {
	var refuse atomic.Bool
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.DisableKeepAlives = true
	dial := base.DialContext
	base.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if refuse.Load() {
			return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ECONNREFUSED}
		}
		return dial(ctx, network, addr)
	}
	a.api.HTTP.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		refuse.Store(r.URL.Path == path)
		return base.RoundTrip(r)
	})
}

// roundTripFunc is an http.RoundTripper made of a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip calls f.
func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWayletURLRequestThatNeverLeftIsNotRemembered(t *testing.T) {
	ctx := context.Background()
	a, fake, st, _ := newWayletApp(t)
	refuseDialsOnce(a, pathWayletURL)

	_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
	typed := apperr.From(err)
	if typed.Category == apperr.OutcomeUnknown || typed.Code == "payment_outcome_unknown" {
		t.Fatalf("error = %v (%s): a refused connection proves the request was never sent", err, typed.Code)
	}
	if typed.Category != apperr.Network {
		t.Errorf("category = %v, want the network failure it is", typed.Category)
	}
	if !strings.Contains(typed.Hint, "no payment") {
		t.Errorf("hint %q should say no payment link was handed out", typed.Hint)
	}
	if fake.Count(pathCart) != 1 {
		t.Errorf("%s requests = %d, want 1", pathCart, fake.Count(pathCart))
	}
	// Nothing was asked for the link, so there is nothing to wait for or to pay.
	requirePendingPayment(t, st, "")

	// The next order is not held back by it.
	fake.SetHistory(nil)
	a.api.HTTP.HTTP.Transport = nil
	if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"}); err != nil {
		t.Fatalf("the next order: %v", err)
	}
}

func TestWayletInterruptedLinkRequestDoesNotPromiseALink(t *testing.T) {
	ctx := context.Background()
	a, fake, st, _ := newWayletApp(t)
	entered, release := fake.Hold(pathWayletURL)
	defer release()

	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-entered
		cancel()
	}()

	_, err := a.PlaceOrder(cctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
	typed := apperr.From(err)
	if typed.Category != apperr.Interrupted {
		t.Fatalf("error = %v, want an interruption", err)
	}
	// No link was handed out, so the message must not tell the owner that it
	// may still be paid.
	text := strings.ToLower(typed.Message + " " + typed.Hint)
	if strings.Contains(text, "link may still be paid") || strings.Contains(text, "keep waiting") {
		t.Errorf("message %q / hint %q promise a link that was never handed out", typed.Message, typed.Hint)
	}
	if !strings.Contains(text, "no link") {
		t.Errorf("message %q / hint %q should say no link was handed out", typed.Message, typed.Hint)
	}
	if !strings.Contains(typed.Hint, "fake-payment-1") {
		t.Errorf("hint %q should name the payment that may exist", typed.Hint)
	}
	// The request was sent, so the payment may exist: it stays remembered.
	requirePendingPayment(t, st, "fake-payment-1")
}

func TestWayletWaitTimeoutKeepsTheLastLookupFailure(t *testing.T) {
	a, fake, _, _ := newWayletApp(t)
	// The first lookup fails on the server and is asked again; the second one
	// never answers and runs into the deadline of the wait. The timeout reports
	// the real failure, not the deadline of the lookup that ended it.
	fake.FailNext(pathByPayment, 503)
	pause := a.sleep
	var release func()
	a.sleep = func(ctx context.Context, d time.Duration) error {
		_, release = fake.Hold(pathByPayment) // the lookup after the pause hangs
		return pause(ctx, d)
	}
	defer func() {
		if release != nil {
			release()
		}
	}()

	const wait = PaymentPollInterval + 300*time.Millisecond
	_, err := a.PlaceOrder(context.Background(), PlaceOrderInput{Quantity: 1, Pay: "waylet", Wait: wait})
	typed := requireErrorCode(t, err, "payment_timeout")
	if !strings.Contains(typed.Error(), "503") {
		t.Errorf("error %q does not carry the 503 of the last failed lookup", typed.Error())
	}
}

// payment state helpers for the tests that damage or age the remembered payment.
func pendingFile(st config.Store) string {
	return filepath.Join(filepath.Dir(st.Paths.Lock), "payment-"+defaultTestProfile+".toml")
}

func TestForceDoesNotSkipTheRememberedPayment(t *testing.T) {
	ctx := context.Background()

	// --force is the owner's word for a delivery that is pending in the
	// history. It says nothing about a payment link that may have been paid
	// since: the payment is looked up all the same.
	t.Run("unpaid", func(t *testing.T) {
		for _, pay := range []string{"waylet", "delivery"} {
			a, fake, st, _ := newWayletApp(t)
			first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
			if err != nil {
				t.Fatalf("first order: %v", err)
			}
			_, err = a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: pay, Force: true})
			requireErrorCode(t, err, "payment_pending")
			if fake.Count(pathCart) != 1 || fake.Count(pathSolicitude) != 0 || fake.Count(pathOrder) != 0 {
				t.Errorf("pay=%s: the forced order reached a write", pay)
			}
			if got := fake.Count(pathByPayment); got != 1 {
				t.Errorf("pay=%s: %s requests = %d, want 1: --force still looks the payment up", pay, pathByPayment, got)
			}
			requirePendingPayment(t, st, first.PaymentID)
		}
	})

	t.Run("paid", func(t *testing.T) {
		for _, pay := range []string{"waylet", "delivery"} {
			a, fake, st, _ := newWayletApp(t)
			first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
			if err != nil {
				t.Fatalf("first order: %v", err)
			}
			fake.ConfirmWaylet(first.PaymentID)
			fake.SetHistory(nil) // the history has not caught up

			_, err = a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: pay, Force: true})
			requireErrorCode(t, err, "pending_order")
			if fake.Count(pathCart) != 1 || fake.Count(pathSolicitude) != 0 || fake.Count(pathOrder) != 0 {
				t.Errorf("pay=%s: the forced order reached a write", pay)
			}
			requirePendingPayment(t, st, first.PaymentID)
		}
	})

	t.Run("lookup failed", func(t *testing.T) {
		a, fake, st, _ := newWayletApp(t)
		a.api.HTTP.Attempts = 1
		first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("first order: %v", err)
		}
		fake.FailNext(pathByPayment, 503)
		_, err = a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery", Force: true})
		requireErrorCode(t, err, "payment_check_failed")
		if fake.Count(pathOrder) != 0 || fake.Count(pathSolicitude) != 0 {
			t.Errorf("the forced order was written although the payment could not be checked")
		}
		requirePendingPayment(t, st, first.PaymentID)
	})

	t.Run("force still skips the history guard", func(t *testing.T) {
		a, fake, _, _ := newWayletApp(t)
		fake.SetHistory([]repsolapi.HistoryEntry{pendingEntry("10000009", "2026-11-03")})
		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery", Force: true}); err != nil {
			t.Fatalf("forced order over a pending delivery: %v", err)
		}
	})
}

func TestWayletRememberedPaymentExpires(t *testing.T) {
	ctx := context.Background()

	t.Run("still held back within a day", func(t *testing.T) {
		a, _, _, clock, _ := newWayletAppClock(t)
		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"}); err != nil {
			t.Fatalf("first order: %v", err)
		}
		clock.Advance(23 * time.Hour)
		_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery"})
		requireErrorCode(t, err, "payment_pending")
	})

	t.Run("unpaid after a day is forgotten with a notice", func(t *testing.T) {
		a, fake, st, clock, _ := newWayletAppClock(t)
		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"}); err != nil {
			t.Fatalf("first order: %v", err)
		}
		clock.Advance(25 * time.Hour)

		res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery"})
		if err != nil {
			t.Fatalf("order after the payment expired: %v", err)
		}
		if len(res.Notices) != 1 || !strings.Contains(res.Notices[0], "forgotten") {
			t.Errorf("notices = %q, want one saying the unpaid payment was forgotten", res.Notices)
		}
		if got := fake.Count(pathByPayment); got != 1 {
			t.Errorf("%s requests = %d, want 1: one last lookup before forgetting", pathByPayment, got)
		}
		requirePendingPayment(t, st, "")
	})

	t.Run("a dry run does not forget it", func(t *testing.T) {
		a, _, st, clock, _ := newWayletAppClock(t)
		first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("first order: %v", err)
		}
		clock.Advance(25 * time.Hour)
		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery", DryRun: true}); err != nil {
			t.Fatalf("dry run after the payment expired: %v", err)
		}
		requirePendingPayment(t, st, first.PaymentID)
	})

	t.Run("paid after a day is not forgotten", func(t *testing.T) {
		a, fake, st, clock, _ := newWayletAppClock(t)
		first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("first order: %v", err)
		}
		fake.ConfirmWaylet(first.PaymentID)
		fake.SetHistory(nil)
		clock.Advance(25 * time.Hour) // 2026-11-02: the delivery of 2026-11-04 is still due

		_, err = a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery"})
		requireErrorCode(t, err, "pending_order")
		requirePendingPayment(t, st, first.PaymentID)
	})

	t.Run("a lookup that fails after a day keeps it", func(t *testing.T) {
		a, fake, st, clock, _ := newWayletAppClock(t)
		a.api.HTTP.Attempts = 1
		first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("first order: %v", err)
		}
		clock.Advance(25 * time.Hour)
		fake.FailNext(pathByPayment, 503)

		_, err = a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery"})
		requireErrorCode(t, err, "payment_check_failed")
		requirePendingPayment(t, st, first.PaymentID)
	})
}

func TestWayletUnreadablePaymentStateIsReported(t *testing.T) {
	ctx := context.Background()
	a, fake, st, _ := newWayletApp(t)
	if err := os.WriteFile(pendingFile(st), []byte("this is [not toml"), 0o600); err != nil {
		t.Fatalf("write the damaged state: %v", err)
	}

	_, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery"})
	typed := requireErrorCode(t, err, "payment_state_invalid")
	if typed.Category != apperr.Refused {
		t.Errorf("category = %v, want %v", typed.Category, apperr.Refused)
	}
	if !strings.Contains(typed.Hint, "--abandon") {
		t.Errorf("hint %q does not say how to clear the state", typed.Hint)
	}
	if fake.Count(pathSolicitude) != 0 || fake.Count(pathOrder) != 0 {
		t.Errorf("an order was written over a state that could not be read")
	}
}

func TestWayletAbandonPaymentInOrderCreate(t *testing.T) {
	ctx := context.Background()

	t.Run("an unpaid payment is forgotten and the order goes on", func(t *testing.T) {
		for _, pay := range []string{"waylet", "delivery"} {
			a, fake, st, _ := newWayletApp(t)
			first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
			if err != nil {
				t.Fatalf("first order: %v", err)
			}
			res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: pay, AbandonPayment: true})
			if err != nil {
				t.Fatalf("pay=%s: order that abandons the payment: %v", pay, err)
			}
			if len(res.Notices) != 1 || !strings.Contains(res.Notices[0], "forgotten") {
				t.Errorf("pay=%s: notices = %q, want one saying the payment was forgotten", pay, res.Notices)
			}
			if pay == "waylet" {
				requirePendingPayment(t, st, res.PaymentID) // the new one is tracked
				if res.PaymentID == first.PaymentID {
					t.Errorf("the abandoned payment was reused")
				}
			} else {
				requirePendingPayment(t, st, "")
			}
			if got := fake.Count(pathByPayment); got != 1 {
				t.Errorf("pay=%s: %s requests = %d, want 1: the payment is checked before it is forgotten", pay, pathByPayment, got)
			}
		}
	})

	t.Run("a paid payment is never forgotten this way", func(t *testing.T) {
		a, fake, st, _ := newWayletApp(t)
		first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("first order: %v", err)
		}
		fake.ConfirmWaylet(first.PaymentID)
		fake.SetHistory(nil)
		_, err = a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery", AbandonPayment: true, Force: true})
		requireErrorCode(t, err, "pending_order")
		requirePendingPayment(t, st, first.PaymentID)
		if fake.Count(pathOrder) != 0 || fake.Count(pathSolicitude) != 0 {
			t.Errorf("an order was written over a paid delivery")
		}
	})

	t.Run("a payment that could not be checked is forgotten on explicit request", func(t *testing.T) {
		a, fake, st, _ := newWayletApp(t)
		a.api.HTTP.Attempts = 1
		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"}); err != nil {
			t.Fatalf("first order: %v", err)
		}
		fake.FailNext(pathByPayment, 503)
		res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery", AbandonPayment: true})
		if err != nil {
			t.Fatalf("order that abandons the payment: %v", err)
		}
		if len(res.Notices) != 1 || !strings.Contains(res.Notices[0], "could not be checked") {
			t.Errorf("notices = %q, want one saying the payment could not be checked", res.Notices)
		}
		requirePendingPayment(t, st, "")
	})

	t.Run("a dry run forgets nothing", func(t *testing.T) {
		a, _, st, _ := newWayletApp(t)
		first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("first order: %v", err)
		}
		res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet", DryRun: true, AbandonPayment: true})
		if err != nil {
			t.Fatalf("dry run: %v", err)
		}
		if len(res.Notices) != 1 || !strings.Contains(res.Notices[0], "would be forgotten") {
			t.Errorf("notices = %q, want one saying the payment would be forgotten", res.Notices)
		}
		requirePendingPayment(t, st, first.PaymentID)
	})
}

func TestAbandonPaymentCommand(t *testing.T) {
	ctx := context.Background()

	t.Run("an unpaid payment is forgotten and ordering resumes", func(t *testing.T) {
		a, _, st, _ := newWayletApp(t)
		first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("first order: %v", err)
		}
		for _, id := range []string{first.PaymentID, ""} {
			if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery"}); err == nil {
				t.Fatal("the unpaid payment did not block the order")
			}
			view, err := a.AbandonPayment(ctx, id)
			if err != nil {
				t.Fatalf("AbandonPayment(%q): %v", id, err)
			}
			if view.Status != "abandoned" || view.PaymentID != first.PaymentID || view.Note != "" {
				t.Errorf("view = %+v, want the payment abandoned without a note", view)
			}
			requirePendingPayment(t, st, "")
			if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery", DryRun: true}); err != nil {
				t.Errorf("ordering after the abandon: %v", err)
			}
			// the next round needs a payment to abandon again
			first, err = a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
			if err != nil {
				t.Fatalf("new payment: %v", err)
			}
		}
	})

	t.Run("nothing remembered", func(t *testing.T) {
		a, _, _, _ := newWayletApp(t)
		_, err := a.AbandonPayment(ctx, "")
		typed := requireErrorCode(t, err, "no_pending_payment")
		if typed.Category != apperr.NotFound {
			t.Errorf("category = %v, want %v", typed.Category, apperr.NotFound)
		}
	})

	t.Run("another payment id", func(t *testing.T) {
		a, _, st, _ := newWayletApp(t)
		first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("first order: %v", err)
		}
		_, err = a.AbandonPayment(ctx, "some-other-payment")
		requireErrorCode(t, err, "payment_mismatch")
		requirePendingPayment(t, st, first.PaymentID)
	})

	t.Run("a paid payment is forgotten with the order it produced", func(t *testing.T) {
		a, fake, st, _ := newWayletApp(t)
		first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("first order: %v", err)
		}
		fake.ConfirmWaylet(first.PaymentID)
		view, err := a.AbandonPayment(ctx, first.PaymentID)
		if err != nil {
			t.Fatalf("AbandonPayment: %v", err)
		}
		if view.Status != "abandoned" || view.ID != "800000000000000003" || view.DeliveryDate != "2026-11-04" {
			t.Errorf("view = %+v, want the abandoned payment with the paid order", view)
		}
		if !strings.Contains(view.Note, "was paid") {
			t.Errorf("note %q does not say that the payment was paid", view.Note)
		}
		requirePendingPayment(t, st, "")
	})

	t.Run("a lookup that fails still abandons, and says so", func(t *testing.T) {
		a, fake, st, _ := newWayletApp(t)
		a.api.HTTP.Attempts = 1
		first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("first order: %v", err)
		}
		fake.FailNext(pathByPayment, 503)
		view, err := a.AbandonPayment(ctx, first.PaymentID)
		if err != nil {
			t.Fatalf("AbandonPayment: %v", err)
		}
		if view.Status != "abandoned" || !strings.Contains(view.Note, "could not be checked") {
			t.Errorf("view = %+v, want an abandoned payment that says it could not be checked", view)
		}
		requirePendingPayment(t, st, "")
	})

	t.Run("an expired session is reported", func(t *testing.T) {
		a, fake, st, _ := newWayletApp(t)
		first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("first order: %v", err)
		}
		fake.FailNext(pathByPayment, 401)
		_, err = a.AbandonPayment(ctx, first.PaymentID)
		if apperr.From(err).Category != apperr.Auth {
			t.Fatalf("error = %v, want the authentication failure", err)
		}
		requirePendingPayment(t, st, first.PaymentID)
	})

	t.Run("it does not run beside an order", func(t *testing.T) {
		a, fake, st, _ := newWayletApp(t)
		first, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "waylet"})
		if err != nil {
			t.Fatalf("first order: %v", err)
		}
		release, err := a.lockOrder()
		if err != nil {
			t.Fatalf("lock: %v", err)
		}
		defer release()
		_, err = a.AbandonPayment(ctx, first.PaymentID)
		requireErrorCode(t, err, "order_in_progress")
		requirePendingPayment(t, st, first.PaymentID)
		_ = fake
	})

	t.Run("a damaged state file is removed", func(t *testing.T) {
		a, _, st, _ := newWayletApp(t)
		if err := os.WriteFile(pendingFile(st), []byte("this is [not toml"), 0o600); err != nil {
			t.Fatalf("write the damaged state: %v", err)
		}
		view, err := a.AbandonPayment(ctx, "")
		if err != nil {
			t.Fatalf("AbandonPayment: %v", err)
		}
		if view.Status != "abandoned" || !strings.Contains(view.Note, "could not be read") {
			t.Errorf("view = %+v, want an abandoned state that says it could not be read", view)
		}
		if _, err := os.Stat(pendingFile(st)); !os.IsNotExist(err) {
			t.Errorf("the damaged state file is still there: %v", err)
		}
		if _, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery"}); err != nil {
			t.Errorf("ordering after removing the damaged state: %v", err)
		}
	})
}

func TestWayletUnreadablePaymentStateCanBeAbandonedInOrderCreate(t *testing.T) {
	ctx := context.Background()
	a, _, st, _ := newWayletApp(t)
	if err := os.WriteFile(pendingFile(st), []byte("payment_id = ["), 0o600); err != nil {
		t.Fatalf("write the damaged state: %v", err)
	}

	res, err := a.PlaceOrder(ctx, PlaceOrderInput{Quantity: 1, Pay: "delivery", AbandonPayment: true})
	if err != nil {
		t.Fatalf("order that abandons the damaged state: %v", err)
	}
	if len(res.Notices) != 1 || !strings.Contains(res.Notices[0], "could not be read") {
		t.Errorf("notices = %q, want one saying the state could not be read", res.Notices)
	}
	if _, err := os.Stat(pendingFile(st)); !os.IsNotExist(err) {
		t.Errorf("the damaged state file is still there: %v", err)
	}
}
