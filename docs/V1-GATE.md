# Gate 1.0 — What Separates Us from a Sellable Version

Definition of 1.0: a paying client lives on a node within their own perimeter
without our daily involvement. Everything in the list is derived from this
definition. Recorded 2026-06-14. Estimates are in weeks of pipeline pace.

## Blockers (cannot sell without these)

### Operations (≈2 wks)
- [ ] **kalita upgrade**: binary update with system-structure migration,
      rollback to the previous version (T5)
- [ ] **Backup/restore as a procedure**: pg_dump + offline chain verification
      + node restore — operator guide with drills
- [ ] **Destructive migrations** via manual procedure with export (v0
      forbids them — a client will need to rename a field within month 1)
- [ ] Benchmark suite in CI (promised in the storm discussion): regressions
      are caught by a number on push

### Security P1 (≈2 wks)
- [ ] **WebAuthn/passkey** human signatures (token ≠ non-repudiation; Event
      Store spec §4 requires this)
- [ ] Secrets: config file 0600 + docker secrets (instead of env)
- [ ] Auth-failure log (to log with IP rate-limit) + `kalita lockdown`
      (emergency revocation of all tokens)
- [ ] govulncheck in CI + signed releases

### Core Completeness (≈3 wks)
- [ ] **core pack**: core.User as a real system entity (currently a stub
      string), attachments; reference data (T9) — data-pack format
- [ ] **file fields** (upload to content-addressed storage) — v0.2 UI remainder
- [ ] **i18n labels**: a RU client must see their locale, not the DSL name —
      label annotations in the ui block or a pack dictionary (grammar stays EN)
- [ ] **Minimum aggregates**: count/sum by status for a director dashboard —
      no full `metric` language, but weekly numbers must exist
- [ ] **Escape hatch v1**: WASM component with typed contract —
      minimal (one function in→out); otherwise DSL ceiling = wall for the
      first client

### Deployment Product (≈2 wks, from T11 — these are the reason to buy)
- [ ] **Shadow mode**: role operates in "dry-run" proposal mode — removes
      the fear of going live
- [ ] **"Week in numbers" report + cost of work in money** (role rate ×
      man-hours) — sells renewals without a salesperson
- [ ] **Egress report** from the log — perimeter compliance artifact

### Open-Core Hygiene (≈0.5 wks)
- [ ] License (decision: core Apache-2.0 / packs and enterprise features
      separately — discuss with founder), CONTRIBUTING, SECURITY.md contact
- [ ] Pack author guide + worker guide (modeled on indexer.py)

## Nice to Have in 1.0, Not a Blocker
- [ ] KnowVault full (embeddings + Qdrant + search_perimeter) — module,
      moves at its own pace; the pilot needs it before 1.0
- [ ] Trust dial (role autonomy levels) — may come in 1.1
- [ ] Pack README generation from DSL (marketplace showcase)
- [ ] Projection snapshots (based on benchmark measurements; ADR-002 threshold)

## Deliberately AFTER 1.0
Federation (remote[...]), marketplace, pre-signature simulation (layer 2),
agent ratings, crypto-shredding, external anchoring, Telegram front (T1 —
unless the pilot requires it sooner), production calendar/business days.

## Summary
≈9-10 weeks of pipeline pace to 1.0. Execution order: operations →
security P1 → core completeness → deployment product (in parallel with the
pilot, which sorts priorities better than any plan).
