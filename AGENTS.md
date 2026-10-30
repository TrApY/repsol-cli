# AGENTS.md

Guidance for coding agents working on this repository.

## What this project is

`repsol-cli` is a Go CLI and MCP server that orders bottled gas from Repsol's
`pidetubombona` web application for the owner, paid on delivery and without a
confirmation prompt. It talks to the web application's private JSON API with the
owner's own account.

## Domain vocabulary

- **Session** — the pair `(login token, gmid)`. The Gigya login token is bound
  to the `gmid` device cookie; a token alone is rejected. The two live on
  different domains.
- **Signature** — the `uid` + `uidSignature` + `signatureTimestamp` triple
  Gigya returns. Repsol's API expects them as headers. The web refreshes it
  every 50 seconds.
- **Solicitude** — a pay-on-delivery order request. `POST /solicitude` places
  one. It is not an order until the agency delivers it.
- **`referenceSAC` vs `externalId`** — two different identifiers for the same
  order. `externalId` is what `POST /solicitude` returns; `referenceSAC` is the
  8-digit reference in the history. `order show` takes the `referenceSAC`.
- **Agency** — the Repsol distributor that serves an address. It is resolved
  from the address with `GET /agency` and changes with the locality.
- **Material id** — the numeric id of a product (`153` is Butano 12.5 kg).
  `materialId` is a string on the wire.
- **Pending** — the history has no status field, so a delivery is considered
  pending when its `deliveryDate` is today or later in `Europe/Madrid`.

## Contributor rules

- **No live writes, ever.** Not while developing, not in a test, not "just once
  to check". The only writes to the real API are the owner's own orders. The
  live test suite is read-only and lives behind the `live` build tag.
- **Fixtures are synthetic.** Never commit a real HAR, token, password, NIF,
  address, name, phone, coordinate or order id. Every fixture is invented.
- **Never log secrets.** The login token, `gmid`, password, signature, UID and
  the personal data in the Gigya `data` block never reach a log line, an error
  message, the error envelope or an MCP response. The transport redacts them;
  keep it that way.
- **Never retry a write.** A failed order request may still have reached
  Repsol. It exits 9 (`order_outcome_unknown`) instead of retrying.
- **Run `make test lint`** before you finish.
- Code, comments, identifiers, commit messages and docs are in English.

## Layout

```
cmd/repsol/            entry point
internal/cli/          cobra commands, thin
internal/app/          use cases shared by the CLI and the MCP server
internal/repsolapi/    typed client for pidetubombona.repsol.es/api
internal/gigya/        Gigya client: login, signature refresh, session import
internal/config/       config.toml and credentials.toml, profiles, locking
internal/httpx/        shared transport: user agent, error mapping, redaction
internal/apperr/       typed errors and the exit-code mapping
internal/output/       JSON/pretty rendering and the error envelope
internal/mcpserver/    MCP tools and harness install
internal/fakerepsol/   in-process fake of the three backends
```
