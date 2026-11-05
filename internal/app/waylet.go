package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// PaymentPollInterval is how often a payment wait asks whether the payment was
// confirmed. It is the cadence of the web's own payment callback page.
const PaymentPollInterval = 7 * time.Second

// The states of a Waylet payment.
const (
	paymentAwaiting  = "awaiting_payment"
	paymentConfirmed = "confirmed"
)

// PaymentLink is the Waylet page that pays one order, and the payment it
// belongs to.
type PaymentLink struct {
	// URL is the https page the owner opens to pay.
	URL string
	// ID is the payment id.
	ID string
}

// PaymentView is the state of one Waylet payment, as "repsol order payment"
// prints it.
type PaymentView struct {
	// PaymentID names the payment.
	PaymentID string `json:"payment_id"`
	// Status is "awaiting_payment" or "confirmed".
	Status string `json:"payment_status"`
	// ID is the external id of the order the payment produced. It is empty
	// until the payment is confirmed.
	ID string `json:"id,omitempty"`
	// DeliveryDate is the promised delivery date, once the payment is confirmed.
	DeliveryDate string `json:"delivery_date,omitempty"`
	// Agency is the agency that will deliver the order, once the payment is
	// confirmed.
	Agency *AgencyView `json:"agency,omitempty"`
}

// startWayletPayment starts the Waylet payment of a planned order: it saves the
// cart, asks for the payment page and, when in.Wait is set, waits for the owner
// to pay. Nothing here places an order by itself: Repsol creates it when the
// payment goes through, and the CLI only reads it.
//
// The two writes are sent once each. The cart is not an order and the page
// charges nothing until the owner opens it, so a failure of either is reported
// as it is, and no outcome-unknown error is needed. The caller holds the order
// lock, so the lock covers the wait: no other order for the profile starts
// while this one is waiting for its payment.
func (a *App) startWayletPayment(
	ctx context.Context,
	in PlaceOrderInput,
	result OrderResult,
	scheduleMode *string,
	addr repsolapi.Address,
	unitPrice float64,
	date repsolapi.DeliveryDate,
) (OrderResult, error) {
	temp, err := a.api.CreateTemporaryOrder(ctx, repsolapi.TemporaryOrderRequest{
		ScheduleMode:   scheduleMode,
		AgencyCode:     addr.AgencyCode,
		Address:        addr,
		MaterialID:     result.MaterialID,
		Quantity:       result.Quantity,
		UnitPrice:      unitPrice,
		DeliveryDateID: date.ID,
		DeliveryDate:   date.Date,
	})
	if err != nil {
		return OrderResult{}, noPaymentStarted(err)
	}
	link, err := a.api.WayletURL(ctx, temp.PaymentID)
	if err != nil {
		return OrderResult{}, noPaymentStarted(err)
	}

	result.PaymentID = temp.PaymentID
	result.PaymentURL = link
	result.PaymentStatus = paymentAwaiting
	if in.OnPaymentLink != nil {
		in.OnPaymentLink(PaymentLink{URL: link, ID: temp.PaymentID})
	}
	if in.Wait == 0 {
		return result, nil
	}

	order, err := a.waitForPayment(ctx, temp.PaymentID, in.Wait)
	if err != nil {
		return OrderResult{}, err
	}
	result.PaymentStatus = paymentConfirmed
	result.ID = string(order.ExternalID)
	if order.DeliveryDate != "" {
		result.DeliveryDate = order.DeliveryDate
	}
	if order.Agency.Name != "" {
		result.Agency.Name = order.Agency.Name
	}
	if order.Agency.Phone != "" {
		result.Agency.Phone = order.Agency.Phone
	}
	return result, nil
}

