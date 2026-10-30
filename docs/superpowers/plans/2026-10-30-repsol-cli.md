# repsol-cli Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `repsol`, a Go CLI and MCP server that lets AI agents order bottled gas from
Repsol's pidetubombona web API on the owner's behalf. Orders are paid on delivery and placed
without a confirmation prompt.

**Architecture:** Two front ends call one use-case layer, `internal/app`: thin cobra
commands (`internal/cli`) and MCP tools (`internal/mcpserver`). `app` drives typed clients
for three backends: Gigya (`internal/gigya`), and the Repsol API and address service
(`internal/repsolapi`). All three share one transport, `internal/httpx`. It sets the
User-Agent, maps HTTP failures to the typed errors in `internal/apperr`, retries reads and
redacts debug logs. State lives in two TOML files (`internal/config`). Tests run against an
in-process fake of all three backends (`internal/fakerepsol`).

**Tech Stack:** Go 1.26; spf13/cobra v1.10.2; pelletier/go-toml/v2 v2.4.3;
adrg/xdg v0.5.3; gofrs/flock v0.13.1; golang.org/x/term v0.47.0;
modelcontextprotocol/go-sdk v1.8.0; rogpeppe/go-internal v1.16.0 (testscript, tests only);
stdlib net/http and log/slog.

**Spec:** `docs/superpowers/specs/2026-10-30-repsol-cli-design.md`. Read it first: §5 holds
every wire fact this plan relies on.

## Global Constraints

- Module `github.com/felipe9962/repsol-cli`, binary `repsol`, `go 1.26`, MIT license.
- Dependencies: only the Tech Stack list above, at those exact versions. No other
  third-party module.
- All code, comments, identifiers, docs and commit messages in English.
- Every outgoing request carries `User-Agent: repsol-cli/<version>`. Go's default
  User-Agent is never sent. Tests use `repsol-cli/test`.
- The login token, `gmid`, password, `UIDSignature`, UID and personal data never appear in
  argv, log lines, `--debug` output, error messages, the error envelope or MCP responses.
- `go test ./...` makes no live network call. The live suite runs only with `-tags live` and
  is read-only.
- Never place a real order while developing. The owner's HAR
  (`~/Downloads/pidetubombona.repsol.es.har`) never enters the repository or a fixture. All
  fixtures are synthetic, built from the key sets and types in spec §5.
- stdout carries data only. stderr carries errors, hints, prompts and debug logs.
- Exit codes: 0 ok, 1 generic, 2 usage, 3 auth, 4 not found, 5 blocked, 6 network,
  7 API changed, 8 refused, 9 order outcome unknown, 130 interrupted.
- CLI output JSON keys are `snake_case`. Wire structs use the API's `camelCase` names in
  both `json` and `toml` tags.
- Gigya API key `3_kaQ1PkNv-0n4Nho4cD_Yz3-v6vbjqFJA6RgdpgTEBtw-2z6Rlnxe3IWDIDBtaETi`.
- Default base URLs:
  - Gigya: `https://accounts.eu1.gigya.com`.
  - Repsol API: `https://pidetubombona.repsol.es/api`.
  - Address service: `https://pro-repsol.ed-integrations.com`.
  - Overridable with `REPSOL_GIGYA_BASE_URL`, `REPSOL_API_BASE_URL`,
    `REPSOL_ADDRESS_BASE_URL`.
- Commit at the end of every task with a Conventional Commits message.

## Review Focus

1. **Day boundary in Europe/Madrid.** At 00:30 in Madrid (23:30 UTC the day before), an
   order for "today" counts as pending and yesterday's does not. Test:
   `TestPendingGuardUsesMadridDate` in Task 10.
2. **Secrets in `--debug` output.** Debug logs never show any of these:
   - the login token, password or `gmid`;
   - the signature or UID;
   - Gigya's `data` block (it holds the NIF) or address fields.

   Readers see `REDACTED` instead. Test: `TestDebugLogRedactsSecrets` in Task 2.
3. **A write that hangs, fails or is interrupted.** A timeout, reset, 5xx, garbled body or
   Ctrl-C during `/solicitude` or `/order` exits 9 after exactly one request. It never
   exits 6 or 130, and it never sends a second POST. Tests: `TestWriteOutcomeUnknown` in
   Task 5 and `TestFinalErrorKeepsOutcomeUnknown` in Task 11.
4. **Pasted values containing `=` and CRLF.** A `gmid=…==` value, a token with `=`, or a
   password piped from a Windows tool parses exactly: trailing `\r\n` is stripped and
   inner `=` is kept. Tests: `TestParseSessionLine` in Task 3 and
   `TestReadSecretLineTrimsCRLF` in Task 11.
5. **Two agents at once.** Concurrent `repsol` processes saving a refreshed token never
   lose another profile's entry or leave `credentials.toml` unreadable or wider than
   0600. Test: `TestConcurrentCredentialUpdates` in Task 6.

## Decisions this plan adds to the spec (builder's calls)

- **Two extra packages.** `internal/apperr` (typed errors with exit codes) and
  `internal/httpx` (shared transport) join the spec's layout.
- **Error envelope.** It also carries `exit_code`, because MCP callers get no process
  exit status. `code` is a snake_case machine code such as `pending_order`. On stderr the
  envelope is printed as one compact JSON line.
- **HTTP status mapping.**
  - Reads: 401→3, 403→5, 404→4, 429→5 (retryable), 5xx→6 (retryable), any other
    non-2xx→7.
  - A 2xx body with `"error": true` → 8. An unparseable 2xx → 7.
  - Final write (`/solicitude`, `/order`): a transport error, timeout, cancellation, 5xx
    or unparseable 2xx → 9.
  - Only GETs and Gigya `getAccountInfo`/`webSdkBootstrap` are retried: 3 attempts,
    with backoff of 250 ms, then 1 s.
- **Headers.** Authenticated calls send `uid`, `signature`, `signatureTimestamp` and
  `ptb-gigya-user`. The five `ptb-*` telemetry headers go only on `/cart/check`,
  `/solicitude` and `/order`.
- **Agency lookup.** `catalog list`, `schedule list` and `order create` re-resolve the
  agency through `/api/agency` each time. The stored `agencyCode` is informational.
- **Address prerequisite.** `catalog list` and `schedule list` need a stored address and
  exit 2 without one.
- **Dry run.** `order create --dry-run` also sends `POST /cart/check`. That call is a
  validation and places nothing, so a dry run can report whether the order would go
  through.
- **Canary Islands default.** The default product `butano` means material 409 when the
  stored address is in province 35 or 38.
- **Password storage.** `auth login --email` always stores the password; choosing that
  method is the opt-in. `auth status --check` exits 3 when the session does not work.
- **Session import.** It keeps a stored password only when the imported account's email
  matches it.
- **Wiring.** Dependency wiring and the error-to-exit-code mapping live in `cli.Main`
  rather than in `cmd/repsol` (spec §10), so testscript can run the real binary entry
  point in-process. `cmd/repsol/main.go` only calls `os.Exit(cli.Main())`.
- **No colours.** Pretty output uses no ANSI colours, so `NO_COLOR` holds without any
  code.
- **Malformed dates.** A `--date` that is not `YYYY-MM-DD` exits 2. A well-formed date
  that is not offered exits 8.

## File map

```
cmd/repsol/main.go              os.Exit(cli.Main())
internal/apperr/                Category (= exit code), *Error, From, ExitCode
internal/httpx/                 Client.Do: UA, status→error mapping, retries, redacted debug log
internal/gigya/                 AccountInfo, Bootstrap, Login, SessionFromHAR, ParseSessionLine
internal/repsolapi/             Address & wire types, PlacesClient, Client (reads, history, writes)
internal/config/                Paths, Store: config.toml + credentials.toml (lock, atomic, 0600)
internal/fakerepsol/            httptest fake of Gigya + API + address service
internal/app/                   Session (signing, re-login), auth, address, catalog, schedule,
                                orders, history, doctor, pretty printers
internal/output/                format resolution, Render, error envelope
internal/cli/                   cobra commands, Main/Run, prompts, testscript suite
internal/cli/skills/repsol/     SKILL.md (embedded agent docs)
internal/mcpserver/             MCP tools over app, harness install
internal/live/                  -tags live read-only checks
docs/                           cli-spec.md, adr/0001, adr/0002
```

---

### Task 1: Repository scaffold and typed errors

**Files:**
- Create: `go.mod`, `.gitignore`, `LICENSE`, `Makefile`
- Create: `internal/apperr/apperr.go`, `internal/apperr/apperr_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Category int` with constants whose values are the exit codes: `Generic=1`,
    `Usage=2`, `Auth=3`, `NotFound=4`, `Blocked=5`, `Network=6`, `APIChanged=7`,
    `Refused=8`, `OutcomeUnknown=9`, `Interrupted=130`.
  - `func (c Category) String() string`.
  - `type Error struct { Category Category; Code, Message, Hint string; HTTPStatus int;
    Endpoint string; Retryable bool; Err error }`, with methods `Error() string`,
    `Unwrap() error`, `WithHint(h string) *Error` and `WithCause(err error) *Error`.
  - `func New(cat Category, code, msg string) *Error`.
  - `func From(err error) *Error`: never nil for a non-nil error.
  - `func ExitCode(err error) int`.

- [x] **Step 1: Initialise the repository and commit the docs on `main`**

```bash
# from the repository root
git init -b main
go mod init github.com/felipe9962/repsol-cli && go mod edit -go=1.26
```

Write these files:
- `.gitignore`: `/bin/`, `/dist/`, `*.har`, `coverage.out`.
- `LICENSE`: MIT, `Copyright (c) 2026 felipe9962`.
- `Makefile`, with `VERSION ?= dev` and these targets:
  - `build`: `go build -trimpath -ldflags "-X github.com/felipe9962/repsol-cli/internal/cli.Version=$(VERSION)" -o bin/repsol ./cmd/repsol`
  - `test`: `go test -race ./...`
  - `lint`: `go vet ./...`, then `golangci-lint run` when it is on PATH.
  - `live`: `go test -tags live -run Live -v ./internal/live/`

```bash
git add docs .gitignore LICENSE Makefile go.mod
git commit -m "docs: add design spec and implementation plan"
git switch -c feat/v1
```

- [x] **Step 2: Write the failing tests**

```go
func TestExitCode(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 0},
		{errors.New("boom"), 1},
		{New(Usage, "bad_flag", "x"), 2},
		{fmt.Errorf("wrap: %w", New(Auth, "session_expired", "x")), 3},
		{New(OutcomeUnknown, "order_outcome_unknown", "x"), 9},
		{context.Canceled, 130},
		{context.DeadlineExceeded, 6},
	}
	for _, c := range cases {
		if got := ExitCode(c.err); got != c.want {
			t.Errorf("ExitCode(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestCategoryNames(t *testing.T) {
	want := map[Category]string{Generic: "generic", Usage: "usage", Auth: "auth",
		NotFound: "not_found", Blocked: "blocked", Network: "network",
		APIChanged: "api_changed", Refused: "refused",
		OutcomeUnknown: "outcome_unknown", Interrupted: "interrupted"}
	for c, s := range want {
		if c.String() != s {
			t.Errorf("%d.String() = %q, want %q", c, c.String(), s)
		}
	}
}

func TestFromKeepsFields(t *testing.T) {
	e := New(Refused, "pending_order", "order 20000001 is pending").WithHint("pass --force")
	got := From(fmt.Errorf("ctx: %w", e))
	if got.Code != "pending_order" || got.Hint != "pass --force" ||
		!strings.Contains(got.Error(), "order 20000001 is pending") {
		t.Fatalf("From lost fields: %+v", got)
	}
	if d := From(context.DeadlineExceeded); d.Code != "timeout" || !d.Retryable {
		t.Fatalf("deadline: %+v", d)
	}
}
```

- [x] **Step 3: Run the tests and confirm they fail**

Run: `go test ./internal/apperr/`
Expected: FAIL with `undefined: ExitCode`.

- [x] **Step 4: Implement `internal/apperr/apperr.go`**

`Error()` returns `Message`, followed by `": " + Err.Error()` when `Err` is set. `From`
maps errors as follows:
- `errors.As` finds a `*Error`: return that.
- `context.Canceled` → `Interrupted`, code `interrupted`.
- `context.DeadlineExceeded` → `Network`, code `timeout`, retryable.
- Anything else → `Generic`, code `error`, with `Message: err.Error()`.

`ExitCode(nil)` is 0; otherwise it is `int(From(err).Category)`.

- [x] **Step 5: Run the tests and confirm they pass**

Run: `go test ./internal/apperr/`
Expected: `ok`.

- [x] **Step 6: Commit**

```bash
git add internal/apperr go.mod
git commit -m "feat(apperr): add typed errors with stable exit codes"
```

---

### Task 2: Shared HTTP transport with redacted debug logging

**Files:**
- Create: `internal/httpx/client.go`, `internal/httpx/redact.go`
- Test: `internal/httpx/client_test.go`, `internal/httpx/redact_test.go`

