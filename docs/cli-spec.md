# repsol CLI specification

Every command, its flags, its JSON output and the error envelope. The JSON
keys are stable: agents may rely on them.

- stdout carries data only. stderr carries errors, prompts and debug logs.
- `--format` defaults to `pretty` when stdout is a terminal and `json`
  otherwise. `--format json` and `--format pretty` always win.
- `--error-format` defaults to `text`; `json` prints one compact JSON line.
  `REPSOL_ERROR_FORMAT` sets the default; the flag wins.
- Errors are printed as `repsol: <message>`, followed by `hint: <hint>` when
  there is a hint.

## Global flags

| flag | meaning |
|---|---|
| `--profile NAME` | profile to use (default: `REPSOL_PROFILE`, then the configured default, then `default`) |
| `--format json\|pretty` | output format |
| `--no-input` | never prompt; a missing input is a usage error |
| `--debug` | log requests and responses to stderr, with secrets redacted |
| `--error-format text\|json` | error output format |

## Exit codes

| code | meaning |
|---|---|
| 0 | success |
| 1 | generic failure |
| 2 | usage error, ambiguous/unknown address or product |
| 3 | auth: no session, session expired or rejected |
| 4 | not found |
| 5 | blocked or rate limited |
| 6 | network or timeout (reads) |
| 7 | API changed: unexpected status or shape |
| 8 | refused: pending order, no delivery to this address, product unavailable |
| 9 | order outcome unknown: check `repsol order last` before retrying |
| 130 | interrupted |

## Error envelope

`--error-format json` prints exactly one line with these keys, always present:

```json
{"code":"pending_order","category":"refused","exit_code":8,"retryable":false,"message":"order 20000001 for 2026-11-05 is still pending at this address","http_status":0,"endpoint":"","hint":"pass --force only if the owner explicitly wants another delivery"}
```

| key | type | meaning |
|---|---|---|
| `code` | string | snake_case machine code, e.g. `pending_order` |
| `category` | string | `generic`, `usage`, `auth`, `not_found`, `blocked`, `network`, `api_changed`, `refused`, `outcome_unknown`, `interrupted` |
| `exit_code` | number | the process exit code |
| `retryable` | bool | whether repeating the call could succeed |
| `message` | string | human-readable description |
| `http_status` | number | HTTP status of the failing call, or 0 |
| `endpoint` | string | API path of the failing call, never a full URL |
| `hint` | string | what to do next |

## `repsol auth`

### `repsol auth login`

Exactly one method:

| flag | meaning |
|---|---|
| `--har FILE` | import the session from a browser HAR exported "with sensitive data" |
| `--token-stdin` | read one cookie line from stdin: `glt_<APIKey>=<token>; gmid=<gmid>` |
| `--email ADDR` | log in with email and password |
| `--password-stdin` | read the password from stdin (with `--email`) |

`--password-stdin` is implied when `--email` is used and stdin is not a
terminal. On a terminal the CLI prompts without echoing. No method, or more
than one, is a usage error (`login_method`). `--email` without a password is
`password_required`.

Output (`AuthStatus`):

```json
{
  "profile": "default",
  "logged_in": true,
  "source": "stored",
  "email": "owner@example.com",
  "first_name": "Owner",
  "password_stored": true,
  "address_set": false,
  "checked": false
}
```

`source` is `stored`, `env` or empty. When no address is stored yet, the CLI
prints the hint `run "repsol address set \"STREET NUMBER, TOWN\""` on stderr.

### `repsol auth status [--check]`

Same `AuthStatus` shape. `--check` proves the session still works: it performs
one signature request and adds `"session_valid": true` (or `false`). Without
`--check` no network call is made.

### `repsol auth logout`

```json
{"profile": "default", "removed": true}
```

## `repsol address`

### `repsol address show`

```json
{
  "profile": "default",
  "summary": "Calle Falsa 1, 28001 Pueblo, Madrid, floor 2, door B",
  "full_address": "Calle Falsa 1, 28001 Pueblo, Madrid",
  "postal_code": "28001",
  "locality": "Pueblo",
  "province": "Madrid",
  "floor": "2",
  "door": "B",
  "details": "",
  "agency_code": "0000000001",
  "origin": "repsol"
}
```

Without a stored address it exits 2 with code `no_address`.

### `repsol address set TEXT`

