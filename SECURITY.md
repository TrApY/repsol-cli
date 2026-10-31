# Security

## Reporting a vulnerability

Report a vulnerability privately through
[GitHub's private vulnerability reporting](https://github.com/felipe9962/repsol-cli/security/advisories/new),
not in a public issue. Describe the problem and how to reproduce it with
invented values.

Never send a HAR file, a login token, a `gmid` cookie, a signature, a password
or your personal data (NIF, address, name, phone, order references), not even
privately. If a report needs a request or a response, replace every value with
a made-up one first.

## What the CLI stores

Everything lives in one directory under your user config directory;
`repsol config path` prints it. The directory is `0700`.

| file | mode | contents |
|---|---|---|
| `credentials.toml` | `0600` | per profile: the login token, the `gmid` cookie, the account UID and first name, and, only if you log in with `--email`, the email and password |
| `config.toml` | `0644` | per profile: the delivery address and the default profile |

`repsol auth logout` removes a profile's credentials. `REPSOL_LOGIN_TOKEN` and
`REPSOL_GMID`, or `REPSOL_EMAIL` and `REPSOL_PASSWORD`, supply a session without
writing it to disk.

## What the CLI never does

- It never logs or prints the login token, the `gmid` cookie, the password, the
  signature, the UID or the personal data in the Gigya account block. The HTTP
  transport redacts them from `--debug` logs, and error messages and MCP
  responses leave them out.
- It never takes a secret on the command line, where other processes can read
  it: passwords and session lines come from stdin or a prompt.
- It never retries an order request. A failed request may still have reached
  Repsol, so the CLI exits 9 and asks you to check `repsol order last`.
- It talks only to the three backends the web application itself uses:
  Repsol's API, its Gigya login service and its address lookup service. There
  is no telemetry.

## HAR files

A HAR exported "with sensitive data" holds your plain-text password, your live
session and your personal data. Import it with `repsol auth login --har`, then
delete it.