**Interfaces:**
- Consumes: `apperr.New`, `apperr.Category`.
- Produces:
  - `type Client struct { HTTP *http.Client; UserAgent string; Logger *slog.Logger;
    Attempts int; Backoff func(attempt int) time.Duration }`
  - `func New(userAgent string, logger *slog.Logger) *Client`: `HTTP.Timeout` 30 s,
    `Attempts` 3, `Backoff(n)` = 250 ms·4^(n-1). A nil logger means no debug log.
  - `type Request struct { Method, URL string; Header http.Header; Body []byte;
    Retry bool; Endpoint string }`: `Endpoint` defaults to the URL path. It is the only
    location that appears in errors.
  - `type Response struct { Status int; Header http.Header; Body []byte }`
  - `func (c *Client) Do(ctx context.Context, r Request) (*Response, error)`: returns an
    `*apperr.Error` for any non-2xx status, or `ctx.Err()` when the context is done.
  - `func RedactURL(u *url.URL) string`, `func RedactHeader(h http.Header) map[string]string`
    and `func RedactBody(b []byte) string`.

- [x] **Step 1: Write the failing tests**

```go
func TestDoSendsUserAgentAndAccept(t *testing.T) {
	// The server records the request. Assert:
	//   r.UserAgent() == "repsol-cli/test"
	//   r.Header.Get("Accept") == "application/json"
	//   a POST with Body and no Content-Type gets "Content-Type: application/json"
	//   an explicit Content-Type in Request.Header is kept as is
}

func TestDoMapsStatus(t *testing.T) {
	cases := []struct {
		status    int
		cat       apperr.Category
		retryable bool
	}{
		{401, apperr.Auth, false}, {403, apperr.Blocked, false}, {404, apperr.NotFound, false},
		{429, apperr.Blocked, true}, {500, apperr.Network, true}, {503, apperr.Network, true},
		{418, apperr.APIChanged, false},
	}
	// For each case: Do(Request{Method: "GET", URL: srv.URL + "/thing"}).
	// apperr.From(err) must have Category == cat, Retryable == retryable,
	// HTTPStatus == status and Endpoint == "/thing".
}

func TestDoRetriesOnlyWhenAllowed(t *testing.T) {
	// The server answers 500, 500, 200 in sequence. c.Backoff = func(int) time.Duration { return 0 }.
	// With Retry: true, there is no error and the server sees 3 calls.
	// With a fresh server and Retry: false, the error category is Network and the server sees 1 call.
}

func TestTransportErrorHidesQuery(t *testing.T) {
	// Start a server, then close it. Request URL: srv.URL + "/x?login_token=SECRET".
	// apperr.From(err).Category == apperr.Network, Retryable == true,
	// and !strings.Contains(err.Error(), "SECRET").
}

func TestDebugLogRedactsSecrets(t *testing.T) { // Review Focus 2
	var buf bytes.Buffer
	c := New("repsol-cli/test", slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	// The server responds:
	// {"UID":"uid-SECRET","UIDSignature":"sig-SECRET","profile":{"email":"me@example.com"},
	//  "data":{"DocID":{"Pers":"12345678Z"}},"sessionInfo":{"login_token":"st2.SECRET"},
	//  "address":{"street":"Calle SECRET","number":"7"}}
	_, _ = c.Do(context.Background(), Request{Method: "POST",
		URL:    srv.URL + "/accounts.login?apiKey=k&input=Calle%20SECRET",
		Header: http.Header{"Cookie": {"gmid=gmid-SECRET"}, "Signature": {"sig-SECRET"}, "Uid": {"uid-SECRET"}},
		Body:   []byte("loginID=me%40example.com&password=pw-SECRET&login_token=st2.SECRET&APIKey=k")})
	out := buf.String()
	for _, s := range []string{"SECRET", "me@example.com", "me%40example.com", "12345678Z"} {
		if strings.Contains(out, s) {
			t.Fatalf("debug log leaks %q:\n%s", s, out)
		}
	}
	if !strings.Contains(out, "REDACTED") || !strings.Contains(out, "/accounts.login") {
		t.Fatalf("debug log missing redaction marker or path:\n%s", out)
	}
}
```

- [x] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/httpx/`
Expected: FAIL with `undefined: New`.

- [x] **Step 3: Implement `Client.Do` and the redactors**

**`Do`**
- Every attempt builds a fresh `*http.Request` from `bytes.NewReader(Body)`.
- It sets `User-Agent`. It sets `Accept: application/json` unless the caller set one.
- When there is a body and no `Content-Type`, it sets `Content-Type: application/json`.
- It reads at most 8 MiB of the response.

**Retries**
- Only when `r.Retry` is true.
- Only for retryable errors (5xx, 429, transport errors).
- At most `Attempts` attempts in total.
- The backoff sleep honours `ctx`.

**Errors**
- A done context returns `ctx.Err()` unwrapped.
- Any other transport error becomes `apperr.New(Network, code, msg)` with retryable set,
  where:
  - `code` is `"timeout"` when the error reports `Timeout()`, else `"network_error"`;
  - `msg` is `"<METHOD> <host><Endpoint>: <inner error>"`;
  - the inner error is taken from `(*url.Error).Err`, so the full URL never appears.
- Status mapping follows the table in the test. Codes: `unauthorized`, `forbidden`,
  `not_found`, `rate_limited`, `server_error`, `unexpected_status`.
- The 403 error gets this hint: `Repsol's firewall or session refused the request; run
  "repsol auth status --check"`.

**Debug logging**
- Only when `Logger != nil`.
- One `Debug("http", …)` record per attempt, with these attributes: `method`, `url`
  (`RedactURL`), `status`, `duration_ms`, `request_headers` (`RedactHeader`),
  `request_body` (`RedactBody`), and `response_body` (`RedactBody`, cut to 4096 chars).

**Redaction**
- Matching is case-insensitive. The whole value of a sensitive key is replaced with
  `"REDACTED"`, including object and array values.
- The sensitive keys, the same set for query keys, header names, JSON keys and form keys:
  `login_token loginid password uid uidsignature signature signaturetimestamp
  usergigyauid ptb-gigya-user cookie set-cookie gmid email firstname lastname profile
  data phones phone name nif street number floor door details placeid fulladdress
  autocompletecontent currentfulladdress currentstreet latitude longitude location input
  externalid referencesac postalcode riskcontext sessioninfo`.
- `RedactBody` tries JSON first, then `url.ParseQuery` with at least one key. If neither
  parses, it returns `"<N bytes>"`.

- [x] **Step 4: Run the tests and confirm they pass**

Run: `go test -race ./internal/httpx/`
Expected: `ok`.

- [x] **Step 5: Commit**

```bash
git add internal/httpx
git commit -m "feat(httpx): add transport with UA, error mapping, read retries and redacted debug log"
```

---

### Task 3: Gigya client and session import parsing

**Files:**
- Create: `internal/gigya/client.go`, `internal/gigya/errors.go`, `internal/gigya/session.go`
- Test: `internal/gigya/client_test.go`, `internal/gigya/session_test.go`
- Test data (synthetic): `internal/gigya/testdata/session.har`, `no_gmid.har`, `no_session.har`

**Interfaces:**
- Consumes: `httpx.Client`, `httpx.Request`, `apperr`.
- Produces:
  - `const APIKey`, `DefaultBaseURL = "https://accounts.eu1.gigya.com"` and
    `PageURL = "https://pidetubombona.repsol.es/"`.
  - `const CodeSessionRejected = "session_rejected"`,
    `CodeInvalidCredentials = "invalid_credentials"`,
    `CodeCaptchaRequired = "captcha_required"` and
    `CodeAccountLocked = "account_locked"`.
  - `type Session struct { LoginToken, GMID string }`
  - `type Signature struct { UID, UIDSignature, Timestamp string; ObtainedAt time.Time }`
  - `type Account struct { UID, Email, FirstName string }`
  - `type Client struct { BaseURL, APIKey string; HTTP *httpx.Client; Now func() time.Time }`
  - `func (c *Client) AccountInfo(ctx context.Context, s Session) (Signature, Account, error)`
  - `func (c *Client) Bootstrap(ctx context.Context) (gmid string, err error)`
  - `func (c *Client) Login(ctx context.Context, gmid, email, password string) (Session, Signature, Account, error)`
  - `func SessionFromHAR(r io.Reader) (Session, error)`
  - `func ParseSessionLine(line string) (Session, error)`

- [x] **Step 1: Write the failing tests**

```go
func TestAccountInfoSendsGMIDCookie(t *testing.T) {
	// The server asserts:
	//   path "/accounts.getAccountInfo", method POST
	//   form APIKey==APIKey, login_token=="tok", format=="json"
	//   cookie gmid=="g1"
	// It responds:
	// {"errorCode":0,"UID":"u1","UIDSignature":"s1","signatureTimestamp":"1700000000",
	//  "profile":{"email":"owner@example.com","firstName":"Owner"}}
	sig, acct, err := c.AccountInfo(ctx, Session{LoginToken: "tok", GMID: "g1"})
	// want sig == Signature{"u1", "s1", "1700000000", now}
	// want acct == Account{"u1", "owner@example.com", "Owner"}
}

func TestAccountInfoErrors(t *testing.T) {
	// errorCode → category, code:
	//   403005 → Auth, CodeSessionRejected
	//   500001 → Network, retryable
	//   400006 → APIChanged
}

func TestBootstrapReadsGMIDCookie(t *testing.T) {
	// The server asserts GET /accounts.webSdkBootstrap with query
	//   apiKey=APIKey, pageURL=PageURL, sdk=js_latest, format=json.
	// It responds with headers "Set-Cookie: gmid=boot-gmid; Path=/" and "Set-Cookie: ucid=x",
	// body {"errorCode":0}.
	// want "boot-gmid". With no gmid cookie in the response: APIChanged.
}

func TestLoginForm(t *testing.T) {
	// The server asserts form values:
	//   loginID, password,
	//   APIKey, sessionExpiration=0, targetEnv=jssdk,
	//   include="profile,data,emails,subscriptions,preferences,",
	//   includeUserInfo=true, loginMode=standard, lang=es, sdk=js_latest, authMode=cookie,
	//   pageURL=PageURL, format=json
	// and cookie gmid=g1.
	// It responds with errorCode 0, UID/UIDSignature/signatureTimestamp, profile, and
	// "sessionInfo":{"login_token":"st2.new","expires_in":"0"}.
	// want Session{"st2.new", "g1"}, and the signature and account parsed from the same body.
}

func TestLoginErrors(t *testing.T) {
	// errorCode → code (all Auth unless noted):
	//   403042 → CodeInvalidCredentials
	//   401020 → CodeCaptchaRequired
	//   401021 → CodeCaptchaRequired
	//   403120 → CodeAccountLocked
	// errorCode 0 without sessionInfo.login_token → APIChanged.
}

func TestLoginIsNotRetried(t *testing.T) { /* HTTP 503 → the server sees 1 call */ }

func TestSessionFromHAR(t *testing.T) {
	s, err := SessionFromHAR(open(t, "testdata/session.har"))
	// want Session{LoginToken: "st2.newest", GMID: "gmid-newest"}, err == nil
}

func TestSessionFromHARErrors(t *testing.T) {
	// testdata/no_gmid.har    → Usage, code "har_no_gmid", hint contains "with sensitive data"
	// testdata/no_session.har → Usage, code "har_no_session"
	// strings.NewReader("not json") → Usage, code "har_invalid"
}

func TestParseSessionLine(t *testing.T) { // Review Focus 4
	ok := []struct {
		in   string
		want Session
	}{
		{"glt_" + APIKey + "=st2.a=b==; gmid=gmid.ver4.x==\r\n", Session{"st2.a=b==", "gmid.ver4.x=="}},
		{"  login_token=t1 ;gmid=g1 ", Session{"t1", "g1"}},
		{"gmid=g1; other=1; login_token=t1", Session{"t1", "g1"}},
	}
	bad := []string{"login_token=t1", "gmid=g1", ""} // each → Usage, code "invalid_session_line"
}
```

`testdata/session.har` is a minimal synthetic HAR (`{"log":{"entries":[…]}}`) with three
entries, in this order:
1. `GET https://pidetubombona.repsol.es/api/config`.
2. `POST https://accounts.eu1.gigya.com/accounts.getAccountInfo`, with
   `postData.params` containing `login_token=st2.older` and request `cookies`
   `[{"name":"gmid","value":"gmid-older"}]`.
3. `POST https://accounts.eu1.gigya.com/accounts.login`, with
   `postData.text` `loginID=x%40example.com&password=hunter2`, request header
   `Cookie: gmid=gmid-newest; ucid=u`, and `response.content.text`
   `{"errorCode":0,"sessionInfo":{"login_token":"st2.newest","expires_in":"0"}}`.

`no_gmid.har` is entry 2 without cookies. `no_session.har` is entry 1 only.

