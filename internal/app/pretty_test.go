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
		Note:         solicitudeNote,
	}

	var buf bytes.Buffer
	if err := res.PrettyPrint(&buf); err != nil {
		t.Fatalf("PrettyPrint: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"800000000000000001", "18.89", "on delivery", "note: " + solicitudeNote} {
		if !strings.Contains(out, want) {
			t.Errorf("pretty output %q does not contain %q", out, want)
		}
	}
}

// TestPrettyWayletPayment checks that a payment awaiting the owner shows the
// link to pay, and that a confirmed one shows the order instead.
func TestPrettyWayletPayment(t *testing.T) {
	awaiting := OrderResult{
		OrderType:     "ORDER",
		Payment:       "waylet",
		PaymentStatus: "awaiting_payment",
		PaymentID:     "fake-payment-1",
		PaymentURL:    "https://waylet.example.test/pay?paymentId=fake-payment-1",
	}
	var buf bytes.Buffer
	if err := awaiting.PrettyPrint(&buf); err != nil {
		t.Fatalf("PrettyPrint: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"payment started", "payment status: awaiting payment", "payment id: fake-payment-1", "pay here: https://waylet.example.test/pay"} {
		if !strings.Contains(out, want) {
			t.Errorf("awaiting output %q does not contain %q", out, want)
		}
	}

	confirmed := awaiting
	confirmed.ID = "800000000000000003"
	confirmed.PaymentStatus = "confirmed"
	buf.Reset()
	if err := confirmed.PrettyPrint(&buf); err != nil {
		t.Fatalf("PrettyPrint: %v", err)
	}
	out = buf.String()
	if !strings.Contains(out, "order 800000000000000003") || strings.Contains(out, "pay here") {
		t.Errorf("confirmed output %q: want the order and no payment link", out)
	}

	buf.Reset()
	view := PaymentView{PaymentID: "p", Status: "confirmed", ID: "800000000000000003", DeliveryDate: "2026-11-04", Agency: &AgencyView{Name: "Agencia Test"}}
	if err := view.PrettyPrint(&buf); err != nil {
		t.Fatalf("PaymentView.PrettyPrint: %v", err)
	}
	for _, want := range []string{"payment status: confirmed", "order: 800000000000000003", "delivery: 2026-11-04", "agency: Agencia Test"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("payment view output %q does not contain %q", buf.String(), want)
		}
	}
}

// TestPrettyConfirmedPaymentWithoutOrderID checks that a confirmed payment whose
// answer carries no external id never prints an empty "order " line: the web
// accepts such an answer, so the CLI has to report it too.
func TestPrettyConfirmedPaymentWithoutOrderID(t *testing.T) {
	var buf bytes.Buffer
	res := OrderResult{
		OrderType:     "ORDER",
		Payment:       "waylet",
		PaymentStatus: "confirmed",
		PaymentID:     "fake-payment-1",
		DeliveryDate:  "2026-11-04",
	}
	if err := res.PrettyPrint(&buf); err != nil {
		t.Fatalf("PrettyPrint: %v", err)
	}
	first, _, _ := strings.Cut(buf.String(), "\n")
	if strings.TrimSpace(first) == "order" || strings.HasPrefix(first, "order \n") || strings.HasSuffix(first, " ") {
		t.Errorf("first line %q is an empty order line", first)
	}
	if !strings.Contains(first, "confirmed") {
		t.Errorf("first line %q does not say the payment was confirmed", first)
	}

	buf.Reset()
	view := PaymentView{PaymentID: "p", Status: "confirmed", DeliveryDate: "2026-11-04"}
	if err := view.PrettyPrint(&buf); err != nil {
		t.Fatalf("PaymentView.PrettyPrint: %v", err)
	}
	if strings.Contains(buf.String(), "order:") {
		t.Errorf("payment view output %q prints an empty order", buf.String())
	}
}
