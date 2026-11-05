<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset=".github/assets/repsol-cli-logo-dark.svg">
    <img src=".github/assets/repsol-cli-logo.svg" width="720" alt="repsol cli logo">
  </picture>
</p>

<p align="center">
  <a href="https://github.com/felipe9962/repsol-cli/releases"><img src="https://img.shields.io/github/v/release/felipe9962/repsol-cli?display_name=tag&style=flat-square&label=release&color=000000" alt="release badge"></a>
  <a href="https://github.com/felipe9962/repsol-cli/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/felipe9962/repsol-cli/ci.yml?branch=main&style=flat-square&label=ci&color=000000" alt="ci badge"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/felipe9962/repsol-cli?style=flat-square&label=go&color=000000" alt="go version badge"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-mit-000000?style=flat-square" alt="license badge"></a>
</p>

---

`repsol` is a command-line tool that lets AI agents (Claude Code, Codex, Cursor,
or anything that can run a shell command or speak MCP) order bottled gas from
Repsol's [`pidetubombona`](https://pidetubombona.repsol.es/) web application on
your behalf, paid on delivery. It compiles to one static Go binary. Output is
JSON when piped, so scripts and agents read the same thing; on a terminal most
commands print plain text for humans.

[cli spec](docs/cli-spec.md) | [agent skill](internal/cli/skills/repsol/SKILL.md) | [decisions](docs/adr) | [releases](https://github.com/felipe9962/repsol-cli/releases)

![order demo](images/demos/order.gif)

<sub>Recorded with `make demo` against the in-process fake backend. Every value
on screen is synthetic; nothing reached Repsol.</sub>

> **Unofficial project.** This tool is not affiliated with, endorsed by, or
> sponsored by Repsol. It is a personal, open-source client of the web
> application's private API. It uses no Repsol branding, logo or imagery, and it
> may stop working at any time if Repsol changes its API.

## Why

Running out of butane is a chore that an agent can handle: "order a bottle" is
one sentence, but the web flow is a login, an address form, a cart and a date
picker. `repsol` turns it into one command that an agent can run on its own:

- `repsol order create` places a real order for a bottle, paid on delivery, with
  no confirmation prompt, and prints the order id and delivery date.
- `repsol order create --pay waylet` pays with Waylet instead: it prints the
  payment link, and `--wait` stays until Repsol confirms the payment.
- You import your session once. After that, agents act on their own, and the
  CLI logs in again by itself if you let it keep your password.
- Agents can also read the catalog and prices, the stored delivery address, the
  available delivery dates, the order history and the last order.
- It refuses to place a second delivery while one is pending or a Waylet payment
  is unpaid, places one order per profile at a time, and never retries an order
  request whose outcome is unknown.

## Quickstart

### Linux or macOS

```bash
curl -fsSL https://raw.githubusercontent.com/felipe9962/repsol-cli/main/scripts/install.sh | sh
repsol version
```

The installer downloads the release archive for your system, checks it against
the release's `checksums.txt` and places `repsol` in `~/.local/bin` (override
with `REPSOL_INSTALL_DIR`, pin a release with `REPSOL_VERSION=v0.1.0`).

### Windows