- [x] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/gigya/`
Expected: FAIL with `undefined: Client`.

- [x] **Step 3: Implement the client, error classification and session parsing**

**Requests**
- `AccountInfo`: POST form, `Content-Type: application/x-www-form-urlencoded`. Sends
  only `APIKey`, `login_token` and `format=json` (verified live), plus `Cookie:
  gmid=<GMID>`. `Retry: true`.
- `Bootstrap`: GET. Reads the cookies from the `Set-Cookie` headers with
  `(&http.Response{Header: h}).Cookies()`. `Retry: true`.
- `Login`: sends the exact form from the test with `Retry: false`. It sends no
  `riskContext` (spec §5 "still unverified" item 1).
- Decode `signatureTimestamp` from either a string or a number.

**Error classification** (`errors.go`), on a non-zero `errorCode`, checked in this order:
1. 403042 → `CodeInvalidCredentials`, message `Repsol rejected the email or password`.
2. 401020 or 401021 → `CodeCaptchaRequired`, message `Repsol asks for a captcha or
   extra verification`, hint `log in on the web and run "repsol auth login --har FILE"`.
3. 403120 → `CodeAccountLocked`.
4. 5xxxxx → `Network`, code `gigya_unavailable`, retryable.
5. 400xxx → `APIChanged`, code `gigya_bad_request`.
6. Other 401xxx or 403xxx → `Auth`: `CodeSessionRejected` for `AccountInfo`,
   `login_failed` for `Login`.
7. 2xxxxx → `Auth`, code `account_pending`.
8. Anything else → `APIChanged`, code `gigya_error`.

Every message includes `errorCode` and Gigya's `errorMessage`, never the request.

**`SessionFromHAR`**
- Decode only the fields it needs.
- Consider only entries whose request host ends with `gigya.com`.
- Token sources:
  - the request's `login_token`, from `postData.params` or a form-parsed
    `postData.text`;
  - the response JSON's `sessionInfo.login_token`.
- gmid sources: the request `cookies`, or the `Cookie` header.
- Return the last entry in file order that has both. Never read or keep any other field.
- If some entry has a token but no entry has both: `har_no_gmid`, with hint `export the
  HAR with sensitive data (Chrome DevTools → Network → Export HAR (with sensitive
  data))`.

**`ParseSessionLine`**
- Trim `\r\n` and spaces, split on `;`, and cut each part at the first `=`.
- `glt_<APIKey>` or `login_token` gives the token; `gmid` gives the gmid. Other keys are
  ignored.

- [x] **Step 4: Run the tests and confirm they pass**

Run: `go test -race ./internal/gigya/`
Expected: `ok`.

- [x] **Step 5: Commit**

```bash
git add internal/gigya
git commit -m "feat(gigya): add account info, bootstrap, login and session import parsing"
```

---

### Task 4: Repsol API wire types, address service and public reads

**Files:**
- Create: `internal/repsolapi/types.go`, `internal/repsolapi/client.go`,
  `internal/repsolapi/places.go`, `internal/repsolapi/public.go`
- Test: `internal/repsolapi/types_test.go`, `internal/repsolapi/public_test.go`
- Test data (synthetic, shaped like spec §5): `internal/repsolapi/testdata/autocomplete.json`,
  `geocode.json`, `catalog.json`, `schedule.json`

**Interfaces:**
- Consumes: `httpx.Client`, `apperr`.
- Produces:
  - `const DefaultBaseURL = "https://pidetubombona.repsol.es/api"` and
    `DefaultPlacesBaseURL = "https://pro-repsol.ed-integrations.com"`.
  - `type FlexNumber string`: a JSON number or string. It marshals `""` as `""` and
    numeric text as a bare number.
  - `type Confidence struct { NumberNotFound, AgencyNotFound, ServiceNotFound,
    IsFromDiscardedIsland, ReferencePolicyInWrongIsland bool }`. The tags are
    `number_not_found`, `agency_not_found`, `service_not_found`,
    `is_from_discarded_island` and `reference_policy_in_wrong_island`.
  - `type Address struct`:
    - String fields: `Province, ProvinceCode, Locality, LocalityCode, StreetType,
      StreetTypeCode, Street, StreetCode, Municipality, MunicipalityCode, Number,
      AgencyCode, ServiceCode, PlaceID (placeId), Origin, FullAddress, Floor, Door,
      Details, PostalCode, AutocompleteContent`.
    - `Latitude, Longitude FlexNumber`.
    - `IsPeninsula bool`.
    - `Neutral *bool` (omitempty).
    - `Confidence *Confidence` (omitempty).
    - `CurrentFullAddress, CurrentStreet *string` (omitempty).
    - Every field's `json` and `toml` tag is the wire name.
  - `type PlacesClient struct { BaseURL string; HTTP *httpx.Client }`, with methods
    `Autocomplete(ctx, input string) ([]Address, error)` and
    `Geocode(ctx, placeID, number string) (Address, error)`.
  - `type Client struct { BaseURL string; HTTP *httpx.Client }`. Task 5 adds the
    `Signer` and `Telemetry` fields.
  - `func (c *Client) Health(ctx) error`
  - `type AgencyInfo struct { AgencyCode string; OnlinePayment, WayletPayment, AllowsTip bool }`
    and `func (c *Client) Agency(ctx, a Address) (AgencyInfo, error)`
  - `type Product struct { ID, MaterialID, Type, Description string; Price, PricePerKilo float64 }`
    and `func (c *Client) Catalog(ctx, agencyCode string) ([]Product, error)`
  - `type DeliveryDate struct { ID, Date string }`,
    `type Schedule struct { MaterialID string; Dates []DeliveryDate; Mode string; Warning bool; Type string }`
    and `func (c *Client) DeliverySchedule(ctx, materialID string, a Address) ([]Schedule, error)`
  - `func (c *Client) AddressMaps(ctx, a Address) (Address, error)`

- [x] **Step 1: Write the failing tests**

```go
func TestAddressJSONWireTypes(t *testing.T) {
	geo := decodeFile[Address](t, "testdata/geocode.json") // google origin, latitude 40.4168
	b, _ := json.Marshal(geo)
	mustContain(t, b, `"latitude":40.4168`, `"confidence":{"number_not_found":false`, `"currentFullAddress":""`)
	rep := decodeFile[struct{ Results []Address }](t, "testdata/autocomplete.json").Results[0] // repsol, latitude ""
	b, _ = json.Marshal(rep)
	mustContain(t, b, `"latitude":""`, `"neutral":false`)
	mustNotContain(t, b, `"confidence"`, `"currentStreet"`)
}

func TestAddressTOMLRoundTrip(t *testing.T) {
	// For both fixtures above: toml.Marshal, then toml.Unmarshal → reflect.DeepEqual to the original.
}

func TestAutocompleteQuery(t *testing.T) {
	// The server asserts path "/v1/autocomplete" and these query values:
	//   input == "Calle Ñandú 3, Móstoles"
	//   maxResults == "5"
	//   neutralAddress == "true"
	//   canarias == "true"
	//   islandToExclude == "elhierro,lagomera,lapalma,fuerteventura,lanzarote,lagraciosa"
	// It serves testdata/autocomplete.json → 2 results; [0].Origin "repsol", [1].Origin "google".
}

func TestGeocodeQuery(t *testing.T) {
	// path "/v1/geocode"; placeId, number, canarias=true, islandToExclude as above.
}

func TestAgencyQueryOmitsEmpty(t *testing.T) {
	a := Address{Origin: "google", ProvinceCode: "28", LocalityCode: "28000000001",
		MunicipalityCode: "28000000001", Number: "5", AgencyCode: "0000000001"}
	// The server asserts path "/agency", origin == "GOOGLE", inferredAgencyCode == "0000000001",
	// number == "5", and no "streetCode" key in the query.
	// It responds {"agencyCode":"0000000001","onlinePayment":true,"wayletPayment":true,"allowsTip":true}.
}

func TestCatalogNullMaterial(t *testing.T) {
	// testdata/catalog.json: a packaging row with "materialId":null, and rows 153 (18.89)
	// and 239 (18.89). Assert agencyCode is in the query; packaging row MaterialID == "";
	// 153 row Price == 18.89.
}

func TestScheduleOptionalMode(t *testing.T) {
	// A response without "mode" → Mode == "".
	// The query omits empty codes and includes materialId "153".
}

func TestBodyErrorIsRefused(t *testing.T) {
	// 200 {"type":"EmptyOrderError","errorCode":"EMPTY_ORDER_ERROR","error":true,"message":"Empty orders"}
	// → apperr Refused, code "api_error", message contains "EMPTY_ORDER_ERROR".
}

func TestUnexpectedShapeIsAPIChanged(t *testing.T) {
	// The catalog returns {"foo":1} → APIChanged, code "unexpected_response".
}

func TestAddressMapsEmptyBody(t *testing.T) {
	// 200 with an empty body → zero Address, nil error.
}
```

- [x] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/repsolapi/`
Expected: FAIL with `undefined: Address`.

- [x] **Step 3: Implement the types, the places client and the public reads**

**Requests**
- All are GETs with `Retry: true`. Endpoints are the paths below `/api` (for example
  `/agency`), or `/v1/...` for the address service.
- Query parameters with empty values are left out.
- Query parameters per call, in spec §5 order:
  - `Agency`: `streetCode, localityCode, provinceCode, municipalityCode,
    origin=strings.ToUpper(Origin), inferredAgencyCode=AgencyCode, number`.
  - `DeliverySchedule`: `materialId, agencyCode, provinceCode, localityCode,
    streetTypeCode, municipalityCode, streetCode, number`.
  - `AddressMaps`: `province, provinceCode, locality, streetType, streetTypeCode,
    street, municipality, postalCode`.

**Decoding** (one helper used by every call)
- If the body is a JSON object with `"error": true`, return `Refused`, code `api_error`.
  The message is the server's `message` with `(errorCode)` appended.
- Otherwise unmarshal. A failure returns `APIChanged`, code `unexpected_response`.

**`AddressMaps`** treats an empty body as a zero `Address`.

- [x] **Step 4: Run the tests and confirm they pass**

Run: `go test -race ./internal/repsolapi/`
Expected: `ok`.

- [x] **Step 5: Commit**

```bash
git add internal/repsolapi go.mod go.sum
git commit -m "feat(repsolapi): add wire types, address service and public reads"
```

---

### Task 5: Authenticated calls, history and order writes

**Files:**
- Create: `internal/repsolapi/auth.go`, `internal/repsolapi/history.go`,
  `internal/repsolapi/order.go`
- Modify: `internal/repsolapi/client.go`: add the fields `Signer Signer` and
  `Telemetry *Telemetry` to `Client`.
- Test: `internal/repsolapi/auth_test.go`, `internal/repsolapi/order_test.go`
- Test data (synthetic): `internal/repsolapi/testdata/history.json`, `solicitude.json`

**Interfaces:**
- Consumes: Task 4 types; `httpx`; `apperr`.
- Produces:
  - `type Auth struct { UID, Signature, Timestamp string }`
  - `type Signer interface { Sign(ctx context.Context) (Auth, error) }`
  - `type Telemetry struct { ProcessID, UserID, Session, User string }`
  - `func NewTelemetry() *Telemetry`:
    - `ProcessID`: 21 random characters.
    - `UserID`: `"us:"` + 48 random characters.
    - `Session` and `User`: 22 random characters each.
    - The alphabet is `A-Za-z0-9_-`, drawn from crypto/rand.
  - `type HistoryAgency struct { Name, Phone, RepsolPhone, AgencyCode string }`
  - `type HistoryAddress struct { Province, ProvinceCode, Locality, LocalityCode, StreetType,
    Street, StreetTypeCode, StreetCode, Municipality, MunicipalityCode, Number, Floor, Door,
    PostalCode, Details string }`
  - `type HistoryEntry struct`:
    - `CreatedAt, DeliveryDate, ReferenceSAC, MaterialID string`
    - `Quantity int`
    - `UnitPrice float64`
    - `Discount *float64`
    - `PaymentMethod *string`
    - `Bill bool`
    - `Agency HistoryAgency`
    - `Address HistoryAddress`
  - `func (c *Client) OrderHistory(ctx, limit, offset int) ([]HistoryEntry, error)`
  - `type CartCheckRequest struct`:
    - `ScheduleMode *string` (`json:"scheduleMode"`, sent as null when nil)
    - `AgencyCode, UserGigyaUID (userGigyaUid) string`
    - `Address Address`
    - `MaterialID string`
    - `Quantity int`
    - `UnitPrice float64`
    - `DeliveryDateID, DeliveryDate string`
  - `type CartCheck struct { Status string; OnlinePolicy, Order bool; OrderType string }`
  - `func (c *Client) CheckCart(ctx, r CartCheckRequest) (CartCheck, error)`: it fills
    `UserGigyaUID` from the `Auth` it signs with.
  - `type SolicitudeRequest struct { Address Address; MaterialID string; Quantity int;
    UnitPrice float64; Status string; Bill bool; Discount int; DeliveryDateID, DeliveryDate string }`
  - `type OrderRequest struct`: the same fields as `SolicitudeRequest` minus `Status`.
  - `type PlacedAgency struct { Name, Phone, RepsolPhone string }`
  - `type PlacedAddress struct { Province, Locality, Municipality, Street, Number, Door,
    Floor, PostalCode, StreetType string }`
  - `type Placed struct { ExternalID, DeliveryDate, Material string; Quantity int;
    UnitPrice float64; Address PlacedAddress; Agency PlacedAgency }`
  - `func (c *Client) CreateSolicitude(ctx, r SolicitudeRequest) (Placed, error)`
  - `func (c *Client) CreateOrder(ctx, r OrderRequest) (Placed, error)`

- [x] **Step 1: Write the failing tests**