// PaymentStatus looks up the order a Waylet payment produced. With a zero wait
// it asks once and reports "awaiting_payment" when the payment is not confirmed
// yet; with a wait it polls until the payment is confirmed or the wait ends in
// payment_timeout. It only reads, so it takes no lock and is safe to repeat.
func (a *App) PaymentStatus(ctx context.Context, paymentID string, wait time.Duration) (PaymentView, error) {
	paymentID = strings.TrimSpace(paymentID)
	if paymentID == "" {
		return PaymentView{}, apperr.New(apperr.Usage, "invalid_payment_id",
			"the payment id is empty").
			WithHint("the id is the payment_id of \"repsol order create --pay waylet\"")
	}
	if wait < 0 {
		return PaymentView{}, invalidWait(wait)
	}

	var (
		order repsolapi.PaymentOrder
		found bool
		err   error
	)
	if wait == 0 {
		order, found, err = a.api.OrderByPaymentID(ctx, paymentID)
	} else {
		order, err = a.waitForPayment(ctx, paymentID, wait)
		found = err == nil
	}
	if err != nil {
		return PaymentView{}, err
	}
	if !found {
		return PaymentView{PaymentID: paymentID, Status: paymentAwaiting}, nil
	}
	view := PaymentView{
		PaymentID:    paymentID,
		Status:       paymentConfirmed,
		ID:           string(order.ExternalID),
		DeliveryDate: order.DeliveryDate,
	}
	if order.Agency.Name != "" || order.Agency.Phone != "" {
		view.Agency = &AgencyView{Name: order.Agency.Name, Phone: order.Agency.Phone}
	}
	return view, nil
}

// waitForPayment polls the order of a payment until it exists or the wait is
// over. The deadline is counted on the app's clock. A lookup that fails on the
// network is tolerated and asked again; any other failure ends the wait, with a
// hint that the payment may still have gone through. A canceled context ends it
// as an interruption.
func (a *App) waitForPayment(ctx context.Context, paymentID string, wait time.Duration) (repsolapi.PaymentOrder, error) {
	deadline := a.now().Add(wait)
	var lastErr error
	for {
		order, found, err := a.api.OrderByPaymentID(ctx, paymentID)
		switch {
		case ctx.Err() != nil:
			return repsolapi.PaymentOrder{}, interruptedWaiting(paymentID)
		case err == nil && found:
			return order, nil
		case err != nil && apperr.From(err).Category != apperr.Network:
			return repsolapi.PaymentOrder{}, withPaymentHint(err, paymentID)
		}
		lastErr = err

		remaining := deadline.Sub(a.now())
		if remaining <= 0 {
			return repsolapi.PaymentOrder{}, paymentTimeout(paymentID, wait, lastErr)
		}
		if err := a.sleep(ctx, min(PaymentPollInterval, remaining)); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return repsolapi.PaymentOrder{}, interruptedWaiting(paymentID)
			}
			return repsolapi.PaymentOrder{}, err
		}
	}
}

// checkPaymentHint tells the owner how to learn whether a payment went through,
// and not to start another order meanwhile.
func checkPaymentHint(paymentID string) string {
	return fmt.Sprintf("the link may still be paid; do not start another order, "+
		"run \"repsol order payment %s\" to check", paymentID)
}

// paymentTimeout is the error of a wait that ended without a confirmation. It
// is retryable: asking again later may find the payment confirmed.
func paymentTimeout(paymentID string, wait time.Duration, lastLookup error) error {
	e := apperr.New(apperr.Network, "payment_timeout",
		fmt.Sprintf("the Waylet payment was not confirmed within %s", wait)).
		WithHint(checkPaymentHint(paymentID))
	e.Retryable = true
	if lastLookup != nil {
		e.Err = lastLookup
	}
	return e
}

// interruptedWaiting is the error of a wait the user canceled.
func interruptedWaiting(paymentID string) error {
	return apperr.New(apperr.Interrupted, "interrupted",
		"interrupted while waiting for the Waylet payment").
		WithHint(checkPaymentHint(paymentID))
}

// withPaymentHint adds the check-the-payment hint to an error that ended a
// wait, unless the error already has a hint of its own.
func withPaymentHint(err error, paymentID string) error {
	typed := *apperr.From(err)
	if typed.Hint == "" {
		typed.Hint = checkPaymentHint(paymentID)
	}
	return &typed
}

// noPaymentStarted adds to a failure of the payment start that nothing was
// charged, unless the error has a hint of its own.
func noPaymentStarted(err error) error {
	typed := *apperr.From(err)
	if typed.Hint == "" {
		typed.Hint = "no payment was started and nothing was charged"
	}
	return &typed
}

// wayletUnavailable is the refusal of a Waylet payment the order cannot use.
func wayletUnavailable(reason string) error {
	return apperr.New(apperr.Refused, "waylet_unavailable", reason).
		WithHint("use --pay delivery to pay the driver on delivery")
}

// invalidWait is the usage error of a negative wait.
func invalidWait(wait time.Duration) error {
	return apperr.New(apperr.Usage, "invalid_wait",
		fmt.Sprintf("the wait %s is negative", wait)).
		WithHint("use a positive duration such as 15m")
}
