# 0001 — Sessions: token import plus an opt-in stored password

Date: 2026-10-30
Status: accepted

## Context

Repsol's `pidetubombona` web application authenticates against Gigya (SAP
Customer Data Cloud). Two things make a session:

1. a Gigya **login token** (`st2.…`), and
2. the **`gmid` device cookie** the token is bound to.

This was verified live on 2026-10-30: `accounts.getAccountInfo` with the token
alone answers `errorCode 403005 "Unauthorized user"`, and the same call with
`Cookie: gmid=<value>` succeeds. `ucid`, `hasGmid`, `Origin` and `Referer` are
not needed.

The two values live on different domains: the token in a `glt_<APIKey>` cookie
on `pidetubombona.repsol.es`, the `gmid` on the Gigya domain. So copying the
site's cookies is not enough, and a stored token alone is not enough either.

## Decision

A session is imported, never created silently:

- `repsol auth login --har FILE` reads the newest Gigya request in a browser
  HAR that carries both values. It reads nothing else from the file and never
  stores it. The HAR contains the owner's plain-text password, so the README
  tells the owner to delete it.
- `repsol auth login --token-stdin` reads one pasted line in cookie syntax,
  `glt_<APIKey>=<token>; gmid=<gmid>`, with a no-echo prompt on a terminal.

The password is stored only when the owner opts in, with
`repsol auth login --email ADDRESS --password-stdin`. It is kept in the `0600`
credentials file, never in argv, logs, error envelopes or MCP responses.

With a stored password, a rejected or expired token is renewed transparently:
the CLI logs in again and saves the new token. Without one, the command fails
with exit 3 and a hint to re-import the session.

A password imported from a HAR is ignored: the HAR is a session import, not a
credential import.

## Consequences

- The CLI works unattended once the owner has imported a session once.
- The owner can revoke access by removing the stored credentials
  (`repsol auth logout`).
- The stored password is the only secret on disk besides the token. It is why
  the credentials file is written atomically under a file lock, with mode
  `0600`, and why concurrent runs never lose another profile's entry.
- The email + password path was never exercised during development, to avoid a
  login without the owner present. It is the first thing the owner tries, and
  if Gigya demands a captcha, the session import stays the supported path.
