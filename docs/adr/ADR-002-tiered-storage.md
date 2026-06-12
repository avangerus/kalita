# ADR-002: Tiered Storage — One Journal, Many Projections

**Status:** Accepted (2026-06-13). Complements ADR-001 (its revision condition
triggers here in a controlled manner, not as an emergency).

**Context:** the platform must handle large volumes of events — the level of
mature enterprise event sourcing, not ClickHouse scale, but with proper
projections. Founder's question: can different modules use different databases?

**Decision — four rules:**

1. **One journal and only in PostgreSQL.** The source of truth is not
   replicated. Scale: monthly partitioning (planned), seq index, append-only —
   PG comfortably handles hundreds of millions of events. Projection-head
   snapshots + tail replay — for fast startup with large journals (v1).
2. **Core projections are two-tiered.** Hot data — in RAM (as today); an entity
   that outgrows the threshold (~1M records or the startup budget from ADR-001)
   moves to a SQL projection in PG (jsonb table, updated in the same transaction
   as the append — the original HLD §3.4 path). The journal does not change,
   behavior does not change, only the projection storage changes.
3. **Different databases — yes, but at the module/worker level, not the core.**
   `Store.Since(seq)` is the public subscription mechanism: any worker builds
   ITS OWN projection in ITS OWN database (Qdrant in knowvault — already the
   case; analytics → DuckDB/ClickHouse; full-text → whatever). Ownership rules:
   a module database (a) is populated only by replaying the journal, (b) is
   disposable — losing it causes no loss of truth, (c) belongs to the worker;
   the core has no knowledge of it.
4. **Journal = internal bus.** No separate brokers (Kafka, etc.) in v0/v1:
   watermarks on seq provide exactly-once consumers; federation (HLD §8a) runs
   on the same mechanism.

**Alternatives considered:** (1) SQL projections for everything from the start —
rejected: projection migration complexity before it is needed, RAM tier covers
95% of packs; (2) embed ClickHouse for analytics in the core — rejected: wrong
domain, rule 3 handles this via a worker; (3) Kafka as a bus — rejected: the
journal is already a reliable ordered bus; a second broker = a second source of
truth.

**Expected outcome (falsifiable):** a node with 10M events and 1M records for
one entity: startup < 30 sec (with snapshot), p95 read < 50ms, write — hundreds
of events/sec. Fails to meet targets — revisit tier 2 (projection sharding).
