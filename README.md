# repsol-cli

A command-line tool that lets AI agents (Claude Code, Codex, Cursor, or anything
that can run a shell command or speak MCP) order bottled gas from Repsol's
`pidetubombona` web application on your behalf, paid on delivery.

> **Unofficial project.** This tool is not affiliated with, endorsed by, or
> sponsored by Repsol. It is a personal, open-source client of the web
> application's private API. It uses no Repsol branding, logo or imagery, and it
> may stop working at any time if Repsol changes its API.

## What it does

- `repsol order create` places a real order for a bottle, paid on delivery, with
  no confirmation prompt, and prints the order id and delivery date as JSON.
- You import your session once. After that, agents act on their own.
- Agents can also read: the catalog and prices, the stored delivery address, the
  available delivery dates, the order history and the last order.

## Install

```bash
go install github.com/felipe9962/repsol-cli/cmd/repsol@latest
```

Or download a release binary for macOS, Linux or Windows from the
[releases page](https://github.com/felipe9962/repsol-cli/releases).

## Log in

A Repsol session is two values: a Gigya **login token** and the **`gmid` device
cookie** the token is bound to. The web app keeps them on two different domains,
so copying the site's cookies is not enough.

### From a HAR file (recommended)

1. In Chrome, open DevTools → Settings → Preferences → Network and enable
   **"Allow to generate HAR with sensitive data"**.
2. Open the Network panel, log in at <https://pidetubombona.repsol.es/> and
   choose **"Export HAR (with sensitive data)"**.
3. Import it:

   ```bash
   repsol auth login --har ~/Downloads/pidetubombona.repsol.es.har
   ```

4. **Delete the HAR file.** It contains your plain-text password, your live
   session and your personal data.

### From a pasted cookie line

```bash
printf 'glt_<APIKey>=<token>; gmid=<gmid>' | repsol auth login --token-stdin
```

On a terminal the CLI prompts for the line without echoing it.

### With your email and password

```bash
repsol auth login --email you@example.com --password-stdin
```

This stores the password in a `0600` file so the CLI can log in again by itself
when the token expires. The password is never passed on the command line and
never written to a log.

## Set your delivery address

The address is configured once, by hand, because the order history only keeps
partial addresses:

```bash
repsol address set "CALLE NÚMERO, PUEBLO"
repsol address show
```

If several addresses match, the CLI lists them and asks for `--pick N`.

## Place an order

Preview first — a dry run validates the cart with Repsol but places nothing:

```bash
repsol order create --dry-run
```

Then order for real. Payment is always on delivery:

```bash
repsol order create                       # one butano bottle, earliest date
repsol order create --product propano --qty 2 --date 2026-11-05
repsol order list
repsol order last
repsol order show 20000001
```

Before writing, the CLI reads your recent orders and refuses to place a second
delivery to the same address while one is still pending. That brake exists to
stop accidental duplicates, not to ask permission: a delivery you cancelled by
phone still looks pending until its date passes, and `--force` covers that case
when you explicitly want another delivery.

Only one order per profile is placed at a time, even with `--force`. A second
`order create` started while one is running exits 8 with `order_in_progress`
before the cart check and the order request; check `repsol order last` instead of retrying.

## Agent setup

```bash
repsol mcp install claude-code     # or codex, or cursor
repsol skills get repsol            # the same instructions, as markdown
```

The MCP server exposes `catalog_list`, `address_show`, `schedule_list`,
`order_create`, `order_list` and `order_last`. `order_create` does not expose
`--force`: an agent must ask you before placing a second delivery while one is
pending.

## Output and exit codes

stdout carries data only; stderr carries errors, prompts and debug logs. Output
is JSON when stdout is not a terminal, and `pretty` when it is. Override it with
`--format json|pretty`, and get machine-readable errors with
`--error-format json` (or `REPSOL_ERROR_FORMAT=json`).

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
| 8 | refused: pending order, order in progress, no delivery to this address, product unavailable |
| 9 | order outcome unknown: check `repsol order last` before retrying |
| 130 | interrupted |

On exit 9 the order may or may not have been placed. Do not retry: run
`repsol order last` and look.

## Environment variables

| variable | effect |
|---|---|
| `REPSOL_PROFILE` | profile to use |
| `REPSOL_LOGIN_TOKEN`, `REPSOL_GMID` | non-persistent session override |
| `REPSOL_EMAIL`, `REPSOL_PASSWORD` | non-persistent credentials override |
| `REPSOL_ERROR_FORMAT` | `text` (default) or `json` |
| `REPSOL_API_BASE_URL`, `REPSOL_GIGYA_BASE_URL`, `REPSOL_ADDRESS_BASE_URL` | base URL overrides, for tests |
| `NO_COLOR` | reserved; the CLI never emits colour |

## Development

```bash
make build     # builds bin/repsol
make test      # go test -race ./...
make lint      # go vet + golangci-lint when it is installed
```

`go test ./...` never touches the network: every test runs against an in-process
fake of the three backends. A read-only live suite lives behind the `live` build
tag.

## License

MIT. See [LICENSE](LICENSE).
