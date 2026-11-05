package app

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// PrettyPrint writes the order result as human-readable text. It uses no ANSI
// colours, so NO_COLOR holds without any code.
func (r OrderResult) PrettyPrint(w io.Writer) error {
	var b bytes.Buffer
	switch {
	case r.DryRun:
		b.WriteString("dry run: no order was placed\n")
	case r.PaymentStatus == paymentAwaiting:
		b.WriteString("payment started: the order is placed when the payment is confirmed\n")
	default:
		fmt.Fprintf(&b, "order %s\n", r.ID)
	}
	fmt.Fprintf(&b, "order type: %s\n", r.OrderType)
	if r.Product != "" {
		fmt.Fprintf(&b, "product: %s (material %s)\n", r.Product, r.MaterialID)
	} else {
		fmt.Fprintf(&b, "product: material %s\n", r.MaterialID)
	}
	fmt.Fprintf(&b, "quantity: %d\n", r.Quantity)
	fmt.Fprintf(&b, "unit price: %s EUR\n", money(r.UnitPrice))
	fmt.Fprintf(&b, "total: %s EUR\n", money(r.Total))
	fmt.Fprintf(&b, "delivery: %s\n", r.DeliveryDate)
	fmt.Fprintf(&b, "address: %s\n", r.Address)
	switch {
	case r.Agency.Name != "" && r.Agency.Code != "":
		fmt.Fprintf(&b, "agency: %s (%s)\n", r.Agency.Name, r.Agency.Code)
	case r.Agency.Name != "":
		fmt.Fprintf(&b, "agency: %s\n", r.Agency.Name)
	case r.Agency.Code != "":
		fmt.Fprintf(&b, "agency: %s\n", r.Agency.Code)
	}
	if r.Agency.Phone != "" {
		fmt.Fprintf(&b, "agency phone: %s\n", r.Agency.Phone)
	}
	fmt.Fprintf(&b, "payment: %s\n", strings.ReplaceAll(r.Payment, "_", " "))
	if r.PaymentStatus != "" {
		fmt.Fprintf(&b, "payment status: %s\n", strings.ReplaceAll(r.PaymentStatus, "_", " "))
	}
	if r.PaymentID != "" {
		fmt.Fprintf(&b, "payment id: %s\n", r.PaymentID)
	}
	if r.PaymentURL != "" && r.PaymentStatus == paymentAwaiting {
		fmt.Fprintf(&b, "pay here: %s\n", r.PaymentURL)
	}
	if r.Note != "" {
		fmt.Fprintf(&b, "note: %s\n", r.Note)
	}
	_, err := w.Write(b.Bytes())
	return err
}

// PrettyPrint writes the payment state as human-readable text. It uses no ANSI
// colours, so NO_COLOR holds without any code.
func (v PaymentView) PrettyPrint(w io.Writer) error {
	var b bytes.Buffer
	fmt.Fprintf(&b, "payment id: %s\n", v.PaymentID)
	fmt.Fprintf(&b, "payment status: %s\n", strings.ReplaceAll(v.Status, "_", " "))
	if v.ID != "" {
		fmt.Fprintf(&b, "order: %s\n", v.ID)
	}
	if v.DeliveryDate != "" {
		fmt.Fprintf(&b, "delivery: %s\n", v.DeliveryDate)
	}
	if v.Agency != nil {
		if v.Agency.Name != "" {
			fmt.Fprintf(&b, "agency: %s\n", v.Agency.Name)
		}
		if v.Agency.Phone != "" {
			fmt.Fprintf(&b, "agency phone: %s\n", v.Agency.Phone)
		}
	}
	_, err := w.Write(b.Bytes())
	return err
}

// PrettyPrint writes the stored address as human-readable text. It uses no ANSI
// colours, so NO_COLOR holds without any code.
func (v AddressView) PrettyPrint(w io.Writer) error {
	var b bytes.Buffer
	fmt.Fprintf(&b, "profile: %s\n", v.Profile)
	fmt.Fprintf(&b, "address: %s\n", v.Summary)
	if v.PostalCode != "" {
		fmt.Fprintf(&b, "postal code: %s\n", v.PostalCode)
	}
	if v.Locality != "" {
		fmt.Fprintf(&b, "locality: %s\n", v.Locality)
	}
	if v.Province != "" {
		fmt.Fprintf(&b, "province: %s\n", v.Province)
	}
	if v.Floor != "" {
		fmt.Fprintf(&b, "floor: %s\n", v.Floor)
	}
	if v.Door != "" {
		fmt.Fprintf(&b, "door: %s\n", v.Door)
	}
	if v.Details != "" {
		fmt.Fprintf(&b, "details: %s\n", v.Details)
	}
	if v.AgencyCode != "" {
		fmt.Fprintf(&b, "agency: %s\n", v.AgencyCode)
	}
	if v.Origin != "" {
		fmt.Fprintf(&b, "origin: %s\n", v.Origin)
	}
	_, err := w.Write(b.Bytes())
	return err
}

// PrettyPrint writes the session status as human-readable text. It uses no ANSI
// colours, so NO_COLOR holds without any code.
func (s AuthStatus) PrettyPrint(w io.Writer) error {
	var b bytes.Buffer
	fmt.Fprintf(&b, "profile: %s\n", s.Profile)
	fmt.Fprintf(&b, "logged in: %s\n", yesNo(s.LoggedIn))
	if s.Source != "" {
		fmt.Fprintf(&b, "source: %s\n", s.Source)
	}
	if s.Email != "" {
		fmt.Fprintf(&b, "email: %s\n", s.Email)
	}
	if s.FirstName != "" {
		fmt.Fprintf(&b, "first name: %s\n", s.FirstName)
	}
	fmt.Fprintf(&b, "password stored: %s\n", yesNo(s.PasswordStored))
	fmt.Fprintf(&b, "address set: %s\n", yesNo(s.AddressSet))
	if s.Checked {
		valid := s.SessionValid != nil && *s.SessionValid
		fmt.Fprintf(&b, "session valid: %s\n", yesNo(valid))
	}
	_, err := w.Write(b.Bytes())
	return err
}

// money prints an amount with two decimals, without a currency symbol.
func money(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// yesNo prints a boolean as a word.
func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
