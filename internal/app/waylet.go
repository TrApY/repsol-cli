package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/felipe9962/repsol-cli/internal/apperr"
	"github.com/felipe9962/repsol-cli/internal/config"
	"github.com/felipe9962/repsol-cli/internal/repsolapi"
)

// PaymentPollInterval is how often a payment wait asks whether the payment was
// confirmed. It is the cadence of the web's own payment callback page.
const PaymentPollInterval = 7 * time.Second

// PaymentStateTTL is how long the profile remembers an unpaid payment before
// the next order looks it up one last time and, when it still is not paid,
// forgets it. A link nobody pays within a day is not going to be paid, and
// remembering it for good would block ordering forever.
const PaymentStateTTL = 24 * time.Hour

// The states of a Waylet payment.
const (
	paymentAwaiting  = "awaiting_payment"
	paymentConfirmed = "confirmed"
	paymentAbandoned = "abandoned"
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
	// PaymentID names the payment. It is empty when a damaged state file was
	// removed and no id could be read from it.
	PaymentID string `json:"payment_id,omitempty"`
	// Status is "awaiting_payment", "confirmed" or "abandoned".
	Status string `json:"payment_status"`
	// ID is the external id of the order the payment produced. It is empty
	// until the payment is confirmed.
	ID string `json:"id,omitempty"`
	// DeliveryDate is the promised delivery date, once the payment is confirmed.
	DeliveryDate string `json:"delivery_date,omitempty"`
	// Agency is the agency that will deliver the order, once the payment is
	// confirmed.
	Agency *AgencyView `json:"agency,omitempty"`
	// Note is a caveat for the owner, set when a payment was abandoned without
	// knowing whether it was paid, or after it was.
	Note string `json:"note,omitempty"`
}

// startWayletPayment starts the Waylet payment of a planned order: it saves the
// cart, asks for the payment page and, when in.Wait is set, waits for the owner
// to pay. Nothing here places an order by itself: Repsol creates it when the
// payment goes through, and the CLI only reads it.
//
// The two writes are sent once each. The cart is not an order and the page
// charges nothing until the owner opens it, so no order-outcome error is
// needed, but the payment id the cart returns is remembered as soon as it
// exists: the link is payable until the owner pays it, Repsol has no order for
// it until then, and the history cannot tell a later run that it is still
// open. The caller holds the order lock, so the lock covers the wait; without a
// wait it is released when the link is printed, and the remembered payment is
// what keeps the next order from paying the same delivery twice.
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
		// No payment id came back, so there is no link to pay and nothing to
		// track, whatever happened to the request.
		return OrderResult{}, noPaymentStarted(err)
	}
	pending := config.PendingPayment{PaymentID: temp.PaymentID, StartedAt: a.now()}
	if err := a.store.SavePendingPayment(a.profile, pending); err != nil {
		return OrderResult{}, err
	}

	link, err := a.api.WayletURL(ctx, temp.PaymentID)
	if err != nil {
		return OrderResult{}, a.wayletURLFailed(ctx, temp.PaymentID, err)
	}

	// The lookup does not know the id the cart answered: it answers under the
	// id the payment link carries. From here on the payment goes by that one,
	// in the state file, in the result and in every hint, and the cart id is
	// kept beside it so that either finds the payment. A link that names no
	// such id leaves the cart id, the only one there is.
	paymentID := temp.PaymentID
	if id := repsolapi.WayletPaymentID(link); id != "" && id != temp.PaymentID {
		paymentID = id
		pending.PaymentID, pending.CartID = id, temp.PaymentID
		if err := a.store.SavePendingPayment(a.profile, pending); err != nil {
			return OrderResult{}, err
		}
	}

	result.PaymentID = paymentID
	result.PaymentURL = link
	result.PaymentStatus = paymentAwaiting
	if in.OnPaymentLink != nil {
		in.OnPaymentLink(PaymentLink{URL: link, ID: paymentID})
	}
	if in.Wait == 0 {
		return result, nil
	}

	order, err := a.waitForPayment(ctx, paymentID, in.Wait)
	if err != nil {
		return OrderResult{}, err
	}
	a.settlePaid(paymentID, order)
	result.PaymentStatus = paymentConfirmed
	result.ID = string(order.ExternalID)
	if order.DeliveryDate != "" {
		result.DeliveryDate = order.DeliveryDate
	}
	if order.Agency != nil {
		if order.Agency.Name != "" {
			result.Agency.Name = order.Agency.Name
		}
		if order.Agency.Phone != "" {
			result.Agency.Phone = order.Agency.Phone
		}
	}
	return result, nil
}

