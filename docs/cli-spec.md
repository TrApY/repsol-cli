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
| `--pay` | `delivery` | only `delivery` is supported |
| `--dry-run` | off | validate only, write nothing |
| `--force` | off | skip the pending-order guard |

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
  "payment": "on_delivery"
}
```

Errors: `invalid_quantity` (exit 2, above the per-material maximum), `unknown_product`
(exit 2), `invalid_date` (exit 2), `unsupported_payment` (exit 2),
`date_unavailable` (exit 8, the date is not offered), `product_unavailable`
(exit 8), `no_delivery_dates` (exit 8), `online_policy_required` (exit 8, order
on the web), `pending_order` (exit 8, a delivery is already pending),
`order_in_progress` (exit 8, another order for the profile is being placed; do
not retry, run `repsol order last` once it finishes; `--force` does not
override it; `--dry-run` does not take the lock),
`order_outcome_unknown` (exit 9, run `repsol order last` before retrying).

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
is pending.

## `repsol version`

```json
{"version": "0.1.0", "commit": "abc1234", "date": "2026-10-30T00:00:00Z", "go": "go1.26.2"}
```

## `repsol completion bash|zsh|fish|powershell`

Cobra's own completion scripts, on stdout.
