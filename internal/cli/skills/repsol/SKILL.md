---
name: repsol
description: Order bottled gas (butane, propane) from Repsol for the owner, paid on delivery
---

# Repsol bottled gas

`repsol` orders bottled gas from Repsol for the owner's own account. Orders are
paid on delivery, never by card, and the driver collects the money.

## Before ordering

`repsol order create` places a real order, paid on delivery, with no confirmation. Use `--dry-run` to preview.

The preview runs the same checks as a real order, including the cart check, and
places nothing. Show the owner the product, the quantity, the price, the
delivery date and the address before placing a real order.

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

## Pending orders

If it exits 8 with code `pending_order`, tell the owner which order is pending. Pass `--force` only when the owner explicitly asks for another delivery while one is pending. A delivery cancelled by phone still shows as pending until its date passes.

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