// wayletURLFailed turns the failure of the payment page request into the error
// the owner sees, and settles what the profile remembers.
//
// A definite refusal proves that no link was handed out: the payment is
// forgotten and the failure says nothing was started. So does a failure that
// came before the request left this machine (a name lookup, a refused
// connection): nothing reached Repsol, so the payment is forgotten and the
// order can be retried. Anything else leaves it open. A transport failure after
// the request was sent, a 5xx, an answer that cannot be used or an interruption
// came after the request may have been processed, so the payment may exist, and
// the CLI cannot tell. Nothing has been charged either way, because only the
// owner paying the link charges, and no link was handed out, but the payment
// stays remembered and the failure is not retryable.
func (a *App) wayletURLFailed(ctx context.Context, paymentID string, err error) error {
	switch {
	case apperr.From(err).NotSent:
		_ = a.store.ClearPendingPayment(a.profile, paymentID)
		typed := *apperr.From(err)
		typed.Hint = "the request for the payment link never left this machine: " +
			"no payment link was handed out and nothing was charged, so it is safe to try again"
		return &typed
	case ctx.Err() != nil:
		return interruptedLinkRequest(paymentID)
	case urlOutcomeUnknown(err):
		return paymentOutcomeUnknown(paymentID, err)
	}
	_ = a.store.ClearPendingPayment(a.profile, paymentID)
	return noPaymentStarted(err)
}

// urlOutcomeUnknown reports whether the failure of the payment page request
// leaves it unknown whether the payment was created: it was sent and then the
// transport failed, the server failed, or the answer was a success that cannot
// be used. A 4xx is an answer, and refuses the request.
func urlOutcomeUnknown(err error) bool {
	typed := apperr.From(err)
	switch typed.Category {
	case apperr.Network:
		return true
	case apperr.APIChanged:
		return typed.HTTPStatus == 0 || typed.HTTPStatus/100 == 2 || typed.HTTPStatus/100 == 5
	default:
		return false
	}
}

// closePayment forgets a payment: the one named, or whichever is open when the
// id is empty. A failure to write the state is ignored: the next order looks
// the payment up again and settles it then.
func (a *App) closePayment(paymentID string) {
	_ = a.store.ClearPendingPayment(a.profile, paymentID)
}

// deliveryPending reports whether the paid order is still to be delivered: its
// delivery date is today or later in Madrid, the rule of the history guard.
func (a *App) deliveryPending(order repsolapi.PaymentOrder) bool {
	return order.DeliveryDate != "" && order.DeliveryDate >= a.today()
}

// settlePaid is what a payment that was seen paid leaves behind. The payment is
// forgotten only once it can no longer cause a duplicate, which is when its
// delivery is behind us: until then it stays remembered, because the history
// guard cannot be trusted to list the order already, or under the same agency
// and locality, and the next order has to be refused on the payment's own word
// (see ensurePaymentResolved).
func (a *App) settlePaid(paymentID string, order repsolapi.PaymentOrder) {
	if a.deliveryPending(order) {
		return
	}
	a.closePayment(paymentID)
}