```go
type stubSigner struct{ err error }

func (s stubSigner) Sign(context.Context) (Auth, error) {
	return Auth{UID: "U1", Signature: "S1", Timestamp: "1700000000"}, s.err
}

func TestHistoryAuthHeaders(t *testing.T) {
	// GET /order-history. Assert headers:
	//   uid=U1, signature=S1, signatureTimestamp=1700000000, ptb-gigya-user=U1
	//   no ptb-order-process-id
	// Assert query: limit=20, offset=0, paramToAvoidCache non-empty.
	// Serve testdata/history.json. Assert on h[0]:
	//   ReferenceSAC "10000001", MaterialID "153"
	//   Agency.AgencyCode "0000000001", Address.LocalityCode "28000000001"
	//   Discount == nil, Quantity == 1
}

func TestSignerErrorStopsRequest(t *testing.T) {
	// The signer returns apperr.New(apperr.Auth, "not_logged_in", "x").
	// OrderHistory returns that error, and the server sees 0 requests.
}

func TestWriteHeadersAndBodies(t *testing.T) {
	// One Client with Telemetry: NewTelemetry(); call CheckCart, then CreateSolicitude.
	// Both requests carry the same ptb-order-process-id (len 21).
	// ptb-appinsights-request matches ^rq:/cart/check:[A-Za-z0-9_-]{21}$, then ^rq:/solicitude:[A-Za-z0-9_-]{21}$.
	// ptb-app-generated-user-id matches ^us:[A-Za-z0-9_-]{48}$.
	// ptb-appinsights-session and ptb-appinsights-user match ^[A-Za-z0-9_-]{22}$.
	// cart/check body, decoded into map[string]any:
	//   scheduleMode "NO_SCHEDULE", userGigyaUid "U1", materialId "153" (string),
	//   quantity float64(1), unitPrice 18.89, deliveryDateId "1", deliveryDate "2026-11-05",
	//   address.latitude float64(40.4168)
	// solicitude body: status "EE04", bill false, discount float64(0), and no "userGigyaUid" key.
	// The CreateSolicitude result has ExternalID "800000000000000001" (testdata/solicitude.json).
}

func TestCartCheckNullScheduleMode(t *testing.T) {
	// ScheduleMode nil → the raw body contains `"scheduleMode":null`.
}

func TestWriteOutcomeUnknown(t *testing.T) { // Review Focus 3
	// Run for both CreateSolicitude and CreateOrder. Every case must give
	// Category OutcomeUnknown, Code "order_outcome_unknown", a hint containing
	// "repsol order last", and exactly 1 server call:
	//   - HTTP 502
	//   - 200 with body "<html>"
	//   - 200 with body {"externalId":""}
	//   - the server sleeps 200ms while c.HTTP.HTTP.Timeout = 50ms
	//   - ctx is cancelled 20ms into a 200ms server sleep
}

func TestWriteRejections(t *testing.T) {
	// 200 {"error":true,"errorCode":"X","message":"no"} → Refused
	// 401 → Auth
	// Neither is OutcomeUnknown. The server sees 1 call each.
}

func TestCheckCartNotRetried(t *testing.T) {
	// 503 → Category Network; the server sees 1 call.
}
```

- [x] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/repsolapi/ -run 'History|Signer|Write|Cart'`
Expected: FAIL with `undefined: HistoryEntry`.

- [x] **Step 3: Implement signing, history and writes**

**Authenticated requests**
- Call `Signer.Sign` before building the request, and return its error unchanged.
- Add the four auth headers.

**Write calls** (`/cart/check`, `/solicitude`, `/order`)
- POST JSON with `Retry: false`.
- Add the five telemetry headers. `ptb-appinsights-request` is
  `"rq:" + endpoint + ":" + 21 fresh random characters`.

**`OrderHistory`** sets `paramToAvoidCache` to the current Unix milliseconds.

**`CreateSolicitude` and `CreateOrder`**
- If `ctx.Err() != nil` before sending, return it.
- After sending, convert any of the following to
  `apperr.New(OutcomeUnknown, "order_outcome_unknown", "the order request failed after it was sent; it may or may not have been placed")`,
  with hint `run "repsol order last" and check before ordering again`:
  - a `Network` category error;
  - `context.Canceled` or `context.DeadlineExceeded`;
  - an `APIChanged` decode failure;
  - an empty `externalId`.
- Every other error passes through unchanged.

- [x] **Step 4: Run the tests and confirm they pass**

Run: `go test -race ./internal/repsolapi/`
Expected: `ok`.

- [x] **Step 5: Commit**

```bash
git add internal/repsolapi
git commit -m "feat(repsolapi): add signed history, cart check and order writes without retry"
```

---

### Task 6: Config and credential storage

**Files:**
- Create: `internal/config/paths.go`, `internal/config/store.go`
- Test: `internal/config/store_test.go`

**Interfaces:**
- Consumes: `repsolapi.Address`; `apperr`.
- Produces:
  - `const AppDir = "repsol-cli"`
  - `type Paths struct { Config, Credentials, Lock string }`
  - `func DefaultPaths() Paths`: `filepath.Join(xdg.ConfigHome, AppDir, "config.toml")`
    and `credentials.toml`; `Lock` is `credentials.toml.lock`. Creates nothing.
  - `type Profile struct { Address *repsolapi.Address }`, TOML key `address` (omitempty).
  - `type Config struct { DefaultProfile string; Profiles map[string]Profile }`, TOML
    keys `default_profile` and `profiles` (both omitempty).
  - `type Credentials struct { LoginToken, GMID, Email, Password, UID, FirstName string;
    UpdatedAt time.Time }`. The toml tags are `login_token`, `gmid`, `email`, `password`,
    `uid`, `first_name` and `updated_at`. The file layout is `[profiles.<name>]`.
  - `type Store struct { Paths Paths }`, with methods:
    - `LoadConfig() (Config, error)`
    - `UpdateConfig(fn func(*Config) error) error`
    - `LoadCredentials(profile string) (Credentials, bool, error)`
    - `UpdateCredentials(profile string, fn func(*Credentials) error) error`
    - `DeleteCredentials(profile string) (bool, error)`
  - `func ResolveProfile(flag, env string, cfg Config) (string, error)`: precedence is
    flag > env > `cfg.DefaultProfile` > `"default"`. Names must match
    `^[a-z0-9][a-z0-9_-]{0,31}$`, else `Usage` with code `invalid_profile`.

- [x] **Step 1: Write the failing tests**

```go
func newStore(t *testing.T) Store {
	d := filepath.Join(t.TempDir(), AppDir)
	return Store{Paths: Paths{Config: d + "/config.toml", Credentials: d + "/credentials.toml",
		Lock: d + "/credentials.toml.lock"}}
}

func TestMissingFilesAreEmpty(t *testing.T) {
	// LoadConfig → zero Config, nil.
	// LoadCredentials("default") → found == false, nil.
}

func TestCredentialsFileIsPrivate(t *testing.T) {
	// UpdateCredentials("default", set LoginToken "t", GMID "g").
	// Credentials file mode is 0600; the directory mode is 0700.
	// LoadCredentials returns the same values.
}

func TestConfigAddressRoundTrip(t *testing.T) {
	// UpdateConfig sets Profiles["default"].Address to an Address with
	// Latitude "40.4168", Confidence &Confidence{}, CurrentStreet ptr("") and Neutral nil.
	// LoadConfig returns a reflect.DeepEqual result.
}

func TestConcurrentCredentialUpdates(t *testing.T) { // Review Focus 5
	// 20 goroutines. Each uses its own Store value on the same Paths and calls
	// UpdateCredentials("p<i>", set LoginToken "t<i>").
	// Afterwards every p0..p19 has its token; no call returned an error; mode is 0600.
}

func TestDeleteCredentials(t *testing.T) {
	// Profiles a and b are stored. Delete("a") → true, and b is intact.
	// Delete("a") again → false.
}

func TestResolveProfile(t *testing.T) {
	// ("work","home",{DefaultProfile:"x"}) → "work"
	// ("","home",…)                        → "home"
	// ("","",{x})                          → "x"
	// ("","",{})                           → "default"
	// ("Bad Name","",{})                   → Usage, code "invalid_profile"
}
```

- [x] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/config/`
Expected: FAIL with `undefined: Store`.

- [x] **Step 3: Implement the store**

**Update functions** (both)
- Take `flock.New(Paths.Lock).Lock()` and release it with defer.
- Load, call `fn`, then write atomically:
  1. `MkdirAll(dir, 0o700)`.
  2. `os.CreateTemp(dir, ".tmp-*")`, then `Chmod` (0600 for credentials, 0644 for
     config).
  3. Write, `Sync`, `Close`, `Rename`.

**Load** of a missing file returns the zero value. A TOML parse error returns `Generic`
with code `config_invalid`. The message names the path and never the content.

- [x] **Step 4: Run the tests and confirm they pass**

Run: `go test -race ./internal/config/`
Expected: `ok`.

- [x] **Step 5: Commit**

```bash
git add internal/config go.mod go.sum
git commit -m "feat(config): add profile config and locked 0600 credential store"
```

---

### Task 7: Fake backends for tests

**Files:**
- Create: `internal/fakerepsol/server.go`, `internal/fakerepsol/gigya.go`,
  `internal/fakerepsol/api.go`, `internal/fakerepsol/places.go`
- Test: `internal/fakerepsol/server_test.go`

**Interfaces:**
- Consumes: `repsolapi` types, `gigya.APIKey`, and the clients (in the test only).
- Produces:
  - `func New() *Server` (starts `httptest.NewServer`) and `func (s *Server) Close()`.
  - Base URLs: `GigyaURL()` = `URL+"/gigya"`, `APIURL()` = `URL+"/api"`, and
    `PlacesURL()` = `URL+"/places"`.
  - Constants:
    - `ValidToken = "fake-token"`, `ValidGMID = "fake-gmid"`,
      `BootGMID = "fake-gmid-boot"`, `RenewedToken = "fake-token-renewed"`.
    - `UID = "fake-uid"`, `Signature = "fake-signature"`.
    - `Email = "owner@example.com"`, `Password = "correct horse"`,
      `FirstName = "Owner"`, `CaptchaEmail = "captcha@example.com"`.
    - `AgencyCode = "0000000001"`, `ProvinceCode = "28"`,
      `LocalityCode = "28000000001"`, `PostalCode = "28001"`.
    - `ExternalID = "800000000000000001"`.
  - Knobs:
    - `RejectToken(token string)`
    - `SetToday(d time.Time)`
    - `SetHistory(h []repsolapi.HistoryEntry)`
    - `SetCheck(c repsolapi.CartCheck)`
    - `SetAgency(code string)`
    - `FailNext(path string, status int)`
  - Inspection: `Requests(path string) []Recorded` and `Count(path string) int`, with
    `type Recorded struct { Method string; Query url.Values; Header http.Header; Body []byte }`.
    Paths are full server paths such as `/api/solicitude`.

Behaviour (all values are synthetic; JSON responses are built from `repsolapi` types or
maps):

| Route | Behaviour |
|---|---|
| `POST /gigya/accounts.getAccountInfo` | Needs a non-empty `gmid` cookie, `APIKey`, and `login_token` ∈ {ValidToken, RenewedToken} that is not rejected. Otherwise `errorCode 403005`. OK → `UID`, `UIDSignature: Signature`, `signatureTimestamp: "1700000000"`, `profile{email: Email, firstName: FirstName}`. |
| `GET /gigya/accounts.webSdkBootstrap` | `Set-Cookie: gmid=BootGMID`, `{"errorCode":0}`. |
| `POST /gigya/accounts.login` | Needs a `gmid` cookie (else 403005). `CaptchaEmail` → 401020. `Email`+`Password` → errorCode 0, signature fields, profile, and `sessionInfo{login_token: RenewedToken, expires_in: "0"}`. Anything else → 403042. |
| `GET /places/v1/autocomplete` | Picks results by `input` substring. `"Falsa"` → [repsol, number "1", neutral false, lat "", all codes set, `fullAddress "Calle Falsa 1, 28001 Pueblo, Madrid"`, agencyCode AgencyCode] plus [repsol, neutral true, number ""]. `"Ambigua"` → two google results, numbers "5" and "7", placeIds `place-a` and `place-b`, empty codes. `"Neutral"` → [repsol, neutral true, number "3"]. `"Sin numero"` → [repsol, neutral true, number ""]. `"Lejos"` → [google, number "9", placeId `place-noagency`]. Otherwise `{"results":[]}`. |
| `GET /places/v1/geocode` | A google address with ProvinceCode, LocalityCode, MunicipalityCode, empty StreetCode, the requested `number`, `latitude 40.4168`, `longitude -3.7038`, AgencyCode, `currentFullAddress ""`, `currentStreet ""`, and confidence all false, except `agency_not_found: true` for `place-noagency`. |
| `GET /api/health` | 200 `{"status":"UP"}`. |
| `GET /api/address-maps` | 200 with an empty body, as in the HAR. |
| `GET /api/agency` | `{"agencyCode": <SetAgency value, default AgencyCode>, "onlinePayment":true, …}`. |
| `GET /api/catalog` | A packaging row (null materialId), then 153 18.89, 154 23.4, 239 18.89, 210 88.7, 230 26.6. |
| `GET /api/deliverySchedule` | `[{"materialId":<query>, "dates":[{"id":"2","date":today+5},{"id":"1","date":today+3}], "mode":"NO_SCHEDULE", "warning":false, "type":"WEEKLY"}]`, where today is the `SetToday` value or the real date in Europe/Madrid. |
| `GET /api/order-history` | Needs `uid==UID` and `signature==Signature`, else 401. Returns the history, newest first, honouring `limit` and `offset`. |
| `POST /api/cart/check` | Same auth. Returns the `SetCheck` value, default `{status "EE04", onlinePolicy false, order false, orderType "SOLICITUDE"}`. |
| `POST /api/solicitude`, `POST /api/order` | Same auth. Responds with `Placed`: externalId ExternalID (`/order` uses `…002`), plus the request's deliveryDate, quantity and unitPrice, material `"Butano 12.5kg"`, and agency `{"Agencia Test","900 000 000","900 000 001"}`. Prepends a `HistoryEntry` (referenceSAC `"2000000N"` counting from 1, AgencyCode, the request address's LocalityCode, materialId, deliveryDate). |
| any | When `FailNext(path, status)` is armed for the path, the next request gets that status and an empty body. Every request is recorded. |