| flag | meaning |
|---|---|
| `--floor TEXT` | floor of the delivery address |
| `--door TEXT` | door of the delivery address |
| `--details TEXT` | free-form delivery notes |
| `--pick N` | choose the N-th candidate, 1-based |

`TEXT` is free text, resolved the way the web app resolves it: autocomplete,
geocode when the web would, then agency lookup. Only candidates with a street
number count. Several candidates exit 2 (`ambiguous_address`) and list them as
`1) <fullAddress> [origin]`; no candidate exits 2 (`address_needs_number`); a
neutral candidate exits 2 (`address_not_supported`); a geocoded address the
agency does not serve exits 8 (`address_not_served`).

## `repsol catalog list`

```json
[
  {
    "product": "butano",
    "material_id": "153",
    "name": "Butano 12.5kg",
    "price": 18.89,
    "price_per_kilo": 1.51,
    "max_quantity": 9
  }
]
```

Needs a stored address; without one it exits 2 (`no_address`).

## `repsol schedule list [--product P]`

```json
{
  "product": "butano",
  "material_id": "153",
  "mode": "NO_SCHEDULE",
  "warning": false,
  "dates": ["2026-11-03", "2026-11-05"]
}
```

Dates are sorted ascending. Without a stored address it exits 2
(`no_address`); with no dates it exits 8 (`no_delivery_dates`).

## `repsol order`

### `repsol order create`

| flag | default | meaning |
|---|---|---|
| `--product P` | `butano` | alias or numeric material id |
| `--qty N` | `1` | number of bottles |
| `--date YYYY-MM-DD` | earliest offered | delivery date |
| `--pay` | `delivery` | `delivery` (pay the driver) or `waylet` (pay on a Waylet page) |
| `--wait[=DUR]` | off | with `--pay waylet`, wait for the payment to be confirmed; `--wait` alone waits 15 minutes. A duration needs the `=` form, as in `--wait=30m` |
| `--dry-run` | off | validate only, write nothing |
| `--force` | off | skip the pending-order guard and the unpaid-payment guard |

```json
{
  "id": "800000000000000001",
  "dry_run": false,
  "order_type": "SOLICITUDE",
  "product": "butano",
  "material_id": "153",
  "quantity": 1,
  "unit_price": 18.89,
  "total": 18.89,
  "delivery_date": "2026-11-04",
  "address": "Calle Falsa 1, 28001 Pueblo, Madrid",
  "agency": {"name": "Agencia Test", "phone": "900 000 000", "code": "0000000001"},
  "payment": "on_delivery",
  "note": "this is an order request, not a confirmed order: …"
}
```

`note` is set when `order_type` is `SOLICITUDE`, on a dry run too. A solicitude
is a request that Repsol's order office or the agency validates by hand. They
may email or call the owner to identify the contract holder before delivering,
and nothing in the API reports that the request is on hold.

### Paying with Waylet

`repsol order create --pay waylet` pays the order on a Waylet page instead of
on delivery. It sends the same checks as a delivery order, then:

1. refuses with `waylet_unavailable` when the agency does not accept Waylet,
   when the cart check says the order is a `SOLICITUDE` (a request Repsol
   validates by hand cannot be paid online), or when the cart check says the
   web would not offer an online payment for it: `billable`, `blockedPolicy`,
   `isCalendarDeliveryWithoutDate` or `inspectionExpired` (a delivery order is
   not subject to these four);
2. refuses with `payment_pending` while an earlier Waylet payment of the profile
   is unpaid (see below);
3. saves the cart (`POST /cart`) and asks for the payment page
   (`POST /waylet-payment/url`);
4. prints the link. **No order exists yet**: Repsol creates it when the owner
   pays the link, and the CLI only reads it.

Output without `--wait`, with exit code 0:

```json
{
  "dry_run": false,
  "order_type": "ORDER",
  "product": "butano",
  "material_id": "153",
  "quantity": 1,
  "unit_price": 18.89,
  "total": 18.89,
  "delivery_date": "2026-11-04",
  "address": "Calle Falsa 1, 28001 Pueblo, Madrid",
  "agency": {"code": "0000000001"},
  "payment": "waylet",
  "payment_url": "https://waylet.example.test/pay?paymentId=…",
  "payment_id": "fake-payment-1",
  "payment_status": "awaiting_payment"
}
```