// ensurePaymentResolved refuses an order while the profile has a Waylet payment
// that can still produce a delivery. It asks Repsol first, whatever --force says:
// --force is about the order history, and a payment link may have been paid
// since. It never guesses the session: an authentication failure is reported as
// it is. What it did to the remembered payment on its own is returned as
// notices for the order result.
//
//   - a payment that was paid and whose delivery is today or later refuses the
//     order with pending_order, naming the order and its date, without looking
//     at the history: the history may not list it yet, or not for this agency
//     and locality. The payment stays remembered, and nothing but its own
//     "repsol order payment ID --abandon" lets go of it. A paid payment whose
//     delivery is behind us is forgotten (unless this is a dry run, which writes
//     nothing) and the order goes on to the history guard;
//   - a payment that is not paid, or whose lookup failed, refuses the order,
//     and the two are told apart: the first is payment_pending, the second
//     payment_check_failed, "could not check". It is forgotten, with a notice,
//     when the order says AbandonPayment, and also when it is more than
//     PaymentStateTTL old and the lookup said conclusively that it is not paid;
//   - a state file that cannot be read refuses the order with
//     payment_state_invalid, unless the order says AbandonPayment.
func (a *App) ensurePaymentResolved(ctx context.Context, in PlaceOrderInput) ([]string, error) {
	pending, found, err := a.store.LoadPendingPayment(a.profile)
	if err != nil {
		if !unreadablePaymentState(err) {
			return nil, err
		}
		if !in.AbandonPayment {
			return nil, paymentStateInvalid(err)
		}
		if !in.DryRun {
			if err := a.store.DiscardPendingPayment(a.profile); err != nil {
				return nil, err
			}
		}
		return []string{forgetNotice(in.DryRun, "the Waylet payment state of this profile could not be read")}, nil
	}
	if !found {
		return nil, nil
	}

	order, paid, err := a.api.OrderByPaymentID(ctx, pending.PaymentID)
	switch {
	case ctx.Err() != nil:
		if err == nil {
			err = ctx.Err()
		}
		return nil, err
	case err != nil && apperr.From(err).Category == apperr.Auth:
		return nil, err
	case err == nil && paid:
		if a.deliveryPending(order) {
			return nil, paidPaymentPending(pending, order)
		}
		if !in.DryRun {
			a.closePayment(pending.PaymentID)
		}
		return nil, nil
	}

	started := pending.StartedAt.In(madrid).Format("2006-01-02 15:04")
	switch {
	case in.AbandonPayment:
		what := fmt.Sprintf("the Waylet payment started at %s was abandoned", started)
		if err != nil {
			what = fmt.Sprintf("the Waylet payment started at %s could not be checked and was abandoned; "+
				"it may have been paid", started)
		}
		return a.forgetPayment(in.DryRun, pending.PaymentID, what)
	case err == nil && a.now().Sub(pending.StartedAt) > PaymentStateTTL:
		what := fmt.Sprintf("the Waylet payment started at %s was not paid within %s", started, PaymentStateTTL)
		return a.forgetPayment(in.DryRun, pending.PaymentID, what)
	}
	return nil, paymentPending(pending, err)
}

// forgetPayment drops the remembered payment and returns the notice that says
// so. A dry run writes nothing and says what would happen.
func (a *App) forgetPayment(dryRun bool, paymentID, what string) ([]string, error) {
	if !dryRun {
		if err := a.store.ClearPendingPayment(a.profile, paymentID); err != nil {
			return nil, err
		}
	}
	return []string{forgetNotice(dryRun, what)}, nil
}

// forgetNotice words what happened to a remembered payment. what is a clause
// that already says why.
func forgetNotice(dryRun bool, what string) string {
	if dryRun {
		return what + "; it would be forgotten when an order is placed"
	}
	return what + "; it was forgotten"
}

// unreadablePaymentState reports whether loading the remembered payment failed
// because the file is damaged, which "--abandon" can clear, as opposed to a
// failure of the disk or of the profile name.
func unreadablePaymentState(err error) bool {
	return apperr.From(err).Code == "config_invalid"
}

// paymentStateInvalid is the refusal of an order while the file that remembers
// the Waylet payment cannot be read: the CLI cannot tell whether a payment is
// waiting, so it holds the order back, and says how to let go.
func paymentStateInvalid(cause error) error {
	e := apperr.New(apperr.Refused, "payment_state_invalid",
		"the file that remembers the Waylet payment of this profile cannot be read").
		WithHint("if no payment link is waiting, run \"repsol order payment --abandon\" to remove it, " +
			"or add --abandon-payment to this command; check the order history first if a link may have been paid")
	e.Err = cause
	return e
}

// paymentPending is the refusal of an order while a payment is open. lookupErr,
// when set, is why the payment could not be confirmed either way: then the
// refusal says so, instead of claiming that the payment is unpaid.
func paymentPending(p config.PendingPayment, lookupErr error) error {
	started := p.StartedAt.In(madrid).Format("2006-01-02 15:04")
	if lookupErr != nil {
		typed := apperr.From(lookupErr)
		e := apperr.New(apperr.Refused, "payment_check_failed",
			fmt.Sprintf("could not check whether the Waylet payment started at %s was paid", started)).
			WithHint(fmt.Sprintf("nothing was ordered: try again, or run \"repsol order payment %s\" to check it; "+
				"pass --abandon-payment only if the owner abandoned that payment", p.PaymentID))
		e.Err = lookupErr
		e.Retryable = typed.Retryable
		return e
	}
	return apperr.New(apperr.Refused, "payment_pending",
		fmt.Sprintf("a Waylet payment started at %s is not paid yet", started)).
		WithHint(fmt.Sprintf("open its link and pay, then run \"repsol order payment %s --wait\"; "+
			"pass --abandon-payment only if the owner abandoned that payment and wants a new order", p.PaymentID))
}