- [x] **Step 1: Write the failing test**

```go
func TestFakeAgreesWithClients(t *testing.T) {
	f := New(); defer f.Close()
	hc := httpx.New("repsol-cli/test", nil)
	g := &gigya.Client{BaseURL: f.GigyaURL(), APIKey: gigya.APIKey, HTTP: hc, Now: time.Now}
	// 1. g.AccountInfo(Session{ValidToken, ""}) → code gigya.CodeSessionRejected (gmid required)
	// 2. g.AccountInfo(Session{ValidToken, ValidGMID}) → sig.UID == UID
	// 3. g.Login(ctx, BootGMID, Email, Password) → Session.LoginToken == RenewedToken
	// 4. places.Autocomplete("Calle Falsa 1, Pueblo") → len 2, [0].Origin "repsol", [0].Number "1"
	// 5. api.Agency(results[0]) → AgencyCode
	// 6. api.OrderHistory with a signer returning the wrong signature → Auth
	// 7. api.CheckCart with the right signer → OrderType "SOLICITUDE", Status "EE04"
	// 8. f.Count("/api/cart/check") == 1
}
```

- [x] **Step 2: Run the test and confirm it fails**

Run: `go test ./internal/fakerepsol/`
Expected: FAIL with `undefined: New`.

- [x] **Step 3: Implement the fake per the table**

Use one `http.ServeMux` and one mutex around the state.

- [x] **Step 4: Run the test and confirm it passes**

Run: `go test -race ./internal/fakerepsol/`
Expected: `ok`.

- [x] **Step 5: Commit**

```bash
git add internal/fakerepsol
git commit -m "test(fakerepsol): add in-process fake of Gigya, Repsol API and address service"
```

---

### Task 8: Session signing, automatic re-login and auth use cases

**Files:**
- Create: `internal/app/app.go`, `internal/app/session.go`, `internal/app/auth.go`,
  `internal/app/madrid.go`
- Test: `internal/app/app_test.go` (helpers), `internal/app/session_test.go`,
  `internal/app/auth_test.go`

**Interfaces:**
- Consumes: `gigya.Client`, `gigya.Session`, `gigya.Code*`, `repsolapi.Client`,
  `repsolapi.PlacesClient`, `repsolapi.Auth`, `config.Store`, `apperr`.
- Produces:
  - `type Env struct { LoginToken, GMID, Email, Password string }`: values from the
    `REPSOL_*` environment variables, never persisted.
  - `type Deps struct { API *repsolapi.Client; Places *repsolapi.PlacesClient;
    Gigya *gigya.Client; Store config.Store; Profile string; Env Env; Now func() time.Time }`
  - `type App struct` (unexported fields) and `func New(d Deps) *App`: sets
    `d.API.Signer` to the app's `*Session`.
  - `const SignatureMaxAge = 50 * time.Second`
  - `func (s *Session) Sign(ctx context.Context) (repsolapi.Auth, error)`
  - `type AuthStatus struct`, JSON keys in brackets:
    - `Profile` (`profile`)
    - `LoggedIn` (`logged_in`)
    - `Source` (`source`): `"stored"`, `"env"` or `""`
    - `Email` (`email,omitempty`)
    - `FirstName` (`first_name,omitempty`)
    - `PasswordStored` (`password_stored`)
    - `AddressSet` (`address_set`)
    - `Checked` (`checked`)
    - `SessionValid *bool` (`session_valid,omitempty`)

    `LoggedIn`, `PasswordStored`, `AddressSet` and `Checked` are `bool`; the other
    fields except `SessionValid` are `string`.
  - `func (a *App) ImportSession(ctx, s gigya.Session) (AuthStatus, error)`
  - `func (a *App) PasswordLogin(ctx, email, password string) (AuthStatus, error)`
  - `func (a *App) Status(ctx, check bool) (AuthStatus, error)`
  - `type LogoutResult struct { Profile string; Removed bool }`, JSON keys `profile`
    and `removed`,
    and `func (a *App) Logout() (LogoutResult, error)`
  - In `madrid.go`: `var madrid = mustLoad("Europe/Madrid")` and the blank import
    `_ "time/tzdata"`.

`Sign` rules, in order:
1. A cached signature younger than `SignatureMaxAge` is returned as is.
2. The session source is chosen:
   - When `Env.LoginToken` is set, the source is env. Without `Env.GMID` this is `Usage`
     with code `missing_gmid` and hint `set REPSOL_GMID as well`.
   - Otherwise the source is the stored credentials of the profile.
   - Login credentials are `Env.Email`/`Env.Password` when both are set, else the stored
     ones.
3. No token and no password → `Auth`, code `not_logged_in`, hint `run "repsol auth login
   --har FILE" or "repsol auth login --email ADDRESS"`.
4. If there is a token, call `AccountInfo`. On `CodeSessionRejected` with a password
   available, re-login. With no password, return `Auth` code `session_expired`, with the
   same hint plus `; log in with --email to enable automatic re-login`. Other errors are
   returned as is.
5. Re-login reuses the session's gmid, or calls `Bootstrap` when it has none. It then
   calls `Login`, which yields the signature directly. The new token and gmid are saved
   with `UpdateCredentials` unless the source is env. A `Login` error is returned as is
   (captcha, invalid credentials). There is no second attempt.

`Session` guards its cache and re-login with a `sync.Mutex`, because the MCP server can
run tool calls concurrently.

- [ ] **Step 1: Write the failing tests**

```go
// app_test.go helpers
type testClock struct{ t time.Time }
func (c *testClock) Now() time.Time          { return c.t }
func (c *testClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// newTestApp wires every client to a fresh fakerepsol.Server and httpx.New("repsol-cli/test", nil)
// with zero backoff. It uses a Store under t.TempDir(), Profile "default",
// a clock at 2026-11-01T10:00:00Z, and fake.SetToday(2026-11-01).
func newTestApp(t *testing.T, opts ...func(*Deps)) (*App, *fakerepsol.Server, config.Store, *testClock)

// storeSession writes c into the "default" profile's credentials.
// Most tests pass config.Credentials{LoginToken: fakerepsol.ValidToken, GMID: fakerepsol.ValidGMID}.
func storeSession(t *testing.T, st config.Store, c config.Credentials)

func TestImportSessionStoresSession(t *testing.T) {
	// ImportSession(Session{ValidToken, ValidGMID}) →
	//   status LoggedIn, Source "stored", Email fakerepsol.Email, AddressSet false.
	// Stored creds: LoginToken ValidToken, GMID ValidGMID, UID, Email.
	// cfg.DefaultProfile == "default".
}
func TestImportSessionRejected(t *testing.T) {
	// fake.RejectToken(ValidToken) → Auth with code gigya.CodeSessionRejected;
	// LoadCredentials found == false.
}
func TestImportKeepsPasswordOnlyForSameAccount(t *testing.T) {
	// Stored {Email: fakerepsol.Email, Password: "pw"} → after import, Password == "pw".
	// Stored {Email: "other@example.com", Password: "pw"} → after import, Password == "",
	// Email == fakerepsol.Email.
}
func TestPasswordLogin(t *testing.T) {
	// Stored: LoginToken RenewedToken, GMID BootGMID, Password fakerepsol.Password.
	// Count(bootstrap) == 1, Count(login) == 1.
	// The login request's Cookie contains "gmid=" + BootGMID.
}
func TestSignCachesSignature(t *testing.T) {
	// Stored session. Sign; Advance(49s); Sign → Count(getAccountInfo) == 1.
	// Advance(2s); Sign → Count == 2.
}
func TestAutoRelogin(t *testing.T) {
	// Stored {ValidToken, ValidGMID, Email, Password} and RejectToken(ValidToken).
	// Sign succeeds; stored LoginToken == RenewedToken.
	// The login Cookie has gmid ValidGMID; Count(bootstrap) == 0.
}
func TestExpiredWithoutPassword(t *testing.T) {
	// Rejected token, no password → Auth, code "session_expired", hint contains "repsol auth login".
}
func TestNotLoggedIn(t *testing.T) { /* empty store → Auth, code "not_logged_in" */ }
func TestCaptchaDoesNotLoop(t *testing.T) {
	// Stored {rejected token, CaptchaEmail, "x"} → Auth, code gigya.CodeCaptchaRequired;
	// Count(login) == 1.
}
func TestEnvSessionIsNotPersisted(t *testing.T) {
	// Env{ValidToken, ValidGMID} → Sign ok, and the credentials file does not exist.
	// Env{LoginToken: ValidToken (rejected), GMID: ValidGMID, Email, Password} →
	// Sign ok, and the file still does not exist.
}
func TestEnvTokenWithoutGMID(t *testing.T) { /* Env{LoginToken: ValidToken} → Usage, code "missing_gmid" */ }
func TestStatusCheck(t *testing.T) {
	// Valid session: Status(ctx, true) → Checked, *SessionValid == true.
	// Rejected token without password: Status(ctx, true) → Auth error.
	// Status(ctx, false) makes no Gigya call.
}
func TestLogout(t *testing.T) { /* Removed true, then false; other profiles untouched */ }
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/app/`
Expected: FAIL with `undefined: New`.

- [ ] **Step 3: Implement `App`, `Session` and the auth use cases**

**`ImportSession`**
- Calls `AccountInfo`. On error it returns before writing anything.
- Stores `LoginToken`, `GMID`, `UID`, `FirstName` and the account `Email`.
- Keeps `Password` only when the stored email matches the account email, compared
  case-insensitively. Otherwise it clears it.
- Sets `DefaultProfile` when it is empty.

**`PasswordLogin`** stores the email and password too.

