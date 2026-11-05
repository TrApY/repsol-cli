---
name: repsol
description: Order bottled gas (butane, propane) from Repsol for the owner, paid on delivery
---

# Repsol bottled gas

`repsol` orders bottled gas from Repsol for the owner's own account. Orders are
paid on delivery by default, and the driver collects the money. Paying with
Waylet is opt-in and only when the owner asks for it (see below); never by card.

## Before ordering

`repsol order create` places a real order, paid on delivery, with no confirmation. Use `--dry-run` to preview.

The preview runs the same checks as a real order, including the cart check, and
places nothing. After placing a real order, tell the owner the product, the
quantity, the price, the delivery date and the address.

When the result has a `note` (order type `SOLICITUDE`), pass it on: the order is
a request that Repsol may hold until the owner answers an email or a call that
identifies the contract holder. `pending: true` in the history only means the
delivery date has not passed, not that the order was confirmed.

## Exit codes

| Exit | Category | Meaning |
|------|----------|---------|
| 0 | ok | The command succeeded. |
| 1 | generic | An unclassified failure. |
| 2 | usage | A bad invocation: unknown flag, invalid value or missing input. |
| 3 | auth | The session is missing or expired; run `repsol auth login`. |
| 4 | not_found | The requested order or skill does not exist. |
| 5 | blocked | Repsol, a firewall or a rate limit refused the request. |
| 6 | network | A transport failure or a timeout on a read. |
| 7 | api_changed | Repsol answered with an unexpected status or shape. |
| 8 | refused | The request was understood but not allowed. |
| 9 | order_outcome_unknown | The order may or may not have been placed. |
| 130 | interrupted | The run was cancelled. |

## Paying with Waylet

Use `--pay waylet` only when the owner asks to pay with Waylet. It is not the
default and the MCP `order_create` tool has no payment option.

```bash
repsol order create --pay waylet --dry-run   # checks only, creates nothing
repsol order create --pay waylet             # prints the payment link
repsol order create --pay waylet --wait=15m  # stays until the payment is confirmed
repsol order payment PAYMENT_ID              # was it confirmed? (add --wait=DUR to poll)
```

- No order exists until the owner pays the link. The result has
  `payment_status: awaiting_payment`, a `payment_url` and a `payment_id`, and no
  `id`. Give the owner the `payment_url` and say the order is not placed yet.
  Never call a Waylet order placed or confirmed before `payment_status` is
  `confirmed`.
- With `--wait` the link is printed on stderr while the command waits, and the
  result on stdout only once the payment is confirmed. Pass a duration with
  `=` (`--wait=30m`); `--wait` alone waits 15 minutes.
- Exit 8 `waylet_unavailable`: the agency does not accept Waylet, or the order is
  a request (`SOLICITUDE`) that cannot be paid online. Offer `--pay delivery`.
- Exit 6 `payment_timeout`: the wait ended, but the owner may still pay the link.
  Do not create another order; run `repsol order payment PAYMENT_ID` later and
  report what it says. The same goes for exit 130 while waiting.
- Never run `order create --pay waylet` again to get the link back: the earlier
  `payment_url` is the one to use. Creating a new payment while one may still be
  paid risks a duplicate delivery.
- The pending-order and order-in-progress rules below apply unchanged.

## Pending orders

If it exits 8 with code `pending_order`, tell the owner which order is pending. Pass `--force` only when the owner explicitly asks for another delivery while one is pending. A delivery cancelled by phone still shows as pending until its date passes.

## Order in progress

If it exits 8 with code `order_in_progress`, another order is being placed for the same profile. Do not retry and do not pass `--force`; run `repsol order last` once it finishes and report what you see.

## Unknown order outcome

If it exits 9, do not retry. Run `repsol order last` and report what you see.

## Secrets

Never put tokens or passwords on the command line.

Pipe a session line with `repsol auth login --token-stdin`, or pipe the password
with `repsol auth login --email ADDRESS --password-stdin`. On a terminal the
commands prompt for the secret without echoing it.

## Examples

One example per command:

```bash
# Show the stored delivery address.
repsol address show

# Resolve and store a free-text address, with an optional floor and door.
repsol address set "Calle Falsa 1, Pueblo" --floor 2 --door B

# List the products the delivery agency sells.
repsol catalog list

# List the delivery dates the agency offers for a product.
repsol schedule list --product butano

# Preview an order without placing it.
repsol order create --dry-run

# Place one bottle of butane on the earliest offered date.
repsol order create --product butano --qty 1

# Pay with Waylet, wait up to 15 minutes for the payment, then check it later.
repsol order create --pay waylet --wait
repsol order payment PAYMENT_ID

# List the newest orders.
repsol order list --limit 10

# Show the newest order.
repsol order last

# Show one order by its eight-digit reference.
repsol order show 20000001

# Check the configuration and the Repsol backends.
repsol doctor

# Print the paths of the configuration files.
repsol config path

# Read and change the default profile.
repsol config get default_profile
repsol config set default_profile work
repsol config list

# Print the version and build metadata.
repsol version

# List the embedded skills and read one as markdown.
repsol skills list
repsol skills get repsol

# Log in, inspect the session and remove it.
repsol auth login --har session.har
repsol auth status --check
repsol auth logout
```
