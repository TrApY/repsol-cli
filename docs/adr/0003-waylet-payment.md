# 0003 — Waylet payment: the owner pays, the CLI only reads

Date: 2026-11-05
Status: accepted

## Context

Until now every order was paid on delivery (ADR 0002). Some agencies also accept
Waylet, Repsol's wallet app. The agency endpoint says so: `GET /agency` answers
`wayletPayment`, next to `onlinePayment` and `allowsTip`, and the web only
offers Waylet when it is true.

The flow of the official web application was read from its public JavaScript
bundle (`https://pidetubombona.repsol.es`, the checkout chunk and the `order`
store). Nothing was executed and no request was sent to Repsol:

1. `saveTemporaryOrder` calls `createTemporaryOrder`, which is
   `POST /cart` with the cart (agency, address, material, quantity, unit price,
   discount, tip, delivery date) and returns `{paymentId}`.
2. `getWayletUrl({paymentId})` is `POST /waylet-payment/url` with
   `{paymentId}` and returns `{wayletRedirectionPaymentUrl}`. The web sets
   `window.location.href` to that URL.
3. After paying, Waylet sends the browser to the web route
   `/payment-waylet-callback`. That page (`PagePaymentCallback`) does **not**
   confirm anything: it only calls `getOrderByPaymentId`, which is
   `GET /order-history/payment-id?paymentId=…`. On success it shows the order;
   on any failure it asks again every 7 seconds, at most four more times, then
   shows an error page. There is no confirmation call anywhere in the callback,
   in the checkout, or in the store.

So the order is created by Repsol's backend when the payment goes through, and
the callback only reads it. The web's other online flow, the card paywall, ends
the same way (`getOrderByPaymentId`). This is evidence from the client code; the
server side was not observed, and the response body of the lookup was not seen
either.

## Decision

1. **`--pay waylet` starts a payment; it never places an order.** It runs the
   same reads and the same pending guard as a delivery order, then sends
   `POST /cart` and `POST /waylet-payment/url`, and prints the link as
   `payment_url`, with `payment_id` and `payment_status: awaiting_payment`. The
   result has no `id`, and the agent documentation says the order is not placed.

2. **Confirmation is read, never written.** `--wait[=DUR]` (15 minutes alone)
   polls `GET /order-history/payment-id` every 7 seconds, the web's cadence, and
   returns the order's external id and delivery date once the lookup answers.
   Anything that is not a complete order means "not yet", as in the web, where
   any failure of the lookup means "ask again": a 404 or any other 4xx but
   401/403, the error envelope `{"error": true}` sent with HTTP 200, an empty
   body, `null`, `{}`, and an answer without an external id, an agency or an
   address (the store action of the web reads `agency` and `address`, and fails
   without them). 401, 403 and the failures of the transport or the server are
   not "not yet" answers: a wait tolerates the last two and ends on the first. `repsol order payment ID` runs
   the same lookup, once or with `--wait`, so a timed-out or interrupted wait is
   recoverable.

3. **Only what the web offers can be paid online.** The bundle's
   `onlinePaymentAllowed` getter is
   `!isCalendarDeliveryWithoutDate && (isOrder || status not in the error list ||
   onlinePolicy) && agency && !billable && !blockedPolicy`, and the checkout's
   `setPaymentChange` ignores any method but on-delivery while
   `inspectionExpired` is set. The cart check answers all of those fields, and
   the CLI decodes `billable`, `blockedPolicy`, `isCalendarDeliveryWithoutDate`
   and `inspectionExpired` next to `order`. It refuses a Waylet payment with
   exit 8 `waylet_unavailable` and the hint `--pay delivery` when the cart check
   selects `SOLICITUDE`, when any of those four is set, or when the agency does
   not accept Waylet. The check happens before any write. The online-policy
   refusal of ADR 0002 still comes first. A delivery order is not subject to the
   four conditions: nothing shows the web applies them to it.