Both cache the signature they obtained in the session.

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test -race ./internal/app/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/app
git commit -m "feat(app): add session signing with cached signature and automatic re-login"
```

---

### Task 9: Products, address resolution, catalog and schedule

**Files:**
- Create: `internal/app/products.go`, `internal/app/address.go`, `internal/app/catalog.go`,
  `internal/app/schedule.go`
- Test: `internal/app/products_test.go`, `internal/app/address_test.go`,
  `internal/app/catalog_test.go`

**Interfaces:**
- Consumes: Task 8 `App` and `newTestApp`; `repsolapi` reads; `config.Store`.
- Produces:
  - `type ProductInfo struct { Alias, MaterialID string; MaxQty int }`
  - `func ResolveProduct(arg, provinceCode string) (ProductInfo, error)`
  - `func AliasFor(materialID string) string`
  - `type SetAddressInput struct { Text, Floor, Door, Details string; Pick int }`
  - `type AddressView struct`, JSON keys in brackets:
    - `Profile` (`profile`)
    - `Summary` (`summary`)
    - `FullAddress` (`full_address`)
    - `PostalCode` (`postal_code`)
    - `Locality` (`locality`)
    - `Province` (`province`)
    - `Floor` (`floor,omitempty`)
    - `Door` (`door,omitempty`)
    - `Details` (`details,omitempty`)
    - `AgencyCode` (`agency_code`)
    - `Origin` (`origin`)

    All `AddressView` fields are `string`.
  - `func (a *App) SetAddress(ctx, in SetAddressInput) (AddressView, error)`
  - `func (a *App) ShowAddress() (AddressView, error)`: with no address, `Usage`, code
    `no_address`, hint `run "repsol address set \"STREET NUMBER, TOWN\""`.
  - `type CatalogItem struct`, JSON keys in brackets: `Product` (`product`),
    `MaterialID` (`material_id`), `Name` (`name`), `Price` (`price`), `PricePerKilo`
    (`price_per_kilo,omitempty`), `MaxQuantity` (`max_quantity`). `Price` and
    `PricePerKilo` are `float64`, `MaxQuantity` is `int`, the rest are `string`.
  - `func (a *App) Catalog(ctx) ([]CatalogItem, error)`
  - `type ScheduleView struct`, JSON keys in brackets: `Product` (`product`),
    `MaterialID` (`material_id`), `Mode` (`mode,omitempty`), `Warning bool`
    (`warning`), `Dates []string` (`dates`). The rest are `string`.
  - `func (a *App) Schedule(ctx, product string) (ScheduleView, error)`
  - Unexported helpers for Task 10: `loadAddress() (repsolapi.Address, error)` and
    `currentAgency(ctx, addr) (string, error)`. With an empty `agencyCode`, the helper
    returns `Refused`, code `no_delivery`.
  - Test helper in `address_test.go`: `mustSetAddress(t, a *App)`, which runs
    `SetAddress(ctx, SetAddressInput{Text: "Calle Falsa 1, Pueblo"})` and fails the
    test on error. Task 10 tests use it together with `storeSession`.

Product table: `butano`→153, `butano-ligera`→154, `propano`→239, `propano-35`→210,
`autogas`→230, `butano-canarias`→409. The maximum quantity is 24 for materials 210, 230,
239, 320 and 620, and 9 for every other material.

`ResolveProduct` rules:
- `""` means `butano`, except that `butano` resolves to 409 when the province code is
  `35` or `38`.
- Aliases are matched case-insensitively.
- An all-digit argument is a material id. Its alias comes from `AliasFor` and may be
  empty.
- Anything else → `Usage`, code `unknown_product`, with the aliases listed in the
  message.

`SetAddress` algorithm:
1. `Autocomplete(in.Text)`.
2. The candidates are the results with `Number != ""` that are not neutral
   (`Neutral == nil || !*Neutral`; google results carry no `neutral` key).
3. If there are no candidates:
   - when some result has a number but is neutral → `Usage`, code
     `address_not_supported`, message `this address needs a policy lookup the CLI does
     not support; try a fuller address`;
   - otherwise → `Usage`, code `address_needs_number`.
4. If there is more than one candidate and `Pick == 0` → `Usage`, code
   `ambiguous_address`. The message lists `1) <fullAddress> [origin]` and so on; the
   hint is `re-run with --pick N`. A `Pick` outside `1..len` → `Usage`, code
   `invalid_pick`.
5. When the chosen result has origin `google`, call `Geocode(placeId, number)`. A result
   with `number_not_found`, `agency_not_found` or `service_not_found` set →
   `Refused`, code `address_not_served`.
6. For google results, call `AddressMaps`, best effort. Copy its non-empty string fields
   over the address and ignore its errors.
7. An empty `ProvinceCode` → `Usage`, code `address_incomplete`.
8. Set `agencyCode` from `currentAgency`.
9. Fill in the remaining fields:
   - `PostalCode`: the first `\d{5}` in `FullAddress`;
   - `IsPeninsula`: false;
   - `Floor`, `Door` and `Details` from the input;
   - `AutocompleteContent`: the chosen candidate's `FullAddress`.
10. Save with `UpdateConfig`.

`Summary` is `FullAddress` followed by `, floor F` and `, door D` when those are set.

`Catalog` needs a stored address. It uses `currentAgency`, drops rows without a
`materialId`, and sets `Product` with `AliasFor`.

`Schedule` uses the stored address, `currentAgency` and `ResolveProduct`. Dates are
sorted ascending. A `NO_AGENCY` mode or no dates → `Refused`, code
`no_delivery_dates`.

- [x] **Step 1: Write the failing tests**

```go
func TestResolveProduct(t *testing.T) {
	// ("", "28") → 153/9; ("", "35") → 409; ("PROPANO", "") → 239/24;
	// ("620", "") → MaxQty 24, Alias ""; ("153", "") → Alias "butano";
	// ("gasoil", "") → Usage, code "unknown_product".
}
func TestSetAddressRepsolResultUsedAsIs(t *testing.T) {
	// The session is stored (helper). SetAddress({Text: "Calle Falsa 1, Pueblo", Floor: "2", Door: "B"}) →
	//   Count(geocode) == 0
	//   view.AgencyCode == AgencyCode, PostalCode == "28001", Summary ends with ", floor 2, door B"
	// The stored Address has IsPeninsula false, Latitude "", and
	// AutocompleteContent == "Calle Falsa 1, 28001 Pueblo, Madrid".
}
func TestSetAddressGoogleGeocodes(t *testing.T) {
	// {Text: "Calle Ambigua", Pick: 2} → geocode query placeId "place-b", number "7".
	// Stored Latitude "40.4168"; Confidence != nil.
}
func TestSetAddressAmbiguous(t *testing.T) {
	// {Text: "Calle Ambigua"} → Usage, code "ambiguous_address".
	// The message contains "1)" and "2)"; the hint contains "--pick"; nothing is stored.
}
func TestSetAddressRejections(t *testing.T) {
	// "Sin numero"        → Usage, code "address_needs_number"
	// "Neutral"           → Usage, code "address_not_supported"
	// "Lejos"             → Refused, code "address_not_served"
	// "Ambigua" + Pick 3  → Usage, code "invalid_pick"
}
func TestCatalogFiltersPackaging(t *testing.T) {
	// Address stored → 5 items; [0] = {Product "butano", MaterialID "153", Price 18.89, MaxQuantity 9}.
	// No address → Usage, code "no_address".
}
func TestScheduleSortedAndRefusals(t *testing.T) {
	// Dates == [today+3, today+5].
	// fake.SetAgency("") → Refused, code "no_delivery".
}
```

- [x] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/app/ -run 'Product|Address|Catalog|Schedule'`
Expected: FAIL with `undefined: ResolveProduct`.

- [x] **Step 3: Implement products, address, catalog and schedule as specified above**

- [x] **Step 4: Run the tests and confirm they pass**

Run: `go test -race ./internal/app/`
Expected: `ok`.

- [x] **Step 5: Commit**

```bash
git add internal/app
git commit -m "feat(app): add product aliases, address resolution, catalog and schedule"
```

---

### Task 10: Placing orders, pending guard and history

**Files:**
- Create: `internal/app/order.go`, `internal/app/history.go`
- Test: `internal/app/order_test.go`, `internal/app/history_test.go`

**Interfaces:**
- Consumes: Task 9 helpers; `repsolapi.CheckCart`, `CreateSolicitude`, `CreateOrder`,
  `OrderHistory`; the `madrid` location.
- Produces:
  - `const PendingLookback = 20` and `const ShowLookback = 100`
  - `type PlaceOrderInput struct { Product string; Quantity int; Date, Pay string; DryRun, Force bool }`
  - `type AgencyView struct { Name, Phone, Code string }`, with JSON keys `name`,
    `phone` and `code`, all omitempty.
  - `type OrderResult struct`, JSON keys in brackets:
    - `ID` (`id,omitempty`)
    - `DryRun` (`dry_run`)
    - `OrderType` (`order_type`)
    - `Product` (`product`)
    - `MaterialID` (`material_id`)
    - `Quantity` (`quantity`)
    - `UnitPrice` (`unit_price`)
    - `Total` (`total`)
    - `DeliveryDate` (`delivery_date`)
    - `Address` (`address`)
    - `Agency AgencyView` (`agency`)
    - `Payment` (`payment`), always `"on_delivery"`

    `DryRun` is `bool`, `Quantity` is `int`, `UnitPrice` and `Total` are `float64`,
    `Address` is the summary `string`, and the rest are `string`.
  - `func (a *App) PlaceOrder(ctx, in PlaceOrderInput) (OrderResult, error)`
  - `type OrderSummary struct`, JSON keys in brackets:
    - `Reference` (`reference`)
    - `CreatedAt` (`created_at`)
    - `DeliveryDate` (`delivery_date`)
    - `Product` (`product,omitempty`)
    - `MaterialID` (`material_id`)
    - `Quantity` (`quantity`)
    - `UnitPrice` (`unit_price`)
    - `Total` (`total`)
    - `Address` (`address`)
    - `Agency AgencyView` (`agency`)
    - `Pending` (`pending`)

    `Pending` is `bool`, `Quantity` is `int`, `UnitPrice` and `Total` are `float64`,
    and the rest are `string`.
  - `func (a *App) ListOrders(ctx, limit int) ([]OrderSummary, error)`: a limit outside
    `1..100` → `Usage`.
  - `func (a *App) LastOrder(ctx) (OrderSummary, error)`: with no orders, `NotFound`, code
    `no_orders`.
  - `func (a *App) ShowOrder(ctx, reference string) (OrderSummary, error)`: a reference
    that does not match `^\d{8}$` → `Usage`; a reference not found in `ShowLookback`
    entries → `NotFound`, code `order_not_found`.

`PlaceOrder` steps (stop at the first error):
1. `Pay` must be `""` or `"delivery"`, else `Usage`, code `unsupported_payment`. A
   non-empty `Date` that does not parse as `2006-01-02` → `Usage`, code
   `invalid_date`.
2. `loadAddress`.
3. `ResolveProduct(in.Product, addr.ProvinceCode)`. The quantity must be in
   `1..MaxQty`, else `Usage`, code `invalid_quantity`, with the maximum in the message.
4. `currentAgency`.
5. `Catalog(agency)`. If the material is missing → `Refused`, code
   `product_unavailable`. Otherwise `unitPrice` is the catalog price.
6. `DeliverySchedule`. Use the entry whose `MaterialID` matches, else the first entry.
   - `NO_AGENCY` or no dates → `Refused`, code `no_delivery_dates`.
   - With `in.Date` set, the matching date is used. A date not offered → `Refused`, code
     `date_unavailable`, with the hint listing the offered dates.
   - Otherwise the earliest date is used.
7. Pending guard, skipped when `Force` is set:
   - Fetch `OrderHistory(PendingLookback, 0)`.
   - An entry is pending when all of these hold:
     - `DeliveryDate >= Now().In(madrid).Format("2006-01-02")`;
     - its `Agency.AgencyCode` equals the current agency;
     - its `Address.LocalityCode` equals `addr.LocalityCode`.
   - The first pending entry → `Refused`, code `pending_order`, message
     `order <referenceSAC> for <deliveryDate> is still pending at this address`.
   - Hint: `pass --force only if the owner explicitly wants another delivery; a delivery
     cancelled by phone still shows as pending until its date`.
8. `CheckCart`:
   - `ScheduleMode` is `&mode` when `mode != ""`, else nil.
   - `Address` is `addr` with `AgencyCode` set to the current agency.
   - `Quantity` and `UnitPrice`, plus the chosen date's `ID` and `Date`.
9. Decide on the check result:
   - `OnlinePolicy` → `Refused`, code `online_policy_required`, hint `this order needs
     the web flow: https://pidetubombona.repsol.es/`;
   - else `OrderType == "SOLICITUDE"` → solicitude;
   - else `Order` → order;
   - else → `Refused`, code `unsupported_order_type`.
10. When `DryRun` is set, return the result with `DryRun: true`, the `OrderType` and no
    ID.
11. Write with `CreateSolicitude` (with `Status` taken from the check) or with
    `CreateOrder`, using `Bill: false` and `Discount: 0`.
12. Build the result:
    - `ID` is the `ExternalID`;
    - `DeliveryDate` is the placed date, falling back to the chosen one;
    - `Agency` comes from the placed agency;
    - `Total = math.Round(unitPrice*qty*100)/100`.

History mapping:
- `CreatedAt` is parsed with `2006-01-02T15:04:05.000-0700` and printed as RFC 3339; on a
  parse failure the raw value is kept.
- `Address`:
  - `"<streetType> <street> <number>, <locality>"` when `Street` is non-empty;
  - else `"<details>, <locality>"`;
  - empty parts are trimmed.
- `Agency` is `{Name, Phone, Code: AgencyCode}`.
- `Pending` uses the rule in step 7 against the stored address. When no address is stored,
  only the date counts.

- [ ] **Step 1: Write the failing tests**

```go
func TestPlaceOrderSolicitude(t *testing.T) {
	// Session and address are stored (helpers). PlaceOrder({Quantity: 1}) →
	//   ID == ExternalID, Payment "on_delivery", Total 18.89, Product "butano",
	//   DeliveryDate == "2026-11-04" (today+3, the earliest), Agency.Name "Agencia Test"
	// Request order: /api/agency, /api/catalog, /api/deliverySchedule, /api/order-history,
	//   /api/cart/check, /api/solicitude (exactly one each)
	// Solicitude body: status "EE04", materialId "153", deliveryDateId "1", unitPrice 18.89
}
func TestPlaceOrderDryRunWritesNothing(t *testing.T) {
	// DryRun: Count("/api/cart/check") == 1, Count("/api/solicitude") == 0,
	// res.DryRun true, res.OrderType "SOLICITUDE", res.ID "".
}
func TestPendingGuardBlocks(t *testing.T) {
	// SetHistory([{ReferenceSAC "10000009", DeliveryDate "2026-11-03", AgencyCode, LocalityCode}]) →
	//   Refused, code "pending_order", message contains "10000009"
	//   Count(cart/check) == 0, Count(solicitude) == 0
	// The same with Force: true → succeeds.
}
func TestPendingGuardScope(t *testing.T) {
	// Each of these does not block:
	//   an entry with another LocalityCode
	//   an entry with another AgencyCode
	//   an entry with DeliveryDate "2026-10-31" (yesterday)
}
func TestPendingGuardUsesMadridDate(t *testing.T) { // Review Focus 1
	// The clock is at 2026-11-04T23:30:00Z (00:30 on 2026-11-05 in Madrid).
	// An entry dated "2026-11-04" → not pending, so PlaceOrder succeeds.
	// An entry dated "2026-11-05" → Refused, code "pending_order".
}
func TestOnlinePolicyRefused(t *testing.T) {
	// SetCheck{OnlinePolicy: true} → Refused, code "online_policy_required";
	// no /solicitude and no /order.
}
func TestOrderBranch(t *testing.T) {
	// SetCheck{Status: "EE01", Order: true, OrderType: "ORDER"} →
	// Count("/api/order") == 1, Count("/api/solicitude") == 0.
}
func TestWriteFailureIsOutcomeUnknown(t *testing.T) {
	// FailNext("/api/solicitude", 502) → ExitCode 9; Count("/api/solicitude") == 1.
}
func TestOrderInputValidation(t *testing.T) {
	// {Quantity: 10} (butano) → Usage, code "invalid_quantity"
	// {Product: "propano", Quantity: 24, DryRun: true} → ok
	// {Date: "2030-01-01"} → Refused, code "date_unavailable"
	// {Pay: "card"} → Usage, code "unsupported_payment"
	// {Date: "5/11/2026"} → Usage, code "invalid_date"
}
func TestHistoryViews(t *testing.T) {
	// ListOrders(10) after one PlaceOrder → [0].Reference "20000001", Pending true.
	// ShowOrder("20000001") → found. ShowOrder("1234") → Usage. ShowOrder("99999999") → NotFound.
	// LastOrder with empty history → NotFound, code "no_orders".
	// A solicitude-style entry (Street "") → Address == "<details>, <locality>".
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/app/ -run 'Order|Pending|History|Policy'`
Expected: FAIL with `undefined: PlaceOrder`.

