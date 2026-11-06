package app

import (
	"context"
	"testing"
)

// TestWayletStateKeepsBothIds: the state names the payment by the id that works
// and keeps the id the cart answered beside it, so that either finds it.
func TestWayletStateKeepsBothIds(t *testing.T) {
	a, fake, st, _ := newWayletApp(t)

	res, err := a.PlaceOrder(context.Background(), PlaceOrderInput{Quantity: 1, Pay: "waylet"})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	cartID := fake.PaymentIDs()[0]
	got, found, err := st.LoadPendingPayment(defaultTestProfile)
	if err != nil || !found {
		t.Fatalf("LoadPendingPayment = found %v, err %v", found, err)
	}
	if got.PaymentID != res.PaymentID || got.CartID != cartID || !got.Is(res.PaymentID) || !got.Is(cartID) {
		t.Errorf("state = %+v, want payment id %q and cart id %q", got, res.PaymentID, cartID)
	}
}