// paidPaymentPending is the refusal of an order while a Waylet payment that was
// paid has a delivery still to come.
func paidPaymentPending(p config.PendingPayment, order repsolapi.PaymentOrder) error {
	what := "the order of a Waylet payment"
	if order.ExternalID != "" {
		what = fmt.Sprintf("order %s", order.ExternalID)
	}
	return apperr.New(apperr.Refused, "pending_order",
		fmt.Sprintf("%s for %s was paid with Waylet and is still pending", what, order.DeliveryDate)).
		WithHint(fmt.Sprintf("--force does not skip a paid delivery; if the owner cancelled it with the agency, "+
			"run \"repsol order payment %s --abandon\" so the CLI stops holding orders back for it", p.PaymentID))
}

// AbandonPayment forgets the Waylet payment the profile remembers, at the
// owner's explicit request. The id is optional: given, it must be the
// remembered one. It looks the payment up first, only to tell the owner what it
// found: a payment that turns out to be paid is forgotten all the same, with a
// note that its order exists, because the owner may have cancelled that delivery
// and has no other way to order again before its date. A lookup that fails does
// not stop it either, except an authentication failure, which is reported as it
// is. A state file that cannot be read is removed without a lookup. It takes the
// order lock, so it cannot run beside an order that is waiting for this payment.
func (a *App) AbandonPayment(ctx context.Context, paymentID string) (PaymentView, error) {
	paymentID = strings.TrimSpace(paymentID)
	release, err := a.lockOrder()
	if err != nil {
		return PaymentView{}, err
	}
	defer release()

	pending, found, err := a.store.LoadPendingPayment(a.profile)
	if err != nil {
		if !unreadablePaymentState(err) {
			return PaymentView{}, err
		}
		if err := a.store.DiscardPendingPayment(a.profile); err != nil {
			return PaymentView{}, err
		}
		return PaymentView{PaymentID: paymentID, Status: paymentAbandoned,
			Note: "the state file of the Waylet payment could not be read and was removed"}, nil
	}
	switch {
	case !found:
		return PaymentView{}, apperr.New(apperr.NotFound, "no_pending_payment",
			"no Waylet payment is remembered for this profile").
			WithHint("there is nothing to abandon; \"repsol order create\" is not held back by a payment")
	case paymentID != "" && !pending.Is(paymentID):
		return PaymentView{}, apperr.New(apperr.Refused, "payment_mismatch",
			"the Waylet payment remembered for this profile is not the one named").
			WithHint(fmt.Sprintf("the remembered payment is %s", pending.PaymentID))
	}

	view := PaymentView{PaymentID: pending.PaymentID, Status: paymentAbandoned}
	order, paid, err := a.api.OrderByPaymentID(ctx, pending.PaymentID)
	switch {
	case ctx.Err() != nil:
		if err == nil {
			err = ctx.Err()
		}
		return PaymentView{}, err
	case err != nil && apperr.From(err).Category == apperr.Auth:
		return PaymentView{}, err
	case err != nil:
		view.Note = "the payment could not be checked, so it may have been paid; the CLI no longer holds orders back for it"
	case paid:
		view.ID = string(order.ExternalID)
		view.DeliveryDate = order.DeliveryDate
		view.Agency = agencyOf(order)
		view.Note = "the payment was paid and its order exists; the CLI no longer holds orders back for it, " +
			"and the order history may still list that delivery as pending"
	}
	if err := a.store.ClearPendingPayment(a.profile, pending.PaymentID); err != nil {
		return PaymentView{}, err
	}
	return view, nil
}

// agencyOf returns the agency of a paid order as the CLI prints it, or nil when
// the answer names none.
func agencyOf(order repsolapi.PaymentOrder) *AgencyView {
	if order.Agency != nil && (order.Agency.Name != "" || order.Agency.Phone != "") {
		return &AgencyView{Name: order.Agency.Name, Phone: order.Agency.Phone}
	}
	return nil
}

// PaymentStatus looks up the order a Waylet payment produced. The id may be
// either name of the payment the profile remembers (the one the lookup takes or
// the one the cart answered); the lookup always goes out under the first. An id
// the profile does not remember is looked up as given. With a zero wait
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
	// An unreadable state file is not this command's business: it looks the id
	// up as given, and the order that finds the file damaged says so.
	if pending, found, err := a.store.LoadPendingPayment(a.profile); err == nil && found && pending.Is(paymentID) {
		paymentID = pending.PaymentID
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
	a.settlePaid(paymentID, order)
	view := PaymentView{
		PaymentID:    paymentID,
		Status:       paymentConfirmed,
		ID:           string(order.ExternalID),
		DeliveryDate: order.DeliveryDate,
	}
	view.Agency = agencyOf(order)
	return view, nil
}