`id` is absent until the payment is confirmed. `payment_url` and `payment_id`
are only present once the payment was started; `--dry-run` runs every check,
creates nothing and prints neither. The API reports no expiry for the link, so
there is no `expires` key.

With `--wait`, the command prints `open this page to pay with Waylet …` and the
link on **stderr** at once, then polls every 7 seconds for the order of the
payment. When Repsol confirms it, stdout carries the final result: the same
keys, with `payment_status` `confirmed`, `id` set to the order's external id,
and `delivery_date` and the agency name and phone as Repsol reports them.

The profile order lock is held for the whole wait, so another `order create` for
the profile exits 8 with `order_in_progress`. A confirmed Waylet order shows in
the history and blocks the next order like any other.

**An unpaid payment blocks every later order.** Without `--wait` the command
returns as soon as it prints the link and releases the lock, and until the owner
pays, Repsol has no order that the history could show. So the CLI remembers the
payment id and start time of the profile's unresolved payment in a private file
next to `credentials.toml` (`payment-PROFILE.toml`, mode 0600; logging out does
not remove it). While it is unresolved, `order create` with either payment
method exits 8 with `payment_pending` and creates nothing, `--dry-run` included.
Before it refuses, the command asks Repsol whether the payment was paid since:
if it was, the payment is closed and the paid order, now in the history, is
handled by the usual pending-order guard. `order payment` and a confirmed
`--wait` close the payment too. A wait that times out or is interrupted leaves
it open. `--force` skips the guard when the owner wants another order anyway; it
replaces the remembered payment with the new one (a Waylet order) or drops it
(a delivery order), so an abandoned link can never block ordering for good.

The two writes of the flow are sent once and never retried. Neither is an
order. A failure of the cart request is a plain error with the hint `no payment
was started and nothing was charged`: no payment id came back, so there is
nothing to pay. A failure of the payment page request that proves it was
refused (a 4xx) is the same. One that leaves it unknown whether the request was
processed (a transport failure or timeout after sending, a 5xx, or a 200 whose
answer cannot be used) exits 9 with `payment_outcome_unknown`, not retryable:
nothing has been charged, but the payment may have been created. It stays
remembered, and the hint says to run `repsol order payment PAYMENT_ID` and not
to start another order. When `--wait` ends without a confirmation the command
exits 6 with `payment_timeout`, **not retryable** (running `order create` again
would start a second payment; the link may still be paid), with the hint
`repsol order payment PAYMENT_ID --wait`. Ctrl-C while waiting exits 130 with the
same hint.

The wait is a total deadline: each lookup is one request cut to what is left of
the wait, with no transport retries inside it.

Errors: `invalid_quantity` (exit 2, above the per-material maximum), `unknown_product`
(exit 2), `invalid_date` (exit 2), `unsupported_payment` (exit 2),
`wait_requires_waylet` (exit 2, `--wait` without `--pay waylet`), `invalid_wait`
(exit 2, a negative wait), `waylet_unavailable` (exit 8, see above),
`payment_timeout` (exit 6, the wait ended unconfirmed; not retryable),
`payment_pending` (exit 8, an earlier Waylet payment is unpaid; see above),
`payment_outcome_unknown` (exit 9, the payment page request failed after it was
sent; the payment may exist, nothing was charged),
`date_unavailable` (exit 8, the date is not offered), `product_unavailable`
(exit 8), `no_delivery_dates` (exit 8), `online_policy_required` (exit 8, order
on the web), `pending_order` (exit 8, a delivery is already pending),
`order_in_progress` (exit 8, another order for the profile is being placed; do
not retry, run `repsol order last` once it finishes; `--force` does not
override it; `--dry-run` does not take the lock),
`order_outcome_unknown` (exit 9, run `repsol order last` before retrying).

### `repsol order payment PAYMENT_ID [--wait[=DUR]]`

Looks up the order a Waylet payment produced (`GET /order-history/payment-id`).
It takes no lock and may be repeated. Without `--wait` it asks once. It only
reads Repsol; its one local effect is to forget the profile's remembered
payment when that payment is confirmed.

```json
{"payment_id": "fake-payment-1", "payment_status": "awaiting_payment"}
```

