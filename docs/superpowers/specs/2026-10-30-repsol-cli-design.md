# repsol-cli design

Date: 2026-10-30
Status: approved by the owner on 2026-10-30

Owner decisions so far: Go CLI; fully autonomous ordering with no confirmation;
pay on delivery only; session from a browser token, plus email + password login with
automatic re-login; verification from a HAR capture of a real web order; code and docs
in English; free to improve on wallapop-cli's structure.

## 1. Intent

A Go command-line tool that lets AI agents (Claude Code, Codex, Cursor and anything
that can run a shell command or speak MCP) order bottled gas from Repsol on the owner's
behalf, fully autonomously. The owner says "pídeme una bombona de butano y la pago al
repartidor" and the agent places the order without the owner opening the website.

Today the owner orders either by phone or through the web app at
`https://pidetubombona.repsol.es/`. Repsol publishes no API; the CLI talks to the same
private JSON API that web app uses, with the owner's own account.

The repository takes inspiration from `github.com/Microck/wallapop-cli` (noun-verb
commands, JSON output for agents, stable exit codes, MCP server, embedded agent docs,
fake-server tests, goreleaser), but the owner explicitly does not require copying its
structure: follow idiomatic Go and current best practices where they are better.
All code, comments, identifiers and repository docs are in English.

## 2. Goals

1. `repsol order create` places a real order for a bottle (butane by default), paid on
   delivery, with no confirmation prompt, and returns the order id and delivery date as
   JSON.
2. Authentication is a one-time import of the owner's web session (a token/cookie from
   the browser). After that, agents act without the owner.
3. Agents can also read: catalog and prices, the stored delivery address, available
   delivery dates, order history and the last order.
4. Agent-first ergonomics: machine-readable output, stable exit codes, a JSON error
   envelope, no prompts when not on a TTY, an MCP server, and embedded usage docs.
5. A clean, well-tested, releasable repository.

## 3. Non-goals (v1)