// waitForPayment polls the order of a payment until it exists or the wait is
// over. The deadline is counted on the app's clock, and it is a total: every
// lookup is one request that may not outlive what is left of the wait, so a
// slow Repsol cannot stretch it. A lookup that fails on the network or on the
// server is tolerated and asked again, and so is every answer the web reads as
// "not yet" (see repsolapi.OrderByPaymentID); any other failure ends the wait,
// with a hint that the payment may still have gone through. A canceled context
// ends it as an interruption.
func (a *App) waitForPayment(ctx context.Context, paymentID string, wait time.Duration) (repsolapi.PaymentOrder, error) {
	deadline := a.now().Add(wait)
	var lastErr error
	for {
		remaining := deadline.Sub(a.now())
		if remaining <= 0 {
			return repsolapi.PaymentOrder{}, paymentTimeout(paymentID, wait, lastErr)
		}
		order, found, err := a.pollPayment(ctx, paymentID, remaining)
		switch {
		case ctx.Err() != nil:
			return repsolapi.PaymentOrder{}, interruptedWaiting(paymentID)
		case err == nil && found:
			return order, nil
		case errors.Is(err, context.DeadlineExceeded):
			// The lookup ran into the deadline of the wait itself. The
			// transport hands the context error back as it is (see
			// httpx.Client.Do), so this is reachable; without it the timeout
			// would report the lookup's deadline instead of the last real
			// failure.
			return repsolapi.PaymentOrder{}, paymentTimeout(paymentID, wait, lastErr)
		case err != nil && apperr.From(err).Category != apperr.Network:
			return repsolapi.PaymentOrder{}, withPaymentHint(err, paymentID)
		}
		lastErr = err

		remaining = deadline.Sub(a.now())
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

// pollPayment sends one lookup of the payment that may take at most limit.
func (a *App) pollPayment(ctx context.Context, paymentID string, limit time.Duration) (repsolapi.PaymentOrder, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	return a.api.PollOrderByPaymentID(ctx, paymentID)
}

// checkPaymentHint tells the owner how to learn whether a payment went through,
// and not to start another order meanwhile.
func checkPaymentHint(paymentID string) string {
	return fmt.Sprintf("the link may still be paid; do not start another order, "+
		"run \"repsol order payment %s --wait\" to keep waiting, or without --wait to check once", paymentID)
}

// paymentTimeout is the error of a wait that ended without a confirmation. It
// is not retryable: repeating the command that waited would start a second
// payment, while the first may still be paid. The way on is to keep waiting for
// the same payment, which is what the hint names.
func paymentTimeout(paymentID string, wait time.Duration, lastLookup error) error {
	e := apperr.New(apperr.Network, "payment_timeout",
		fmt.Sprintf("the Waylet payment was not confirmed within %s", wait)).
		WithHint(checkPaymentHint(paymentID))
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

// interruptedLinkRequest is the error of a payment link request the user
// canceled. Unlike an interrupted wait there is no link to pay: none was handed
// out, so the owner cannot pay anything, but the request may have been processed
// and the payment may exist.
func interruptedLinkRequest(paymentID string) error {
	return apperr.New(apperr.Interrupted, "interrupted",
		"interrupted while requesting the Waylet payment link; no link was handed out and nothing was charged, "+
			"but the payment may have been created").
		WithHint(fmt.Sprintf("do not start another order: run \"repsol order payment %s\" to check it", paymentID))
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

// noPaymentStarted adds to a failure that came before any payment link existed
// that nothing was charged, unless the error has a hint of its own.
func noPaymentStarted(err error) error {
	typed := *apperr.From(err)
	if typed.Hint == "" {
		typed.Hint = "no payment was started and nothing was charged"
	}
	return &typed
}

// paymentOutcomeUnknown is the error of a payment page request that failed
// after it was sent. The payment may have been created, so the error is not
// retryable and says where to look. Nothing has been charged: only the owner
// paying the link charges.
func paymentOutcomeUnknown(paymentID string, cause error) error {
	return apperr.New(apperr.OutcomeUnknown, "payment_outcome_unknown",
		"the Waylet payment link request failed after it was sent; "+
			"nothing has been charged, but the payment may have been created").
		WithHint(fmt.Sprintf("do not start another order: run \"repsol order payment %s\" to check it, "+
			"and \"repsol order payment %s --abandon\" only if the owner does not want that payment", paymentID, paymentID)).
		WithCause(cause)
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