Download the `.zip` for your architecture from the
[releases page](https://github.com/felipe9962/repsol-cli/releases) and put
`repsol.exe` on your `PATH`.

### go install

```bash
go install github.com/felipe9962/repsol-cli/cmd/repsol@latest
repsol version
```

### Verify a release

Every release archive and its `checksums.txt` carry a GitHub build provenance
attestation:

```bash
gh release download --repo felipe9962/repsol-cli --pattern checksums.txt
gh attestation verify checksums.txt --repo felipe9962/repsol-cli
```

Once you have logged in and set an address, check the whole chain:

```bash
repsol doctor
```

`doctor` probes the local configuration, the stored session, the Repsol API, the
stored address and its agency. It writes nothing.

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

Check or forget the stored session:

```bash
repsol auth status
repsol auth logout
```

Run several accounts or addresses side by side with `--profile NAME` (or
`REPSOL_PROFILE`).

## Set your delivery address

The address is configured once, by hand, because the order history only keeps
partial addresses:

```bash
repsol address set "CALLE NÚMERO, PUEBLO" --floor 2 --door B
repsol address show
```

If several addresses match, the CLI lists them and asks for `--pick N`.

## Place an order

Preview first. A dry run validates the cart with Repsol but places nothing:

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

### Order requests are not confirmed orders

When the result says `order type: SOLICITUDE`, Repsol has received an order
**request**, not a confirmed order. Repsol's order office or the agency validates it by hand
and may email or call you first, for example to identify the holder of the
supply contract. The CLI prints a `note` saying so, but it cannot tell you
whether that has happened: Repsol's API has no order status, so
`repsol order list` marks an order as `pending` only because its delivery date
has not passed yet. If Repsol contacts you, answer them; the CLI cannot do it
for you.

Only one order per profile is placed at a time, even with `--force`. A second
`order create` started while one is running exits 8 with `order_in_progress`
before the cart check and the order request; check `repsol order last` instead
of retrying.

## Agent setup

```bash
repsol mcp install claude-code     # or codex, or cursor
repsol skills get repsol            # the same instructions, as markdown
```

The MCP server exposes `catalog_list`, `address_show`, `schedule_list`,
`order_create`, `order_list` and `order_last`. `order_create` places the order
at once, with no confirmation. It does not expose `--force`: when a delivery is
still pending, the agent reports it to you, and a second delivery goes through
`repsol order create --force` by hand.

## Command surface

| command | what it does |
|---|---|
| `auth login / status / logout` | import a session (HAR, cookie line, or email and password), inspect it, forget it |
| `address set / show` | resolve a free-text address once and store it on the profile |
| `catalog list` | products the agency sells at the stored address, with prices and limits |
| `schedule list` | delivery dates the agency offers for a product |
| `order create` | place a pay-on-delivery order (`--dry-run`, `--product`, `--qty`, `--date`, `--force`) |
| `order list / last / show` | the newest orders, the last one, or one by its eight-digit reference |
| `doctor` | check the config, session, API, address and agency without writing |
| `config path / get / set / list` | the configuration files and values |
| `mcp` / `mcp install TARGET` | serve the MCP tools on stdio, or register them in claude-code, codex or cursor |
| `skills list / get` | the embedded agent skill documents |
| `completion bash\|zsh\|fish\|powershell` | shell completion |
| `version` | version and build metadata |

Global flags: `--profile`, `--format json|pretty`, `--error-format text|json`,
`--no-input`, `--debug` (HTTP calls on stderr, with secrets redacted).

## Shell completion

```bash
# zsh
repsol completion zsh > "${fpath[1]}/_repsol"

# bash
repsol completion bash > /etc/bash_completion.d/repsol

# fish
repsol completion fish > ~/.config/fish/completions/repsol.fish

# powershell
repsol completion powershell >> $PROFILE
```

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

## Building from source

```bash
git clone https://github.com/felipe9962/repsol-cli && cd repsol-cli
make build     # bin/repsol
make test      # go test -race ./...
make lint      # go vet + golangci-lint when it is installed
make demo      # re-record images/demos/order.gif (needs vhs and jq)
```

`go test ./...` never touches the network: every test runs against an in-process
fake of the three backends. A read-only live suite lives behind the `live` build
tag.

## Documentation

- [docs/cli-spec.md](docs/cli-spec.md): the interface contract and every
  endpoint the CLI uses
- [docs/adr](docs/adr): decisions, such as how sessions work and why ordering
  is autonomous
- [SKILL.md](internal/cli/skills/repsol/SKILL.md): the instructions agents get
  from `repsol skills get repsol`
- [AGENTS.md](AGENTS.md): vocabulary and rules for contributors and coding
  agents

## Contributing

Issues and pull requests are welcome. When Repsol changes its API the CLI exits
7; the [API changed](.github/ISSUE_TEMPLATE/api-changed.md) issue template
collects what is needed to fix it. Never attach a HAR file, a token, a cookie or
your personal data to an issue; see [SECURITY.md](SECURITY.md).

## Disclaimer

This is an unofficial client of a private API. Repsol may change or block it at
any time. You are responsible for the orders it places with your account, and
you pay the driver on delivery. Not affiliated with Repsol.

## License

[MIT](LICENSE)
