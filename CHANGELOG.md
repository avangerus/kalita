# Changelog

## v1.0.0

First release. Scope and release criteria: `docs/DECISIONS.md` D-018; the path there, stage by stage
(`v1.0.0-rc.3` … `v1.0.0-rc.7`): `docs/STATUS.md`.

### Product since v1.0.0-rc.2

- Self-building product: Ask Kalita (builder worker, `packs/builder`, business frameworks) and the web
  shell — Home, Ask, Company Model, Changes, Integrations.
- English-first UI, frameworks and reports.
- Durable webhook delivery: per-firing delivery identity, retries, dead-letter queue with operator retry,
  per-attempt receipts, per-destination secrets (D-011).

### Correctness

- A task lease is kept when its expiry cannot be journaled; the error is logged and counted.
- `agent_runner` reports what the node accepted: a refused completion is `rejected`, not `done`.
- `RotateKey` and `Disable` are serialized with the rest of the identity registry.
- The dead-letter queue and `kalita_egress_dead_lettered` no longer show deliveries that were already
  retried; retrying one is refused (D-019).
- `on update` rules also fire after workflow moves (act, granted approval); `field changed` and
  `field changed to V` work in their conditions; a model with an automation condition the node cannot
  parse is refused at load (D-020).
- Deciding an approval whose transition was removed from the model is refused before anything is
  journaled, instead of applying a guard-less transition (D-020).

### Security

- Egress allowlist compares canonical hosts (IPv6 included), refuses non-ASCII (IDN) hosts and dials
  only vetted addresses: public and loopback by default, private networks with
  `KALITA_EGRESS_ALLOW_PRIVATE=1` (D-021).
- Credentialed webhooks are never sent over cleartext `http://` to a non-loopback host.

### Operations

- Signed releases: archives for linux/windows/darwin with the UI embedded, `SHA256SUMS` signed keyless,
  multi-arch image `ghcr.io/avangerus/kalita`; `kalita upgrade` verifies the signature (D-022).
- Record history, rewind/diff, revert and the task fact check read one record's events through an index
  instead of the whole journal.
- Automated release acceptance on a real node and PostgreSQL (`tests/acceptance`), run in CI.

### Known limitations

Listed in `docs/STATUS.md` → «Известные ограничения»: staff-only portal (no consumer auth), no browser
tests for the UI, federation at worker level, prompt-injection detection is a tripwire, single node.