4. **The guarantees of ADR 0002 hold, and an unpaid payment is remembered.**
   - The pending guard runs before the cart is saved, and a paid Waylet order
     appears in the history and blocks the next order like any other.
   - The profile order lock is taken before the guards and held for the whole
     wait, so no second order starts while one is waiting for its payment.
   - The lock does not cover a payment without `--wait`: the command prints the
     link and returns, Repsol has no order until the owner pays, and the
     history guard cannot see the payment. So the CLI keeps `{payment id, start
     time}` of the profile's unresolved payment in a private file
     (`payment-PROFILE.toml`, 0600, next to the lock files; not in the
     world-readable `config.toml`, because the id is redacted from every log;
     not in `credentials.toml`, because a logout must not forget a link that can
     still be paid). It is written when the cart returns the id, before the
     link is requested.
   - While it is unresolved, `order create` (Waylet or delivery) exits 8
     `payment_pending` and creates nothing, unless `--force`; a dry run reports
     it too. The guard first asks `GET /order-history/payment-id`: a payment that
     was paid is closed and the paid order, now in the history, is left to the
     pending-order guard; a payment it cannot confirm paid stays open. An
     authentication failure of that lookup is reported as it is.
   - `order payment` and a confirmed `--wait` close the payment. A timed-out or
     interrupted wait leaves it open. `--force` replaces it (Waylet) or drops it
     (a delivery order placed), so an abandoned link cannot block ordering for
     good; this is the owner's decision, as in ADR 0002.
   - `--dry-run` runs every check and creates nothing.
   - `POST /cart` and `POST /waylet-payment/url` are sent once and never
     retried. They are not orders, so they do not use `order_outcome_unknown`.
     A failure of the cart request comes before any payment id, so there is
     nothing to pay: a plain error that says no payment was started. A
     definite refusal (a 4xx) of the link request says the same and forgets the
     payment. A failure of the link request that leaves it unknown whether the
     request was processed (a transport failure or timeout after sending, a 5xx,
     or a 200 answer that cannot be used) exits 9 `payment_outcome_unknown`, not
     retryable: nothing has been charged, since only the owner opening and
     paying the link charges, but the payment may exist. It stays remembered.
     The lookup is a read and keeps the read retries when it is asked once.
   - A wait that ends unconfirmed exits 6 `payment_timeout`, **not retryable**:
     running the command again would create a second payment while the first may
     still be paid. An interrupted wait exits 130. Both tell the agent not to
     start another order and to run `repsol order payment ID --wait`.
   - The wait is a total deadline. Each lookup is a single request cut to what
     is left of the wait, because the transport's own timeout (30 s) and its
     three read attempts would otherwise outlast a short `--wait`.

5. **No `expires` key.** The only field the web reads from the Waylet answer is
   `wayletRedirectionPaymentUrl`. No expiry was found, so none is reported. The
   URL must be `https`; any other answer is refused as a changed API.

6. **MCP is unchanged.** `order_create` has no payment option, so an agent on MCP
   still orders paid on delivery. A blocking wait does not fit a single tool
   call; exposing Waylet through MCP needs a start tool and a lookup tool and is
   left for later.

7. **Logs.** `paymentId` and `wayletRedirectionPaymentUrl` join the redacted
   keys: the id keys the order it pays and the URL opens the payment.

## Consequences

- An agent can order with Waylet when the owner asks, but a human has to open
  the link: the command is complete only after the owner pays.
- The CLI keeps one piece of state between runs, the unresolved payment of the
  profile. A second order started before the first one is paid is refused with
  `payment_pending`, with or without `--wait`. A payment the owner abandons keeps
  refusing until `--force` is used or the payment is paid; there is no expiry,
  because the API reports none. State written by another machine, or lost with
  the file, is not seen: the guard is local.
- `awaiting_payment` cannot tell a payment that was never made from an unknown
  payment id: the lookup answers the same for both.
- The shape of the lookup's answer was inferred from the web code. The CLI
  decodes only `externalId`, `deliveryDate`, the address and the agency name and
  phone, and treats an answer as a confirmation only when it has an external id,
  an agency and an address. The first live Waylet payment is the owner's to try;
  if the answer differs, only `PaymentOrder` has to change, and a payment that
  was paid but is read as `awaiting_payment` is the symptom.
- The fake backend gains the three routes and the controls (`SetWayletPayment`,
  `SetWayletPayAfter`, `ConfirmWaylet`, `ReplyNext`, `DropNext`), so every
  behaviour above is tested without a request to Repsol.