- Online card payment (Sipay payment wall) and Waylet payment. Only pay-on-delivery.
- Account registration, the "online register / policy" flows (`/order-policy`,
  `createFinishedOrderWithOnlineRegister`, `onlinePolicy: true`), installation review
  requests, promotions, vouchers, referral codes, tips, ratings. (`/solicitude` is in
  scope: it is how the owner's real pay-on-delivery order was placed.)
- Cancelling or modifying orders (no endpoint was found; the owner does that by phone).
- Background session renewal daemon, watches, notifications, SQLite state.
- Docs website, npm wrapper package.
- Ordering for other people or arbitrary third-party addresses.

## 4. Anti-goals (failure even if it "works")

- **Accidental duplicate orders.** An agent retrying a command must not send two
  deliveries. See the pending-order guard and no-retry rule in §7.
- **Secrets leaking.** The login token never appears in argv, logs, `--debug` output,
  error envelopes or MCP responses.
- **Hanging agents.** No interactive prompt ever blocks a non-TTY run.
- **Passwords in the wrong places.** The Repsol password is stored only when the owner
  opts in with `auth login --password-stdin` (see §6), only in the 0600 credentials
  file, and never in argv, logs, debug output, error envelopes or MCP responses.
- **Looking like Repsol.** No Repsol logo or branding; the README states the project is
  unofficial and unaffiliated.

## 5. Verified facts about the target (recon on 2026-10-30)

Sources: the web app's JavaScript bundles, public read-only probes, and a HAR capture
of a real order the owner placed on the web on 2026-10-30 (pay on delivery, butane
12.5 kg, delivered by a local agency). Items marked (HAR) were observed in that capture.

### Identity (Gigya)

- SAP Customer Data Cloud (Gigya), data center `eu1`, API key
  `3_kaQ1PkNv-0n4Nho4cD_Yz3-v6vbjqFJA6RgdpgTEBtw-2z6Rlnxe3IWDIDBtaETi`.
- Login (HAR): `POST https://accounts.eu1.gigya.com/accounts.login`, form-encoded:
  `loginID`, `password`, `APIKey`, `sessionExpiration=0`, `targetEnv=jssdk`,
  `include=profile,data,emails,subscriptions,preferences,`, `includeUserInfo=true`,
  `loginMode=standard`, `lang=es`, `sdk=js_latest`, `authMode=cookie`,
  `pageURL=https://pidetubombona.repsol.es/`, `format=json`, plus a browser
  fingerprint `riskContext`. Gigya always answers HTTP 200; success or failure is in
  the body (`errorCode` 0 on success; 403042 = wrong email or password).
  Success returns `UID`, `UIDSignature`, `signatureTimestamp`, `profile`, and
  `sessionInfo: {login_token: "st2.…" (≈236 chars), expires_in: "0"}`.
- Signature refresh (HAR): `POST https://accounts.eu1.gigya.com/accounts.getAccountInfo`,
  form-encoded: `APIKey`, `login_token`, `include=preferences, data, profile,`,
  `extraProfileFields=phones`, `lang`, `sdk=js_latest`, `authMode=cookie`,
  `pageURL`, `format=json`. Returns fresh `UID`, `UIDSignature`, `signatureTimestamp`.
  The only cookies sent to Gigya were device ids (`gmid`, `ucid`, `hasGmid`).
- In the browser the login token lives in the cookie
  `glt_3_kaQ1PkNv-0n4Nho4cD_Yz3-v6vbjqFJA6RgdpgTEBtw-2z6Rlnxe3IWDIDBtaETi` on
  `pidetubombona.repsol.es` (HAR).
- **A login token is bound to the Gigya device cookie `gmid`** (live, 2026-10-30).
  `accounts.getAccountInfo` with the token alone answers `errorCode 403005
  "Unauthorized user"`; adding `Cookie: gmid=<value>` makes it succeed. `ucid` and
  `hasGmid` are not needed, nor are `Origin`/`Referer` or any parameter besides
  `APIKey`, `login_token` and `format=json`. A session is therefore the pair
  (login token, gmid). The `gmid` cookie belongs to the Gigya domain, not to
  `pidetubombona.repsol.es`.
- An anonymous `GET https://accounts.eu1.gigya.com/accounts.webSdkBootstrap?apiKey=<key>&pageURL=https://pidetubombona.repsol.es/&sdk=js_latest&format=json`
  returns `errorCode 0` and sets fresh `gmid`, `ucid`, `hasGmid` cookies (live). In the
  HAR, `accounts.login` was sent with the browser's `gmid` cookie, so the CLI's password
  login bootstraps a `gmid` first and logs in with it.
- Authenticated API calls carry headers `uid`, `signature`, `signatureTimestamp`
  (from the Gigya response) and `ptb-gigya-user: <UID>` (HAR). The web refreshes the
  signature when older than 50 s.

### API (`https://pidetubombona.repsol.es/api`, JSON)

- Public (no Gigya headers): `/config`, `/health`, `/promotion`, `/agency`,
  `/catalog`, `/catalog/getCatalogIds`, `/deliverySchedule`, `/address-maps`, `/device`,
  `/localities`, `/provinces`, `/street_types`, `/streets`, `/payment`.
- The web also sends telemetry headers on every call: `ptb-order-process-id` (21-char
  id, constant for one order flow), `ptb-app-generated-user-id` (`us:` + 48 chars),
  `ptb-appinsights-request` (`rq:` + the path below `/api`, e.g. `rq:/cart/check:`,
  + 21 chars), `ptb-appinsights-session` (22 chars), `ptb-appinsights-user` (22
  chars); the random parts use the URL-safe alphabet `A-Za-z0-9_-` (HAR). Reads work
  without them (live).
- **User-Agent matters** (live): a request with Python's default User-Agent got HTTP 403
  from the front door; the same request with `User-Agent: repsol-cli/0.0.0-probe` got
  200. Every request the CLI sends carries `User-Agent: repsol-cli/<version>`; Go's
  default User-Agent is never used.
- Some failures come back as HTTP 200 with a JSON object `{"type": "...",
  "errorCode": "...", "error": true, "message": "..."}` (live, e.g.
  `EMPTY_ORDER_ERROR`). The client treats any object with `error: true` as an error.
- Catalog: `GET /catalog?agencyCode=X` (HAR, 18 entries for the owner's agency, some
  without `materialId`, which are packaging and not orderable). Known material ids:

  | materialId | type | price € (sample) |
  |---|---|---|
  | 153 | Butano 12.5kg | 18.89 |
  | 154 | Butano 12kg - Ligera | |
  | 239 | Propano 11kg | 18.89 |
  | 210 | Propano 35kg | 88.70 |
  | 230 | Autogas 12kg - Carretillas | 26.60 |
  | 409 | Butano 12.5kg Canarias | |

### Address resolution (HAR)

The web resolves a free-text address through an address service, not through the
province/locality/street lookups (those remain as a fallback in the bundle):

1. `GET https://pro-repsol.ed-integrations.com/v1/autocomplete?input=<text>&maxResults=5&neutralAddress=true&canarias=true&islandToExclude=elhierro,lagomera,lapalma,fuerteventura,lanzarote,lagraciosa`
   (the web also sends the browser's `location=lat,lng`; not needed). No API key, no
   auth, no `Origin` needed (live). → `{results: [{province, provinceCode, locality,
   localityCode, streetType, streetTypeCode, street, streetCode, municipality,
   municipalityCode, number, agencyCode, serviceCode, placeId, latitude, longitude,
   origin, neutral, fullAddress}]}`. `origin` is `"repsol"` (Repsol's street
   database, codes filled in) or `"google"` (codes partly empty).
2. The web's selection rule (bundle): a result with `origin != "google"` and
   `neutral == false` is used as-is. Otherwise, when it has `placeId` and `number`,
   it is replaced by `GET https://pro-repsol.ed-integrations.com/v1/geocode?placeId&number&canarias=true&islandToExclude=…`
   → the same fields plus `confidence: {number_not_found, agency_not_found,
   service_not_found, is_from_discarded_island, reference_policy_in_wrong_island}`,
   `currentFullAddress`, `currentStreet`. Results without a number make the web ask
   for one. (Neutral results go through a `/policies` lookup the CLI does not support:
   exit 2.)
3. `GET /api/agency?streetCode&localityCode&provinceCode&municipalityCode&origin=<ORIGIN uppercased>&inferredAgencyCode=<result agencyCode>&number`
   → `{agencyCode, onlinePayment, wayletPayment, allowsTip}`.

Steps 1–3 and `/deliverySchedule` were exercised live for one `repsol` and one
`google` public test address: all 200.

The stored address is the chosen result (geocoded when step 2 says so) plus `floor`,
`door`, `details`, `postalCode`, `isPeninsula`, `agencyCode` and `autocompleteContent`;
that whole object is what the order calls send as `address`. How the web fills the
extra fields (bundle, matches the HAR):

- `postalCode` is the first 5-digit run in `fullAddress`.
- `isPeninsula` comes from a helper that, as written, always returns `false`; the CLI
  sends `false` like the web does.
- For `origin: "google"` the web also calls `GET /api/address-maps?province&provinceCode&locality&streetType&streetTypeCode&street&municipality&postalCode`
  and merges the result into the address. In the HAR that response was empty, so the
  merge is best-effort.
- `autocompleteContent` (the chosen suggestion text) is sent too; it is display-only.
- An address is accepted only when `provinceCode` is present.

### Placing an order (HAR)

1. `GET /api/deliverySchedule?materialId&agencyCode&provinceCode&localityCode&streetTypeCode&municipalityCode&streetCode&number`
   → `[{materialId, dates: [{id: "1", date: "2026-11-05"}, …], mode: "NO_SCHEDULE",
   warning, type: "WEEKLY"}]`. `mode` can be absent (live); then `scheduleMode` is
   sent as `null`, as the web does.
2. `POST /api/cart/check` (authenticated) with `{scheduleMode, agencyCode,
   userGigyaUid, address, materialId, quantity, unitPrice, deliveryDateId,
   deliveryDate}` → `{status: "EE04", onlinePolicy: false, order: false,
   orderType: "SOLICITUDE"}`. This response decides which write follows.
3. For `orderType: "SOLICITUDE"` (the owner's case):
   `POST /api/solicitude` (authenticated) with `{address, materialId, quantity,
   unitPrice, status: <status from check>, bill: false, discount: 0, deliveryDateId,
   deliveryDate}` → `{externalId: "801…", userGigyaUid, deliveryDate, discount,
   address: {…summary}, agency: {name, phone, repsolPhone}, material, quantity,
   unitPrice}`. No payment method is sent: payment happens on delivery.
4. For `order: true` the bundle sends `POST /api/order` with `{address, materialId,
   quantity, unitPrice, bill, discount, deliveryDateId, deliveryDate}`; not observed
   in the HAR. `onlinePolicy: true` (policy/contract flows) is out of scope and must
   fail with exit 8 and a message to order on the web.
5. No `POST /cart` happens on the pay-on-delivery path.
6. Wire types (HAR, values withheld): `materialId` is a string (`"153"`) in
   `/deliverySchedule`, `/catalog`, `/order-history` and both request bodies;
   `deliveryDateId` is a string (`"1"`); `quantity` and `discount` are integers;
   `unitPrice` and catalog `price` are numbers; `latitude`/`longitude` are numbers after
   geocode but empty strings in `repsol` autocomplete results; every code
   (`provinceCode`, `localityCode`, `agencyCode`, …) is a string. Query parameters with
   empty values are left out (a `google` address sends no `streetCode`). Bodies are
   `Content-Type: application/json`. The sent `address` carries every result field
   plus the extras above, and `confidence`, `currentFullAddress`, `currentStreet` when
   it was geocoded.
7. Constants in the bundle: check statuses `EE01`–`EE04`, `EE06`–`EE08`; schedule
   modes `NO_SCHEDULE`, `NO_AGENCY` (the latter means no delivery to that address →
   exit 8); maximum quantity per order 24 for materials 210, 230, 239, 320, 620 and 9
   for everything else; provinces `35` and `38` are the Canary Islands (material 409).

### History (live, read-only, 2026-10-30)

- `GET /api/order-history?limit=N&offset=M&paramToAvoidCache=<random>` (authenticated)
  → a JSON array, newest first by `createdAt`. Each entry: `createdAt`
  (`2006-01-02T15:04:05.000-0700` layout), `deliveryDate` (`YYYY-MM-DD`),
  `referenceSAC` (8 digits; the only order reference in history), `orderNumber`
  (null), `materialId`, `quantity`, `unitPrice`, `discount`, `paymentMethod` (null for
  pay on delivery), `tip`, `bill`, `emptyBottles`, `devices`, `isPeninsula`,
  `agency: {name, phone, repsolPhone, agencyCode}`, `address: {province, provinceCode,
  locality, localityCode, streetType, street, streetTypeCode, streetCode,
  municipality, municipalityCode, number, floor, door, postalCode, details}`, and
  some always-null promo fields.
- **There is no status field.** Nothing says pending, delivered or cancelled.
- History addresses are partial: the solicitude placed in the HAR shows up with empty
  `street` and `number` and the text in `details`. They cannot rebuild the address
  object the order calls need. `agency.agencyCode` and `address.localityCode` are
  always present.
- The newest entry has no `externalId`; the id returned by `/solicitude`
  (`externalId`) and the history's `referenceSAC` are different identifiers.
- `GET /api/order-history/last-order` returned HTTP 200 with `EMPTY_ORDER_ERROR`
  although the account has 12 orders (all solicitudes). The CLI does not use it.

### Still unverified, checked first in implementation

1. Gigya `accounts.login` with email + password from Go, with a bootstrapped `gmid`
   and without the browser's `riskContext`. It was not tried during recon so that
   no login happened without the owner present; the owner runs the first
   `repsol auth login --email` themselves.
2. How long a login token stays valid (`expires_in: "0"`). With a stored password the
   CLI re-logs in (§6); without one, the owner re-imports.
3. Whether the writes (`/cart/check`, `/solicitude`, `/order`) need the `ptb-*`
   headers. The CLI sends them on writes, as the web does.
4. The `POST /order` branch (not observed; implement from the bundle, lenient parsing).

### Verification approach (owner's decision)

The owner exported the HAR of a real order to their Downloads folder. It contains the
plain-text password, a live login token and personal data, so it never enters the
repository: only redacted extracts become fixtures (redact `UID`, `UIDSignature`,
tokens, password, email, NIF, names, phones, street, number, floor, door, `placeId`,
`fullAddress`, `autocompleteContent`, coordinates, `externalId`, agency name and
phone). Once fixtures exist the owner deletes the HAR. Remaining items are checked
with read-only live calls using the owner's session. The CLI never places a test order
of its own; its first real order is a genuine one the owner wants.

During planning (2026-10-30) the HAR's session was used for read-only calls only:
`accounts.getAccountInfo` (8 calls, mostly to find the gmid binding), `/order-history`
(3 calls) and `/order-history/last-order` (1 call), plus anonymous `webSdkBootstrap`,
autocomplete, geocode, `/agency` and `/deliverySchedule` calls with public test
addresses. No login and no write was sent.

## 6. How it is used

### Commands

```
repsol auth      login | status | logout
repsol address   show | set
repsol catalog   list
repsol schedule  list
repsol order     create | list | last | show
repsol mcp       (serves MCP on stdio) | install claude-code|codex|cursor
repsol doctor
repsol skills    list | get
repsol config    path | get | set | list
repsol completion bash|zsh|fish|powershell
repsol version
```

Global flags: `--profile NAME`, `--format json|pretty`, `--no-input`, `--debug`,
`--error-format text|json`, `-h/--help`.

### Behaviour

- `auth login` supports two methods; the owner wants both:
  - **Session import** `auth login --har FILE | --token-stdin`. A session is the
    login token plus the `gmid` cookie (§5), which live on two different domains, so a
    plain cookie export of the Repsol site is not enough.
    - `--har FILE` reads a HAR exported from the browser after logging in on the web
      (the method the owner already used). It takes the newest Gigya request that
      carries a `login_token` (`accounts.getAccountInfo` form body) or returns one
      (`accounts.login` response `sessionInfo.login_token`), plus the `gmid` from that
      request's `Cookie` header. It reads nothing else from the file, never copies or
      stores it, and ignores the password the HAR may contain.
    - `--token-stdin` reads one line in cookie syntax with both values:
      `glt_<APIKey>=<token>; gmid=<gmid>` (`login_token=<token>` is accepted for the
      first). A TTY gets a no-echo prompt.
    (builder's call: replaces the `--cookies FILE` import of the first draft, which
    could not carry `gmid`.)
  - **Email + password** `auth login --email ADDR [--password-stdin]` (no-echo prompt
    on TTY): bootstraps a `gmid`, logs in through Gigya `accounts.login` with it,
    stores the token and `gmid`, and also stores the password so the CLI can log in
    again by itself.
  - Either way it validates by obtaining a signature once and records the account
    email and first name. Neither token nor password is ever read from argv. The
    address is not seeded from history, because history addresses are partial (§5);
    `auth login` ends with a hint to run `repsol address set` when no address is
    stored.
- **Automatic re-login.** When the stored token is rejected or expired and a password is
  stored, the CLI logs in again transparently, saves the new token and continues the
  command. Without a stored password it fails with exit 3 and a hint to re-import the
  token or log in with email. If Gigya demands a captcha or extra verification, the
  CLI fails with exit 3 and says so; it never tries to solve it.
- `auth status [--check]`: shows stored account metadata; `--check` proves the session
  still works.
- `auth logout`: deletes stored credentials for the profile.
- `address show`: the stored address.
- `address set "STREET NUMBER, TOWN" [--floor F] [--door D] [--details TEXT]
  [--pick N]`: resolves the text the way the web does (autocomplete → geocode when
  the web would → agency, §5). Only candidates with a street number count. Exactly
  one → stored. Several → exit 2 listing them (index, `fullAddress`, origin);
  `--pick N` (1-based) chooses one. None → exit 2 asking for the street number.
  Neutral candidates → exit 2 ("not supported, try a fuller address"). A
  `confidence` flag set to true (number, agency or service not found) → exit 8. The
  province/locality/street lookup chain is not used in v1 (builder's call).
- `catalog list`: products and prices, for the profile's agency when known.
- `schedule list [--product P]`: available delivery dates for the stored address.
- `order create [--product P] [--qty N] [--date YYYY-MM-DD] [--pay delivery]
  [--dry-run] [--force]`:
  - Defaults: `--product butano`, `--qty 1`, `--pay delivery`, earliest date.
  - Product aliases map to material ids (`butano`→153, `butano-ligera`→154,
    `propano`→239, `propano-35`→210, `autogas`→230, `butano-canarias`→409);
    `--product` also accepts a numeric material id.
  - `--qty` is capped at the web's per-material maximum (9, or 24 for the larger
    bottles); above it → exit 2.
  - Steps: load address → resolve agency → price from catalog for that agency → pick
    date from `/deliverySchedule` → pending-order guard → `POST /cart/check` → write
    with `POST /solicitude` (when `orderType` is `SOLICITUDE`) or `POST /order` (when
    `order` is true), sending the `status` from the check → print result. Any other
    check outcome (`onlinePolicy: true`, unknown `orderType`) → exit 8 with a hint to
    order on the web, and nothing is written.
  - `--dry-run` runs every read and prints the planned order without writing anything.
  - No confirmation prompt, on TTY or not. This is the owner's decision.
  - Output: order id (`externalId`), product, quantity, unit price, total, delivery
    date, address summary, agency name and phone, payment method `on_delivery`.
- `order list [--limit N]` (default 10), `order last` (newest history entry; exit 4
  when there is none), `order show REFERENCE` (the 8-digit `referenceSAC` from
  `order list`; searched in the newest 100 entries, exit 4 if absent). Every history
  entry is printed with `pending: true|false` (§7).
- `doctor`: config parses, credentials present, signature obtainable, `/health` ok,
  address set and its agency still resolvable. Exit 1 if any check fails.
- `skills list | get NAME`: embedded agent-facing usage docs (what to run, output
  shapes, exit codes).
- `mcp`: MCP server over stdio. Tools: `catalog_list`, `address_show`, `schedule_list`,
  `order_create`, `order_list`, `order_last`. `order_create` is exposed (fully autonomous,
  by the owner's decision). Tool results are the same JSON the CLI prints.
- `mcp install claude-code|codex|cursor`: writes the server entry into that harness's
  config, preserving other entries; running twice is a no-op that says so.

## 7. Safety rules

- **Pending-order guard.** Before writing, `order create` reads the newest 20 history
  entries; if one is pending for the same delivery address (any product), it refuses
  with exit 8 unless `--force`, and the error names that entry's reference and date.
  This is the only brake, and it exists to stop accidental duplicates, not to ask
  permission. Because history has no status and only partial addresses (§5)
  (builder's calls):
  - *pending* means `deliveryDate` is today or later in `Europe/Madrid`;
  - *same address* means equal `agency.agencyCode` and `address.localityCode`.
  A delivery cancelled by phone still looks pending until its date passes; the agent
  docs say so, and `--force` covers it when the owner asks. Scoping by address means a
  pending delivery at one profile's home never blocks an order for another town on the
  same account.
- **`--force` is for explicit owner intent only** (builder's call). The CLI accepts it;
  the MCP `order_create` tool does not expose it; the embedded agent docs tell agents to
  pass `--force` only when the owner explicitly asks for another delivery while one is
  pending, and otherwise to report the pending order back.
- **No automatic retry of the write.** If the final order request times out or fails
  ambiguously, the CLI does not retry. It exits with a dedicated "outcome unknown" code
  and tells the caller to run `repsol order last` before trying again.
- Reads may retry with backoff on network errors and 5xx.
- `--debug` logs requests and responses with the token, signature and personal data
  redacted.

## 8. Output and errors

- stdout carries data only; stderr carries errors, prompts and debug logs.
- `--format`: defaults to `pretty` when stdout is a TTY and `json` otherwise, so agents
  always get JSON (builder's call: an explicit flag always wins).
- JSON error envelope on stderr with `--error-format json` (or `REPSOL_ERROR_FORMAT`):
  `{code, category, retryable, message, http_status, endpoint, hint}`.
- Exit codes:

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
  | 8 | refused: pending order exists, no delivery to this address, product unavailable |
  | 9 | order outcome unknown: check `repsol order last` before retrying |
  | 130 | interrupted |

## 9. Configuration and storage

- Paths via XDG (`adrg/xdg`), app dir `repsol-cli`:
  - `config.toml` (0644): default profile, per-profile address and agency.
  - `credentials.toml` (0600, atomic writes, file lock for concurrent agent runs):
    per-profile login token and `gmid`, optional email + password (owner opted in),
    and account metadata. Storing in a 0600 file rather than the OS keychain is the builder's
    choice, so headless agents never hit a keychain prompt.
- Profiles exist (e.g. a second home), selected by `--profile` > `REPSOL_PROFILE` >
  config default. The first login creates `default`.
- Env: `REPSOL_PROFILE`, `REPSOL_LOGIN_TOKEN` + `REPSOL_GMID` (non-persistent session
  override, for CI or sandboxed agents), `REPSOL_EMAIL` + `REPSOL_PASSWORD`
  (non-persistent override), `REPSOL_ERROR_FORMAT`, `REPSOL_API_BASE_URL`,
  `REPSOL_GIGYA_BASE_URL` and `REPSOL_ADDRESS_BASE_URL` (tests), `NO_COLOR`.
  Precedence: flags > env > config.

## 10. Engineering decisions (builder's calls, stated here so the owner can object)

- Module `github.com/felipe9962/repsol-cli`, binary `repsol`, Go 1.26, MIT license.
- Layout:

  ```
  cmd/repsol/            main: wires dependencies, maps errors to exit codes
  internal/cli/          cobra commands, one file per noun, thin
  internal/app/          use cases shared by CLI and MCP (place order, resolve address)
  internal/repsolapi/    typed client for pidetubombona.repsol.es/api
  internal/gigya/        Gigya REST client: token → UID signature, cached
  internal/config/       config + credentials files, XDG paths, profiles
  internal/output/       json/pretty renderers, error envelope, exit codes
  internal/mcpserver/    MCP tools over internal/app, harness install
  internal/fakerepsol/   httptest fake of the API and Gigya for tests
  internal/cli/skills/   embedded agent docs (go:embed)
  docs/                  cli-spec.md, adr/
  ```

  Use cases live in `internal/app` so CLI and MCP share one code path.
- Libraries: `spf13/cobra`, `pelletier/go-toml/v2`, `adrg/xdg`, the official MCP Go SDK
  (`github.com/modelcontextprotocol/go-sdk`), stdlib `net/http`, `log/slog`. No other
  frameworks.
- `context.Context` through every call; Ctrl-C cancels in-flight requests.
- Errors: typed errors in `repsolapi`/`gigya`/`app`, mapped to exit codes in one place.
- Tests: stdlib `testing`, `httptest` fake server with redacted fixtures, CLI-level
  tests with an isolated XDG home (`rogpeppe/go-internal/testscript` is a good fit).
  No live network in `go test`; an opt-in `-tags live` suite for read-only checks.
- Tooling: `golangci-lint`, `go vet`, Makefile (`build`, `test`, `lint`), GitHub Actions
  (CI on push/PR; release on tag), goreleaser (darwin/linux/windows, amd64/arm64,
  checksums).
- Repo docs: `README.md` (install, login walkthrough, agent setup, unofficial
  disclaimer), `AGENTS.md` (domain vocabulary and contributor rules for coding agents),
  `docs/cli-spec.md`, ADRs (`0001` sessions: Gigya token import plus opt-in stored
  password with automatic re-login, `0002` autonomous ordering with pending-order
  guard), issue template for "API changed".

## 11. Left to the builder

- Which of the web's Gigya parameters and `ptb-*` headers are really needed (§5
  "still unverified"); start by sending what the web sends.
- Pretty-output formatting.
- Product alias names beyond the ones listed.
- Fixture recording and redaction approach.
- Whether to re-resolve the agency on every order or cache it with the address.