- [ ] **Step 3: Implement `PlaceOrder` and the history views as specified above**

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test -race ./internal/app/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/app
git commit -m "feat(app): add order placement with pending guard and history views"
```

---

### Task 11: Output, CLI root, auth commands and the script test harness

**Files:**
- Create: `internal/output/output.go`, `internal/output/envelope.go`
- Create: `internal/cli/main.go`, `internal/cli/root.go`, `internal/cli/prompt.go`,
  `internal/cli/auth.go`
- Create: `cmd/repsol/main.go`
- Test: `internal/output/output_test.go`, `internal/cli/prompt_test.go`,
  `internal/cli/root_test.go`, `internal/cli/script_test.go`, `internal/cli/testdata/script/auth.txtar`,
  `internal/cli/testdata/script/errors.txtar`

**Interfaces:**
- Consumes: everything in Tasks 1–10.
- Produces:
  - `output.Format` (`FormatJSON = "json"`, `FormatPretty = "pretty"`) and
    `func ResolveFormat(flag string, stdoutTTY bool) (Format, error)`.
  - `type Pretty interface { PrettyPrint(w io.Writer) error }` and
    `func Render(w io.Writer, f Format, v any) error`:
    - JSON is indented with 2 spaces and ends with a newline.
    - Pretty uses `PrettyPrint` when `v` implements it, else falls back to indented JSON.
  - `output.ErrorFormat` (`ErrorText = "text"`, `ErrorJSON = "json"`) and
    `func ResolveErrorFormat(flag, env string) (ErrorFormat, error)`: flag > env >
    `text`.
  - `type Envelope struct`, with these JSON keys, all always present:
    - `Code` (`code`)
    - `Category` (`category`)
    - `ExitCode` (`exit_code`)
    - `Retryable` (`retryable`)
    - `Message` (`message`)
    - `HTTPStatus` (`http_status`)
    - `Endpoint` (`endpoint`)
    - `Hint` (`hint`)

    `ExitCode` and `HTTPStatus` are `int`, `Retryable` is `bool`, and the rest are
    `string`.
  - `func EnvelopeFor(err error) Envelope`
  - `func WriteError(w io.Writer, f ErrorFormat, err error)`:
    - JSON: one compact line.
    - Text: `repsol: <message>`, then `hint: <hint>` when a hint is present.
  - `cli.Version`, `cli.Commit`, `cli.Date` (string vars set by ldflags; `Version`
    defaults to `"dev"`).
  - `type System struct { In io.Reader; Out, Err io.Writer; InTTY, OutTTY bool;
    Getenv func(string) string; ReadPassword func() (string, error) }`
  - `func Run(ctx context.Context, args []string, sys System) int`
  - `func Main() int`: builds `System` from the `os` streams, using `term.IsTerminal` and
    `term.ReadPassword`, and runs under
    `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`.
  - `func readSecretLine(r io.Reader) (string, error)`: reads the first line and strips
    the trailing `\r\n`.

Root behaviour:
- Global flags: `--profile`, `--format`, `--no-input`, `--debug`, `--error-format`.
- `SilenceUsage` and `SilenceErrors` are set.
- `PersistentPreRunE` marks the run as started. Any error returned before that mark
  (unknown command, flag or arguments) becomes `Usage`, code `usage`.
- The `App` is built lazily, on first use by a command:
  - `config.DefaultPaths()`;
  - `ResolveProfile(--profile, REPSOL_PROFILE, cfg)`;
  - `httpx.New("repsol-cli/"+Version, logger)`, where `--debug` gives
    `slog.NewTextHandler(stderr)` at Debug level;
  - base URLs from the env overrides;
  - `Env` from `REPSOL_LOGIN_TOKEN`, `REPSOL_GMID`, `REPSOL_EMAIL` and
    `REPSOL_PASSWORD`;
  - `Telemetry: repsolapi.NewTelemetry()`.
- On error, `finalError(ctx context.Context, err error) error` (in `root.go`) applies
  one rule: when `ctx.Err() != nil` and the category is not `OutcomeUnknown`, the error
  becomes `Interrupted`. `Run` then calls `WriteError` to stderr and returns
  `apperr.ExitCode`.
- Prompts happen only when `InTTY` is true and `--no-input` is not set. Otherwise a
  missing input → `Usage`.

Auth commands:
- `auth login`:
  - Exactly one method:
    - `--har FILE`;
    - `--token-stdin`: `readSecretLine` from stdin, or a `ReadPassword` prompt `Paste
      "glt_…=TOKEN; gmid=GMID": ` on a TTY;
    - `--email ADDR`: the password comes from `--password-stdin`, or from a TTY prompt
      `Repsol password: `. With neither available → `Usage`, code
      `password_required`.
  - No method, or several → `Usage`, code `login_method`, with a hint listing the three
    forms.
  - After success, render the `AuthStatus`. When `!AddressSet`, write
    `hint: run "repsol address set \"STREET NUMBER, TOWN\""` to stderr.
- `auth status [--check]` and `auth logout`.

`script_test.go`:
- `TestMain` calls `testscript.Main(m, map[string]func(){"repsol": func() { os.Exit(Main()) }})`.
- `TestScripts` runs `testdata/script` with:
  - `Setup`, which starts `fakerepsol.New()` (`env.Defer(srv.Close)`) and sets:
    - `REPSOL_API_BASE_URL`, `REPSOL_GIGYA_BASE_URL`, `REPSOL_ADDRESS_BASE_URL`;
    - `XDG_CONFIG_HOME=$WORK/.config`, `HOME=$WORK/home`;
    - `REPSOL_ERROR_FORMAT` unset.

    It stores the server in `env.Values["fake"]`.
  - Cmds:
    - `fake count PATH N`
    - `fake fail PATH STATUS`
    - `fake check online-policy|order`
    - `fake reject-token TOKEN`
    - `filemode PATH OCTAL`

- [ ] **Step 1: Write the failing tests**

```go
func TestResolveFormat(t *testing.T) {
	// ("", true) → pretty; ("", false) → json; ("json", true) → json; ("yaml", false) → Usage
}
func TestEnvelope(t *testing.T) {
	// err := apperr.New(apperr.Refused, "pending_order", "m").WithHint("h")
	// WriteError(&buf, ErrorJSON, err) is one line ending in "\n". Unmarshalled, it has exactly the keys
	//   code, category, exit_code, retryable, message, http_status, endpoint, hint
	// with code "pending_order", category "refused", exit_code 8.
	// WriteError(&buf, ErrorText, err) == "repsol: m\nhint: h\n"
}
func TestReadSecretLineTrimsCRLF(t *testing.T) { // Review Focus 4
	// readSecretLine(strings.NewReader("pw=with=signs\r\nnext")) == "pw=with=signs"
	// readSecretLine(strings.NewReader("")) → Usage error
}
func TestFinalErrorKeepsOutcomeUnknown(t *testing.T) { // Review Focus 3, in root_test.go
	// With a cancelled ctx:
	//   OutcomeUnknown error → ExitCode 9
	//   Network error        → ExitCode 130
	// With a live ctx, a Network error → ExitCode 6.
}
```

`testdata/script/auth.txtar`:

```
exec repsol auth login --har session.har
stdout '"logged_in": true'
stdout '"email": "owner@example.com"'
stderr 'repsol address set'
filemode $WORK/.config/repsol-cli/credentials.toml 0600
exec repsol auth status --check
stdout '"session_valid": true'
stdin session.txt
exec repsol auth login --token-stdin --profile work
stdout '"profile": "work"'
stdin password.txt
exec repsol auth login --email owner@example.com --password-stdin
stdout '"password_stored": true'
! exec repsol --error-format json auth login --email owner@example.com
stderr '"code":"password_required"'
! exec repsol --error-format json auth login
stderr '"code":"login_method"'
exec repsol auth logout
stdout '"removed": true'
-- session.har --
{"log":{"entries":[{"startedDateTime":"2026-10-30T10:00:00Z","request":{"method":"POST","url":"https://accounts.eu1.gigya.com/accounts.getAccountInfo","headers":[{"name":"Cookie","value":"gmid=fake-gmid"}],"cookies":[],"postData":{"mimeType":"application/x-www-form-urlencoded","text":"login_token=fake-token&APIKey=k"}},"response":{"status":200,"content":{"text":"{}"}}}]}}
-- session.txt --
login_token=fake-token; gmid=fake-gmid
-- password.txt --
correct horse
```

`testdata/script/errors.txtar`:

```
! exec repsol frobnicate
stderr 'repsol: unknown command'
! exec repsol --error-format json catalog list
stderr '"exit_code":2'
env REPSOL_ERROR_FORMAT=json
! exec repsol auth status --check
stderr '"category":"auth"'
stderr '"exit_code":3'
! exec repsol --format yaml auth status
stderr '"exit_code":2'
```

The `catalog list` line expects 2 now: before Task 12 it is an unknown command, and after
Task 12 it needs an address. Both are exit 2.

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/output/ ./internal/cli/`
Expected: FAIL with `undefined: ResolveFormat` / `undefined: Main`.

- [ ] **Step 3: Implement `output`, the root, `prompt.go`, the auth commands and `cmd/repsol/main.go`**

`cmd/repsol/main.go` is `func main() { os.Exit(cli.Main()) }`.

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test -race ./internal/output/ ./internal/cli/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/output internal/cli cmd go.mod go.sum
git commit -m "feat(cli): add root command, output formats, error envelope and auth commands"
```

---

### Task 12: Address, catalog, schedule, order, doctor, config, version and skills commands

**Files:**
- Create: `internal/cli/address.go`, `internal/cli/catalog.go`, `internal/cli/schedule.go`,
  `internal/cli/order.go`, `internal/cli/doctor.go`, `internal/cli/configcmd.go`,
  `internal/cli/version.go`, `internal/cli/skillscmd.go`,
  `internal/cli/skills/repsol/SKILL.md`, `internal/cli/skills/embed.go`
- Create: `internal/app/doctor.go`, `internal/app/pretty.go`
- Test: `internal/app/doctor_test.go`, `internal/app/pretty_test.go`,
  `internal/cli/testdata/script/address.txtar`, `order.txtar`, `misc.txtar`

**Interfaces:**
- Consumes: Task 9–10 use cases; Task 11 root, `Render` and `System`.
- Produces:
  - `type DoctorCheck struct { Name string; OK bool; Detail string }`, with JSON keys
    `name`, `ok` and `detail` (omitempty).
  - `type DoctorReport struct { OK bool; Checks []DoctorCheck }`, with JSON keys `ok`
    and `checks`.
  - `func (a *App) Doctor(ctx) DoctorReport`: the checks are `config`, `credentials`,
    `session`, `api`, `address` and `agency`, in that order. A check that depends on a
    failed one fails too, with the detail `skipped: <name> failed`.
  - `PrettyPrint` on `OrderResult`, `AddressView` and `AuthStatus`.
  - `skills.FS embed.FS` and `func skills.List() ([]Skill, error)`, where
    `Skill{Name, Description string}` is read from the SKILL.md frontmatter.

Commands:
- `address show` and `address set TEXT [--floor] [--door] [--details] [--pick]`.
  `address set` takes exactly one argument.
- `catalog list` and `schedule list [--product]`.
- `order create [--product] [--qty=1] [--date] [--pay=delivery] [--dry-run] [--force]`.
- `order list [--limit=10]`, `order last` and `order show REFERENCE`.
- `doctor`: renders the report, and returns `Generic`, code `doctor_failed`, when
  `!OK`.
- `config path` → `{"config": …, "credentials": …}`.
- `config get KEY`, `config set KEY VALUE` and `config list`. The only key is
  `default_profile`, validated with `ResolveProfile` rules. Any other key → `Usage`, code
  `unknown_key`.
- `version` → `{"version","commit","date","go"}`.
- `skills list` → `[{"name","description"}]`.
- `skills get NAME` writes the raw markdown whatever `--format` says. An unknown name →
  `NotFound`, code `skill_not_found`.
- `completion` is cobra's default command, kept enabled.

`SKILL.md` frontmatter: `name: repsol` and `description: Order bottled gas (butane,
propane) from Repsol for the owner, paid on delivery`.

The body contains these statements verbatim, plus the exit-code table and one example per
command:
- "`repsol order create` places a real order, paid on delivery, with no confirmation.
  Use `--dry-run` to preview."
- "If it exits 8 with code `pending_order`, tell the owner which order is pending. Pass
  `--force` only when the owner explicitly asks for another delivery while one is
  pending. A delivery cancelled by phone still shows as pending until its date passes."
- "If it exits 9, do not retry. Run `repsol order last` and report what you see."
- "Never put tokens or passwords on the command line."

- [ ] **Step 1: Write the failing tests**

```go
func TestDoctorAllGreen(t *testing.T) {
	// Session and address stored → OK true, 6 checks all OK.
}
func TestDoctorMissingAddress(t *testing.T) {
	// No address → OK false; the address check fails;
	// the agency check fails with Detail "skipped: address failed".
}
func TestPrettyOrderResult(t *testing.T) {
	// OrderResult{ID: "800000000000000001", Total: 18.89, Payment: "on_delivery", …}.PrettyPrint
	// contains "800000000000000001", "18.89" and "on delivery".
}
```

`testdata/script/order.txtar`:

```
env REPSOL_LOGIN_TOKEN=fake-token
env REPSOL_GMID=fake-gmid
exec repsol address set 'Calle Falsa 1, Pueblo' --floor 2 --door B
stdout '"agency_code": "0000000001"'
exec repsol order create --dry-run
stdout '"dry_run": true'
fake count /api/solicitude 0
exec repsol order create
stdout '"id": "800000000000000001"'
stdout '"payment": "on_delivery"'
! exec repsol --error-format json order create
stderr '"code":"pending_order"'
stderr '"exit_code":8'
exec repsol order create --force
fake count /api/solicitude 2
exec repsol order list
stdout '"pending": true'
exec repsol order show 20000001
stdout '"reference": "20000001"'
! exec repsol --error-format json order create --qty 10 --force
stderr '"exit_code":2'
fake fail /api/solicitude 502
! exec repsol --error-format json order create --force
stderr '"exit_code":9'
stderr 'repsol order last'
fake count /api/solicitude 3
```

`address.txtar` covers:
- `address show` with nothing stored → exit 2, `no_address`;
- an ambiguous `Calle Ambigua` → exit 2 and `1)`;
- `--pick 2` succeeding;
- `schedule list` printing two dates;
- `catalog list` containing `"material_id": "153"`.

`misc.txtar` covers:
- `version` (stdout contains `"version"`);
- `config path`;
- `config set default_profile work`, then `config get default_profile` → `work`;
- `config set foo bar` → exit 2;
- `skills list` → `"name": "repsol"`;
- `skills get repsol` → stdout contains `pending_order`;
- `doctor` without a session → exit 1;
- `completion bash` → stdout contains `bash completion`.

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/app/ ./internal/cli/`
Expected: FAIL with `undefined: DoctorReport`, and the new scripts fail on unknown
commands.

