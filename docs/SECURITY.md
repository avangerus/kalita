# Kalita — Threat Model and Security Status

Status: v0 (2026-06-13). An honest document: what holds, what does not, what to
fix before which stage. Security in kalita is a product (agents are trusted with
work), not an appendage to it.

## 1. Threat Model — Who We Are Protecting Against

| Threat | Vector |
|---|---|
| T1. Hostile/broken agent | valid token, attempts to exceed its rights, lie, corrupt data |
| T2. Stolen agent key/token | attacker acts as the agent |
| T3. Network attacker on LAN | interception, impersonation, token brute-force |
| T4. Insider with Postgres access | retroactive substitution/deletion of history |
| T5. Author of a hostile pack | DSL as an attack channel |
| T6. Flood/DoS | log overflow, resource exhaustion |

## 2. What Already Holds (by design)

- **T1 — the platform's primary bet:** default deny; deny > allow; an agent role
  without deny does not compile; the workflow field cannot be written directly;
  HITL cannot be bypassed or fast-forwarded (a transition without a signature
  does not exist); mutations without a basis are rejected; reports are reconciled
  against facts (facts=0 is visible); permissions are enforced in the core —
  UI/MCP have no access logic of their own.
- **T2 (partially):** no anonymous actors; tokens are shown only once, the log
  stores only sha256; comparison is constant-time; disabling an actor
  immediately kills both the token and its signatures; key rotation is a log
  event.
- **T4:** append-only log + SHA-256 chain; UPDATE/DELETE are blocked by a DB
  trigger; checkpoints are signed with the node key and can be exported
  externally — a log rewritten after the fact is caught by offline verification.
  Decision signatures (Ed25519) are verifiable without server access.
- **T5:** the DSL contains no code — a pack cannot execute anything; outbound
  webhooks are declaration-only (egress is visible from the spec); applying a
  pack requires a signature from a human with the approver role, migration plan
  in hand; destructive changes are inexpressible.
- **T6 (partially):** rate limit per actor in MCP; failed authentication attempts
  are intentionally NOT written to the log (otherwise token brute-force = log
  flood).
- Expressions fail-closed: an unevaluable condition = deny, not allow.

## 3. Known Gaps (by priority)

### P0 — Fix BEFORE any deployment outside localhost
1. **REST dev headers (X-Actor-Id/Role)** — impersonation by anyone who can
   reach the port. Remove; for humans — token-based sessions (same as agents)
   until WebAuthn arrives.
2. **No TLS.** The node must either set up TLS itself (autocert/files) or
   strictly document a reverse-proxy requirement; tokens over plaintext HTTP = T3.
3. **CORS/CSRF not configured** — same-origin mode is mandatory before the UI
   is released.

### P1 — Before the first external user
4. **WebAuthn for humans** — currently human decisions are signed only when a
   registered key is present; a token ≠ non-repudiation. A passkey makes the
   signature evidential (key on device, Event Store spec §4).
5. **Secrets:** PG DSN in env/.env; needs a config file with 0600 permissions
   and docker secrets support.
6. **Auth-failure logging** — to memory/log with IP rate-limit (not to the log,
   see T6), alert on N failures.
7. **Lockout/invalidation:** no emergency command to revoke all tokens.

### P2 — Before enterprise sales
8. Crypto-shredding of payloads (reserved in Event Store §6).
9. External checkpoint anchoring (client backup, notary).
10. Dependency scanning in CI (govulncheck) + release signing.
11. WASM sandbox escape hatch with capability model (the contract already requires it).

## 4. Rules That Must Not Be Violated During Development

1. No interface gets its own access model — only the core does.
2. New events — additive only; the log is never "cleaned".
3. Every new outbound channel is declared in the DSL (visible egress).
4. A permission evaluation error = deny (fail closed), always.
5. A secret never appears in the log in any form other than a hash.
6. **On-premise by default: no cloud services in the core or packs.**
   Dashboards, analytics, search, BI — computed and rendered within the
   perimeter. External models/services — only through a worker with a declared
   egress (rule 3) and by explicit client decision. References like "like Power BI"
   describe the genre, NOT a dependency.
