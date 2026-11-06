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
| `--force` | off | skip the pending-order guard (the order history). It does **not** skip the remembered Waylet payment, which is looked up all the same |
| `--abandon-payment` | off | forget the unpaid Waylet payment the profile remembers (or its unreadable state file) and go on with the order. A payment that was paid is not forgotten this way |

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
2. refuses while an earlier Waylet payment of the profile is unpaid, paid with a
   delivery still due, or could not be checked (see below);
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
  "payment_url": "https://waylet.example.test/pay?token=fake-payment-1:…&id=…",
  "payment_id": "fake-payment-1",
  "payment_status": "awaiting_payment"
}
```

`id` is absent until the payment is confirmed. `payment_id` is the id Repsol
looks the paid order up under, which is the one the payment link's `token`
leads with (ADR 0003, amendment of 2026-11-06), not the id the cart answered;
`repsol order payment` accepts either. `payment_url` and `payment_id`
are only present once the payment was started; `--dry-run` runs every check,
creates nothing and prints neither. The API reports no expiry for the link, so
there is no `expires` key.

With `--wait`, the command prints `open this page to pay with Waylet …` and the
link on **stderr** at once, then polls every 7 seconds for the order of the
payment. When Repsol confirms it, stdout carries the final result: the same
keys, with `payment_status` `confirmed`, `id` set to the order's external id,
and `delivery_date` and the agency name and phone as Repsol reports them.

The profile order lock is held for the whole wait, so another `order create` for
the profile exits 8 with `order_in_progress`. A confirmed Waylet order blocks
the next order twice over: through the history like any other, and through the
remembered payment, which refuses on its own until the delivery date has passed.

**A remembered payment blocks every later order.** Without `--wait` the command
returns as soon as it prints the link and releases the lock, and until the owner
pays, Repsol has no order that the history could show. So the CLI remembers the
payment id and start time of the profile's unresolved payment in a private file
next to `credentials.toml` (`payment-PROFILE.toml`, mode 0600; logging out does
not remove it). Before every `order create`, `--dry-run` included, it asks
Repsol about that payment, **whatever `--force` says** (`--force` is about the
order history only), and the answer decides:

| Repsol says | the order | the payment |
|---|---|---|
| paid, delivery today or later | exit 8 `pending_order`, naming the order and its date; `--force` and `--abandon-payment` do not skip it | stays remembered, and refuses on its own word: the history may not list the order yet, or not under the same agency and locality |
| paid, delivery behind us | goes on to the history guard | forgotten (a dry run writes nothing) |
| not paid | exit 8 `payment_pending` | stays, unless `--abandon-payment` (forgotten, with a notice) or it is over 24 hours old (forgotten, with a notice) |
| the lookup failed | exit 8 `payment_check_failed` ("could not check whether the payment … was paid"), retryable when the failure was the network or the server | stays, unless `--abandon-payment` |
| 401 | exit 3, as everywhere | stays |

`order payment` and a confirmed `--wait` apply the same rule when they see the
payment paid: it is forgotten only once its delivery is behind us. A wait that
times out or is interrupted leaves it open. An unpaid payment older than
24 hours is looked up one last time; if the lookup says it is not paid, the
next order forgets it and reports the fact in `notices`; if the lookup fails, it
stays.

The ways to let go of a payment are explicit and separate from `--force`:
`order create --abandon-payment` (an unpaid or unchecked payment; a paid one is
refused with `pending_order`) and `repsol order payment [PAYMENT_ID] --abandon`
(see below). A state file that cannot be read refuses the order with
`payment_state_invalid`, whose hint says to run `repsol order payment --abandon`
(or to pass `--abandon-payment`), which removes it.

`notices` is an array of strings in the order result (present only when the
command forgot a payment or a state file on its own, or, with `--dry-run`,
would): `"the Waylet payment started at … was abandoned; it was forgotten"`.

The two writes of the flow are sent once and never retried. Neither is an
order. A failure of the cart request is a plain error with the hint `no payment
was started and nothing was charged`: no payment id came back, so there is
nothing to pay. A failure of the payment page request that proves it was
refused (a 4xx) is the same, and so is one that never left the machine (the
connection was refused or a name did not resolve: the transport proves that not
a byte of the request was written): the payment is forgotten, the network
failure is reported with the hint that no link was handed out, and trying again
is safe. One that leaves it unknown whether the request was
processed (a transport failure or timeout after sending, a 5xx, or a 200 whose
answer cannot be used) exits 9 with `payment_outcome_unknown`, not retryable:
nothing has been charged, but the payment may have been created. It stays
remembered, and the hint says to run `repsol order payment PAYMENT_ID` and not
to start another order. Ctrl-C while the link is requested exits 130 with the
same advice: it says that no link was handed out (so nothing is waiting to be
paid) and that the payment may exist; it does not say that the link may still be
paid. When `--wait` ends without a confirmation the command
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
`payment_check_failed` (exit 8, the earlier payment could not be looked up, so
nothing is known about it; retryable when the failure was the network or the
server), `payment_state_invalid` (exit 8, the file that remembers the payment
cannot be read; `repsol order payment --abandon` removes it),
`payment_outcome_unknown` (exit 9, the payment page request failed after it was
sent; the payment may exist, nothing was charged),
`date_unavailable` (exit 8, the date is not offered), `product_unavailable`
(exit 8), `no_delivery_dates` (exit 8), `online_policy_required` (exit 8, order
on the web), `pending_order` (exit 8, a delivery is already pending: in the history, or a
Waylet payment that was paid and is still due, which `--force` does not skip),
`order_in_progress` (exit 8, another order for the profile is being placed; do
not retry, run `repsol order last` once it finishes; `--force` does not
override it; `--dry-run` does not take the lock),
`order_outcome_unknown` (exit 9, run `repsol order last` before retrying).

### `repsol order payment [PAYMENT_ID] [--wait[=DUR]] [--abandon]`

Looks up the order a Waylet payment produced (`GET /order-history/payment-id`).
It takes no lock and may be repeated. Without `--wait` it asks once. It only
reads Repsol; its one local effect is to forget the profile's remembered
payment when that payment is confirmed and its delivery is behind us (until
then the payment stays remembered, so it keeps refusing other orders). The
`--abandon` form below is the exception: it writes the state and takes the lock.

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

A payment counts as confirmed only when the answer carries the agency and
address objects the web needs (a bare `{}` or an answer missing one of them is
not a confirmation). Like the web, which reads the external id as
`externalId || ""`, it does not need an order id: a confirmed payment without
one prints no `id` (the pretty output says "payment confirmed" instead of an
empty `order` line). Like the web, which asks again on
any failure of this lookup, the CLI reads as `awaiting_payment` (and, with
`--wait`, polls again on) a 404 or any other 4xx but 401/403, the error envelope
`{"error": true, …}` sent with HTTP 200, a null or empty body and an incomplete
answer; 401 and 403 end the wait. `awaiting_payment` also covers a payment that
was never made or that Repsol does not know: the API answers the same for both.
With `--wait` it polls like `order create --wait` and exits 6 with
`payment_timeout` when the wait ends. An empty id exits 2
(`invalid_payment_id`).

With `--abandon` it forgets the payment the profile remembers; `PAYMENT_ID` is
optional then, and must be the remembered one when given (`payment_mismatch`,
exit 8, otherwise; `no_pending_payment`, exit 4, when nothing is remembered).
Use it only when the owner will not pay that link. It takes the order lock
(`order_in_progress` beside a running order), cannot be combined with `--wait`
(`abandon_with_wait`, exit 2), and looks the payment up first, only to report:

```json
{"payment_id": "fake-payment-1", "payment_status": "abandoned"}
```

A payment that turns out to be paid is forgotten all the same, because the owner
may have cancelled that delivery and has no other way to order again before its
date; the output then carries the order (`id`, `delivery_date`, `agency`) and a
`note` saying that the payment was paid and the history may still list the
delivery. A lookup that fails does not stop it (the `note` says the payment could
not be checked, so it may have been paid); a 401 does. A state file that cannot
be read is removed without a lookup, with a `note` that says so.

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
`force` and no way to abandon a payment: an agent must ask the owner before
placing a second delivery while one is pending or a Waylet payment started from
the CLI is unpaid, paid and still due, or could not be checked
(`payment_pending`, `pending_order`, `payment_check_failed`). The result may
carry `notices`, to pass on to the owner.
It has no payment method either: orders placed through MCP are paid on
delivery. Waylet payments are a CLI feature.

## `repsol version`

```json
{"version": "0.1.0", "commit": "abc1234", "date": "2026-10-30T00:00:00Z", "go": "go1.26.2"}
```

## `repsol completion bash|zsh|fish|powershell`

Cobra's own completion scripts, on stdout.
