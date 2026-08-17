# Kalita — Threat Model and Security Status

Status: v1.0 release candidate (staff-only pilot, 2026-08). An honest document:
what holds, what does not, what to fix before which stage. Security in kalita is
a product (agents are trusted with work), not an appendage to it. The current
24-surface threat model with per-threat mitigation and residual risk lives in
docs/forge/THREAT_MODEL.md (Milestone G verdict: RELEASE CANDIDATE ACCEPT).

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
- **T3 (transport):** REST and MCP authenticate by bearer token through the
  registry — the same model for humans and agents, no parallel access path. The
  `X-Actor-*` dev headers are OFF by default, reachable only behind the explicit
  `--insecure-dev-auth` flag (logged as a warning) for local dev/tests. TLS is
  served from `--tls-cert`/`--tls-key`; the node **refuses to bind a non-loopback
  address over plaintext** unless `--insecure-http` is passed (which then warns
  that tokens travel in the clear). Browser requests are same-origin enforced
  (cross-origin `Origin` → 403) with `nosniff` / `X-Frame-Options: DENY` /
  `Referrer-Policy: no-referrer` / a `'self'` CSP on every response.
- Expressions fail-closed: an unevaluable condition = deny, not allow.

## 3. Known Gaps (by priority)

### P0 — Fix BEFORE any deployment outside localhost
**All three original P0 items are now closed** (see T3 above); they are kept here
for the audit trail, struck through, with the mechanism that closed each.
1. ~~**REST dev headers (X-Actor-Id/Role)** — impersonation.~~ **Closed:** bearer
   tokens are the only ambient auth; dev headers require `--insecure-dev-auth`
   (off by default, warned). WebAuthn for non-repudiation remains P1 (#4).
2. ~~**No TLS.**~~ **Closed:** `--tls-cert`/`--tls-key`, and the node refuses to
   serve plaintext on a non-loopback address without an explicit `--insecure-http`
   override (which warns). Autocert/Let's Encrypt is deferred (on-prem nodes
   usually have no public domain; cert files or a TLS proxy are the path).
3. ~~**CORS/CSRF not configured.**~~ **Closed:** same-origin enforcement +
   security headers in the `Secure` middleware on the whole handler. Auth is
   header-bearer (not cookie), so classic CSRF does not apply.

### P1 — Before the first external user (now the front line)
4. ~~**WebAuthn for humans** — currently human decisions are signed only when a
   registered key is present; a token ≠ non-repudiation. A passkey makes the
   signature evidential (key on device, Event Store spec §4).~~ **Closed:**
   `internal/identity/webauthn.go` (HLD-V1 H1/LLD-02) — passkeys registered via
   `actor.credential_registered`, approvals signed by the device key and verified
   offline.
5. ~~**Secrets:** PG DSN in env/.env; needs a config file with 0600 permissions
   and docker secrets support.~~ **Closed:** `internal/secrets` (LLD-18 §1) —
   0600 config file / docker-secrets `<NAME>_FILE` fallback; fail-fast on weak
   permissions, secret values never logged.
6. ~~**Auth-failure logging** — to memory/log with IP rate-limit (not to the log,
   see T6), alert on N failures.~~ **Closed:** `internal/identity/authfail.go`
   (LLD-10 §4) — IP rate-limit with aggregated `auth.throttled` events, not one
   per attempt.
7. ~~**Lockout/invalidation:** no emergency command to revoke all tokens.~~
   **Closed:** `internal/identity/lockdown.go` (LLD-10 §3) — `kalita lockdown`
   rejects every token except one emergency admin (hash-only in the journal).

### P2 — Before enterprise sales
8. ~~Crypto-shredding of payloads (reserved in Event Store §6).~~ **Closed:**
   `internal/engine/records.go` (`sealPII`) + `internal/pii` (LLD-16) —
   per-subject-key encryption of declared `pii subject=…` fields on write;
   destroying the key renders the data unreadable while the hash chain stays
   intact.
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
