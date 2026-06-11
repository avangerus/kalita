# ADR-001: In-memory projections in v0

**Status:** Accepted (2026-06-12)

**Context:** HLD §3.4 and EVENT-STORE-v0 §5 assumed projection tables in
PostgreSQL updated in the same transaction as the event append. Building and
migrating per-entity SQL projections is a large share of week-3..8 effort.

**Decision:** In v0 the journal is the only persistent state. Projections
(current records, registry, task/approval queues) live in process memory and
are rebuilt by replaying the journal at startup. PostgreSQL stores events only.

**Alternatives considered:**
1. SQL projection tables per entity (HLD original) — rejected for v0: migration
   engine complexity lands in week 3 instead of week 8, doubles surface area.
2. Embedded KV cache (bbolt) for projections — rejected: a second source of
   truth with invalidation bugs; replay is simpler and always correct.

**Expected outcome (falsifiable):** node restart with a 100k-event journal
rebuilds projections in under 5 seconds on commodity hardware. If real packs
blow past this budget, this ADR is superseded by SQL projections (v1).

**Consequences:** query scale is bounded by RAM (fine for the SMB profile,
one client = one node); `simulate` (layer 2) becomes a free byproduct; the
PG projection path returns in v1 only where measurements demand it.
