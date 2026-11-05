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
   A 404, an empty body, `null` and `{}` all mean "not yet", as in the web, where
   any failure of the lookup means "ask again". `repsol order payment ID` runs
   the same lookup, once or with `--wait`, so a timed-out or interrupted wait is
   recoverable.

3. **Only an order can be paid online.** In the bundle, online payment methods
   are offered when the cart is an order (`isOrder`, or a status outside the
   error list) and never alongside the solicitude write, so the CLI treats a
   solicitude as not payable online. It refuses a Waylet payment when the cart
   check selects `SOLICITUDE`, or when the agency does not accept Waylet, with
   exit 8 `waylet_unavailable` and the hint `--pay delivery`. The check happens
   before any write. The online-policy refusal of ADR 0002 still comes first.

4. **The guarantees of ADR 0002 hold.**
   - The pending guard runs before the cart is saved, and a paid Waylet order
     appears in the history and blocks the next order like any other.
   - The profile order lock is taken before the guard and held for the whole
     wait, so no second order starts while one is waiting for its payment.
   - `--dry-run` runs every check and creates nothing.
   - `POST /cart` and `POST /waylet-payment/url` are sent once and never
     retried. They are not orders, so their failures are plain errors that say
     nothing was charged, not `order_outcome_unknown`. The lookup is a read and
     keeps the read retries.
   - A wait that ends unconfirmed exits 6 `payment_timeout` (retryable), and an
     interrupted one exits 130. Both tell the agent not to start another order
     and to run `repsol order payment ID`.

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
- Without `--wait`, the lock is released as soon as the link is printed. A second
  order started before the first one is paid is not blocked, because Repsol has
  no order yet and the CLI keeps no state between runs. The agent documentation
  tells the agent not to start another order before `repsol order payment`
  reports on the first.
- `awaiting_payment` cannot tell a payment that was never made from an unknown
  payment id: the lookup answers the same for both.
- The shape of the lookup's answer was inferred from the web code. The CLI decodes
  only `externalId`, `deliveryDate` and the agency name and phone, and treats any
  non-empty object as a confirmation. The first live Waylet payment is the owner's
  to try; if the answer differs, only `PaymentOrder` has to change.
- The fake backend gains the three routes and two controls (`SetWayletPayment`,
  `SetWayletPayAfter`, `ConfirmWaylet`), so every behaviour above is tested
  without a request to Repsol.
