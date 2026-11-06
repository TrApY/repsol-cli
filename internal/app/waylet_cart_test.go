package app

import (
	"context"
	"encoding/json"
	"testing"
)

// cartBody returns the JSON object of the first POST /cart the fake saw.
func cartBody(t *testing.T, fakeBody func() []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(fakeBody(), &body); err != nil {
		t.Fatalf("cart body: %v", err)
	}
	return body
}

// The web strips autocompleteContent from the address of a Repsol-street
// result before it goes into the order, and keeps it for a Google result (that
// one goes through the address mapping with it). POST /cart carries it the
// same way, and always carries bill, discount and tip; policy and exchange are
// left out because they belong to the online policy flow only.
func TestWayletCartBodyMatchesTheWeb(t *testing.T) {
	a, fake, _, _ := newWayletApp(t)
	if _, err := a.PlaceOrder(context.Background(), PlaceOrderInput{Quantity: 1, Pay: "waylet"}); err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	body := cartBody(t, func() []byte { return requireFirstRequest(t, fake, pathCart).Body })
	addr, _ := body["address"].(map[string]any)
	if _, has := addr["autocompleteContent"]; has {
		t.Errorf("address of a Repsol result carries autocompleteContent %v, the web strips it", addr["autocompleteContent"])
	}
	for _, key := range []string{"bill", "discount", "tip", "scheduleMode", "agencyCode", "userGigyaUid", "materialId", "quantity", "unitPrice", "deliveryDateId", "deliveryDate"} {
		if _, has := body[key]; !has {
			t.Errorf("cart body lacks %q", key)
		}
	}
	for _, key := range []string{"policy", "exchange"} {
		if _, has := body[key]; has {
			t.Errorf("cart body carries %q without an online policy", key)
		}
	}
	if got, _ := addr["isPeninsula"].(bool); got {
		t.Errorf("address isPeninsula = true: the web helper answers false for every province")
	}
}

func TestWayletCartBodyKeepsAutocompleteContentOfAGoogleAddress(t *testing.T) {
	a, fake, _, _ := newWayletApp(t)
	if _, err := a.SetAddress(context.Background(), SetAddressInput{Text: "Calle Ambigua 5, Pueblo", Pick: 1}); err != nil {
		t.Fatalf("SetAddress: %v", err)
	}
	if _, err := a.PlaceOrder(context.Background(), PlaceOrderInput{Quantity: 1, Pay: "waylet"}); err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	body := cartBody(t, func() []byte { return requireFirstRequest(t, fake, pathCart).Body })
	addr, _ := body["address"].(map[string]any)
	if got, _ := addr["autocompleteContent"].(string); got == "" {
		t.Errorf("address of a Google result lacks autocompleteContent: %v", addr)
	}
}
