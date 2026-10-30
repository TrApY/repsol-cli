package app

import (
	"bytes"
	"strings"
	"testing"
)

// TestPrettyOrderResult checks the human-readable order rendering: the id, the
// amount and the payment method are readable words, not JSON.
func TestPrettyOrderResult(t *testing.T) {
	res := OrderResult{
		ID:           "800000000000000001",
		OrderType:    "SOLICITUDE",
		Product:      "butano",
		MaterialID:   "153",
		Quantity:     1,
		UnitPrice:    18.89,
		Total:        18.89,
		DeliveryDate: "2026-11-04",
		Address:      "Calle Falsa 1, 28001 Pueblo, Madrid",
		Agency:       AgencyView{Name: "Agencia Test", Code: "0000000001"},
		Payment:      "on_delivery",
	}

	var buf bytes.Buffer
	if err := res.PrettyPrint(&buf); err != nil {
		t.Fatalf("PrettyPrint: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"800000000000000001", "18.89", "on delivery"} {
		if !strings.Contains(out, want) {
			t.Errorf("pretty output %q does not contain %q", out, want)
		}
	}
}
