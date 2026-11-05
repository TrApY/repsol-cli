# 0002 — Autonomous ordering with a pending guard and no write retry

Date: 2026-10-30
Status: accepted

## Context

The owner wants the agent to order gas without any confirmation: "pídeme una
bombona de butano y la pago al repartidor". A prompt on every order would put
the owner back in the loop for something they can pay for on delivery.

The risk of removing the prompt is a duplicate delivery: an agent that retries,
or a command that runs twice, sends two bottles to the same address. There is
no way to cancel or modify an order through the API, so the only brake is
preventing the second write.

## Decision

1. **No confirmation prompt.** `repsol order create` writes the order, on a
   terminal as well as in a script. `--dry-run` runs every read, validates the
   cart with Repsol and writes nothing.

2. **A pending-order guard before every write.** The CLI reads the newest 20
   history entries and refuses with exit 8 when one of them is pending for the
   same address. Pending means `deliveryDate` is today or later in
   `Europe/Madrid`; same address means equal `agency.agencyCode` and
   `address.localityCode`. The refusal names the pending order's reference and
   date.

3. **`--force` is for explicit owner intent only.** It skips the history guard
   (and only that one: see ADR 0003 for the remembered Waylet payment). The MCP
   `order_create` tool does not expose it, and the embedded agent
   documentation tells agents to report the pending order back to the owner and
   to pass `--force` only when the owner explicitly asks for another delivery
   while one is pending. A delivery cancelled by phone still looks pending
   until its date passes; this is stated in the error hint and in the agent
   docs.

4. **Never retry a write.** If `/solicitude` or `/order` times out, resets,
   fails with a 5xx or returns an unreadable body, the order may or may not
   have been placed. The CLI does not retry, does not guess, and exits 9
   (`order_outcome_unknown`) with the instruction to run `repsol order last`
   before trying again. The write is sent exactly once.

5. **Reads may retry.** GETs retry on transport errors, 429 and 5xx, three
   attempts with a backoff of 250 ms, 1 s and 4 s.

## Consequences

- The owner can leave an agent running without watching it.
- A duplicate delivery is possible only when the owner explicitly asks for one,
  or when the owner wants a second bottle on the same date. Both are visible in
  the history.
- The guard is scoped by address, so a pending delivery at one address never
  blocks an order for another town on the same account.
- Exit 9 is a state the caller must handle: the agent documentation says "do
  not retry, run `repsol order last` and report what you see".