```json
{
  "payment_id": "fake-payment-1",
  "payment_status": "confirmed",
  "id": "800000000000000003",
  "delivery_date": "2026-11-04",
  "agency": {"name": "Agencia Test", "phone": "900 000 000"}
}
```

A payment counts as confirmed only when the answer carries an order external id
and the agency and address objects the web needs (a bare `{}` or an answer
missing one of them is not a confirmation). Like the web, which asks again on
any failure of this lookup, the CLI reads as `awaiting_payment` (and, with
`--wait`, polls again on) a 404 or any other 4xx but 401/403, the error envelope
`{"error": true, …}` sent with HTTP 200, a null or empty body and an incomplete
answer; 401 and 403 end the wait. `awaiting_payment` also covers a payment that
was never made or that Repsol does not know: the API answers the same for both.
With `--wait` it polls like `order create --wait` and exits 6 with
`payment_timeout` when the wait ends. An empty id exits 2
(`invalid_payment_id`).

### `repsol order list [--limit N]`

Default limit 10, maximum 100.

```json
[
  {
    "reference": "20000001",
    "created_at": "2026-11-01T09:12:33Z",
    "delivery_date": "2026-11-04",
    "product": "butano",
    "material_id": "153",
    "quantity": 1,
    "unit_price": 18.89,
    "total": 18.89,
    "address": "Calle Falsa 1, Pueblo",
    "agency": {"name": "Agencia Test", "phone": "900 000 000", "code": "0000000001"},
    "pending": true
  }
]
```

The history has no status field. `pending` only means that `delivery_date` is
today or later in `Europe/Madrid`: it does not mean the order was confirmed, and
a request on hold or cancelled by phone still shows as pending.

### `repsol order last`

The newest history entry, in the same shape. Without any order it exits 4 with
code `no_orders`.

### `repsol order show REFERENCE`

`REFERENCE` is the 8-digit `referenceSAC` from the history. It is searched in
the newest 100 entries. A reference that is not 8 digits exits 2
(`invalid_reference`); a reference that is not found exits 4 (`order_not_found`).

## `repsol doctor`

```json
{
  "ok": true,
  "checks": [
    {"name": "config", "ok": true, "detail": ""},
    {"name": "credentials", "ok": true, "detail": ""},
    {"name": "session", "ok": true, "detail": ""},
    {"name": "api", "ok": true, "detail": ""},
    {"name": "address", "ok": true, "detail": ""},
    {"name": "agency", "ok": true, "detail": ""}
  ]
}
```

A check that depends on a failed check fails too, with the detail
`skipped: <name> failed`. When any check fails the command exits 1 with code
`doctor_failed`.

## `repsol config`

| command | output |
|---|---|
| `repsol config path` | `{"config": "…", "credentials": "…"}` |
| `repsol config list` | the same keys as `config get` for every key |
| `repsol config get KEY` | the value of the key |
| `repsol config set KEY VALUE` | nothing on success |

The only key is `default_profile`. Any other key exits 2 (`unknown_key`).

## `repsol skills`

| command | output |
|---|---|
| `repsol skills list` | `[{"name": "repsol", "description": "…"}]` |
| `repsol skills get NAME` | the raw markdown of the skill, whatever `--format` says |

An unknown name exits 4 (`skill_not_found`).

## `repsol mcp`

- `repsol mcp` serves the MCP protocol on stdio. Tools: `catalog_list`,
  `address_show`, `schedule_list`, `order_create`, `order_list`, `order_last`.
  A tool result is the same JSON the CLI prints; a failure is returned as a
  tool error whose text is the error envelope.
- `repsol mcp install claude-code|codex|cursor` writes the server entry into
  that harness's configuration, preserving the other entries. It prints
  `{"target": "…", "path": "…", "status": "installed|updated|unchanged"}`.

`order_create` takes `product`, `quantity`, `date` and `dry_run`. It has no
`force`: an agent must ask the owner before placing a second delivery while one
is pending or a Waylet payment started from the CLI is unpaid (`payment_pending`).
It has no payment method either: orders placed through MCP are paid on
delivery. Waylet payments are a CLI feature.

## `repsol version`

```json
{"version": "0.1.0", "commit": "abc1234", "date": "2026-10-30T00:00:00Z", "go": "go1.26.2"}
```

## `repsol completion bash|zsh|fish|powershell`

Cobra's own completion scripts, on stdout.