- [ ] **Step 3: Implement the commands, `Doctor`, the pretty printers and `SKILL.md`**

- [ ] **Step 4: Run all tests and confirm they pass**

Run: `go test -race ./...`
Expected: `ok` for every package.

- [ ] **Step 5: Commit**

```bash
git add internal cmd
git commit -m "feat(cli): add address, catalog, schedule, order, doctor, config, version and skills commands"
```

---

### Task 13: MCP server and harness install

**Files:**
- Create: `internal/mcpserver/server.go`, `internal/mcpserver/install.go`,
  `internal/cli/mcpcmd.go`
- Test: `internal/mcpserver/server_test.go`, `internal/mcpserver/install_test.go`

**Interfaces:**
- Consumes: `app.App` use cases, `output.Render`, `output.EnvelopeFor`, `fakerepsol` (tests).
- Produces:
  - `func NewServer(a *app.App, version string) *mcp.Server`. It registers tools with
    `mcp.AddTool[In, any]`. Each tool returns
    `&mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: <the CLI's JSON>}}}`,
    or `IsError: true` with the envelope JSON as text.
  - `func ToolNames() []string` = `catalog_list`, `address_show`, `schedule_list`,
    `order_create`, `order_list`, `order_last`.
  - Tool inputs:
    - `scheduleListIn{Product string}`, JSON key `product` (omitempty)
    - `orderCreateIn{Product string; Quantity int; Date string; DryRun bool}`, with the
      JSON tags `product`, `quantity`, `date` and `dry_run`, all omitempty. There is no
      `force` field, and quantity 0 means 1.
    - `orderListIn{Limit int}`, JSON key `limit` (omitempty)
    - the other tools take an empty struct.
  - `type InstallResult struct { Target, Path, Status string }`, with JSON keys
    `target`, `path` and `status`. Status is `installed`, `updated` or `unchanged`.
  - `func Install(target, command string, args []string, home string, getenv func(string) string) (InstallResult, error)`
  - CLI:
    - `repsol mcp` runs `NewServer(app, Version).Run(ctx, &mcp.StdioTransport{})`.
    - `repsol mcp install claude-code|codex|cursor` uses `os.Executable()` and args
      `["mcp"]`.

Install targets:

| target | file | entry |
|---|---|---|
| `claude-code` | `$CLAUDE_CONFIG_DIR/.claude.json`, else `<home>/.claude.json` | `mcpServers.repsol = {"type":"stdio","command":…,"args":["mcp"]}` |
| `cursor` | `<home>/.cursor/mcp.json` | `mcpServers.repsol = {"command":…,"args":["mcp"]}` |
| `codex` | `$CODEX_HOME/config.toml`, else `<home>/.codex/config.toml` | the text block `[mcp_servers.repsol]` / `command = "<cmd>"` / `args = ["mcp"]` |

**JSON targets.** Decode into `map[string]any`, set the entry, and write with 2-space
indent, keeping the file's mode (0600 for new files). The parent directory is created
with 0700.

**Codex.** Replace an existing `[mcp_servers.repsol]` header and its lines up to the next
`[` header, or append the block after a blank line. The command is written as a TOML
basic string, with `\` and `"` escaped, so Windows paths survive. The result must parse with go-toml
and give back `command`/`args`; otherwise `Usage`, code `codex_config_conflict`, with a
hint to edit by hand.

**Status.** `unchanged` when the entry already has the same command and args.

- [ ] **Step 1: Write the failing tests**

```go
func TestToolsListed(t *testing.T) {
	// In-memory transports (mcp.NewInMemoryTransports); client ListTools →
	// names == ToolNames().
	// The order_create input schema has properties product, quantity, date and dry_run,
	// and no "force".
}
func TestOrderListTool(t *testing.T) {
	// App on fakerepsol, session stored, one order placed → CallTool("order_list", {}) →
	// !IsError; the text unmarshals to []map with [0]["reference"] == "20000001".
}
func TestOrderCreatePendingIsToolError(t *testing.T) {
	// The second CallTool("order_create", {}) → IsError, and the text JSON has
	// code "pending_order" and exit_code 8.
}
func TestInstallClaudeCode(t *testing.T) {
	// The existing .claude.json has {"theme":"dark","mcpServers":{"other":{"command":"x"}}}.
	// Install → status "installed"; theme and other are intact; repsol has type "stdio" and args ["mcp"].
	// Install again → "unchanged". With another command → "updated".
}
func TestInstallCodex(t *testing.T) {
	// The existing config.toml has [mcp_servers.other], plus [mcp_servers.repsol] with command = "old".
	// Install → "updated"; other is intact; exactly one "[mcp_servers.repsol]" line remains;
	// toml.Unmarshal gives command == the new path.
}
func TestInstallCursorCreatesFile(t *testing.T) {
	// No file → "installed"; the file is created with mode 0600.
}
```

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./internal/mcpserver/`
Expected: FAIL with `undefined: NewServer`.

- [ ] **Step 3: Implement the server, install and the `mcp` commands**

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test -race ./internal/mcpserver/ ./internal/cli/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/mcpserver internal/cli go.mod go.sum
git commit -m "feat(mcp): add MCP server tools and harness install"
```

---

### Task 14: Docs, CI, release, live checks and a PII scan

**Files:**
- Create: `README.md`, `AGENTS.md`, `docs/cli-spec.md`, `docs/adr/0001-sessions.md`,
  `docs/adr/0002-autonomous-ordering.md`, `.github/ISSUE_TEMPLATE/api-changed.md`
- Create: `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `.goreleaser.yaml`,
  `.golangci.yml`
- Create: `internal/live/live_test.go` (`//go:build live`)

**Interfaces:**
- Consumes: the finished CLI. The live test uses the `gigya`, `repsolapi` and `httpx`
  clients directly.
- Produces: no code interfaces.

**Docs content**
- **`README.md`.** Unofficial-project disclaimer at the top: no affiliation with Repsol,
  no Repsol branding. Install (`go install …/cmd/repsol@latest`, release binaries). Login
  walkthrough:
  1. In Chrome, open DevTools Settings → Preferences → Network and enable "Allow to
     generate HAR with sensitive data".
  2. Open the Network panel, log in at `https://pidetubombona.repsol.es/` and use
     "Export HAR (with sensitive data)".
  3. Run `repsol auth login --har FILE`.
  4. Delete the HAR, because it contains your password.

  Then the alternatives `--token-stdin` and `--email`, followed by `address set`, a first
  `order create --dry-run`, agent setup (`repsol mcp install …`, `repsol skills get
  repsol`), and the exit-code table.
- **`AGENTS.md`.** Domain vocabulary:
  - solicitude;
  - `referenceSAC` vs `externalId`;
  - login token + `gmid`;
  - agency;
  - material id.

  Contributor rules:
  - no live writes, ever;
  - fixtures synthetic only;
  - never log secrets;
  - run `make test lint`.
- **`docs/cli-spec.md`.** Every command, its flags, its JSON output keys (copied from the
  structs in Tasks 8–12), the envelope, and the exit codes.
- **ADRs.** `0001`: sessions are token + gmid import, plus an opt-in stored password with
  re-login. `0002`: autonomous ordering with the pending guard, `--force`, and no write
  retry.
- **`api-changed.md`.** Asks for the command, the exit code and the envelope, never
  tokens or HAR files.

**CI and release**
- `ci.yml`: on push and pull_request, `actions/setup-go` with `go-version: '1.26'`, then
  `go vet ./...`, `go test -race ./...` and `golangci/golangci-lint-action`.
- `release.yml`: on tags `v*`, runs goreleaser.
- `.goreleaser.yaml` (`version: 2`):
  - builds `./cmd/repsol` with `CGO_ENABLED=0` for darwin, linux and windows on amd64 and
    arm64;
  - `-trimpath`, plus ldflags `-s -w -X …/internal/cli.Version={{.Version}}
    -X …/internal/cli.Commit={{.Commit}} -X …/internal/cli.Date={{.Date}}`;
  - archives and `checksums.txt`.
- `.golangci.yml`: v2 format, enabling `errcheck`, `govet`, `staticcheck`,
  `ineffassign`, `unused`, `misspell` and `gofmt`.

**Live suite**
- `TestLiveReadOnly` skips unless `REPSOL_LOGIN_TOKEN` and `REPSOL_GMID` are set.
- The address comes from `REPSOL_LIVE_ADDRESS`, default `Calle de Alcala 1, Madrid`.
- The `httpx.Client`'s transport is wrapped in a RoundTripper that fails the test on any
  request other than a GET or a POST to `/accounts.getAccountInfo`.
- Checks:
  - `AccountInfo` succeeds;
  - `OrderHistory(5, 0)` decodes;
  - the autocomplete → (geocode) → `Agency` path gives an agency code;
  - `Catalog` contains material 153;
  - `DeliverySchedule` has at least one date.
- It logs only counts and booleans, never values.

- [x] **Step 1: Write the live test, then confirm it compiles and skips**

Run: `go vet -tags live ./internal/live/ && go test -tags live ./internal/live/ -v`
Expected: `--- SKIP: TestLiveReadOnly`.

- [x] **Step 2: Write the docs, workflows, goreleaser and golangci config**

- [ ] **Step 3: Verify the build and the test suite**

Run: `make test && make lint && make build VERSION=0.1.0 && ./bin/repsol version`
Expected: tests `ok`; `go vet` clean; the version output shows `"version": "0.1.0"`.

- [ ] **Step 4: Scan the repository for the owner's personal data**

Write theThe scanner already exists outside the repository, in the planning session's scratchpad
(`scripts/pii_scan.py`). If it is missing, write it there again: it must never be
committed. It:
- loads the HAR;
- collects every value of 6 or more characters under the personal keys (UID,
  UIDSignature, login_token, password, loginID, email, names, phones, street, placeId,
  fullAddress, autocompleteContent, coordinates, externalId, referenceSAC, agency name,
  autocomplete input) and the gmid and `glt_` cookies;
- counts how many of those values appear in each file it is given;
- prints only file names and counts, never values.

Run: `python3 -I <scratchpad>/scripts/pii_scan.py ~/Downloads/pidetubombona.repsol.es.har $(git ls-files)`

Expected: the only matches are generic words. To check, print the doc context around each
match with the value masked as `<V>`. When the docs were written, the matches were
`Calle`, `string`, `subscription`, the catalog product names, and the owner's GitHub
handle in the module path. Remove anything else from the repository before committing.

tep 5: Commit**

```bash
git add README.md AGENTS.md docs .github .goreleaser.yaml .golangci.yml internal/live
git commit -m "docs: add README, agent guide, CLI spec, ADRs, CI, release config and live read-only checks"
```

---

## Owner acceptance (after the branch is reviewed)

The owner runs these steps, or watches them run. Nothing here places an order:

1. `make build`, then `./bin/repsol auth login --har ~/Downloads/pidetubombona.repsol.es.har`,
   then `./bin/repsol auth status --check`.
2. `./bin/repsol auth login --email …`. This is the first real password login (spec §5
   unverified item 1). If Gigya asks for a captcha, the session import stays the
   supported path.
3. `./bin/repsol address set "…"`, then `./bin/repsol order create --dry-run`.
4. `make live` with `REPSOL_LOGIN_TOKEN` and `REPSOL_GMID` set.
5. Delete the HAR from Downloads.
6. Place the first real order only when the owner actually wants a bottle.
