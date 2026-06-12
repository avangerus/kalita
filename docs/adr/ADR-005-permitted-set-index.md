# ADR-005: Secondary indexes for the permitted-set query

**Status:** Accepted (2026-06-12)

**Context:** Row-level ABAC has a scale trap. A role scoped `read X where owner =
$me` (or `where region = $me.region`) means "give me only the records I may
see." The naive evaluation scans every row of `X` and runs the permission check
on each — O(table) per query, regardless of how few rows the actor may actually
see. For a directory-style entity (positions, clients, documents) with a master
dictionary and per-row permissions, this is the failure the founder named: "they
add row-level permissions and the query goes O(n) and dies." Projections are
in-memory (ADR-001), so there is no SQL planner or RLS to lean on — the kernel
must narrow the set itself.

**Decision:** Maintain lazy per-entity secondary indexes over the fields any
role's `read`/`full` rule scopes by `= $me[.attr]` (see `internal/engine/index.go`).
When a query's read scope is index-backed, `candidateIDs` serves the candidate
set straight from that field's bucket — the actor's share — and the loop runs
`can()` only over those rows. The index is:

- **a superset, never an authority.** `candidateIDs` returns a SUPERSET of the
  permitted rows (the bucket ⊇ its satisfying set); `can()` still runs per
  candidate as the final check, so the index can narrow but never leak.
- **conservative by construction.** An unconditional allow, an `or` disjunction,
  or a scope on a non-indexed field returns `(nil, false)` → honest full scan.
  Correctness never depends on the index existing.
- **cheap to keep coherent.** Indexes are built lazily on first query, carry a
  record-count staleness backstop, and are dropped on any write to the entity
  (the next query rebuilds). Ideal for read-often, write-rarely directories.

**Alternatives considered:**
1. Push the filter to SQL / Postgres RLS — rejected for v0: projections are
   in-memory (ADR-001); there is no SQL layer to push into. Returns in v1 with
   the tiered-storage path (ADR-002) for entities too large for RAM.
2. Always scan, document a row cap — rejected: the cap is exactly the SMB
   directory size we must serve; a scan-only kernel makes ABAC a footgun.
3. Eager indexes maintained on every write — rejected: write amplification for a
   read-optimization; lazy build + drop-on-write is simpler and self-correcting.

**Expected outcome (falsifiable):** answering one actor's index-backed
permitted-set query examines only that actor's share of the rows, independent of
table size. Measured (`BenchmarkPermittedSet`, `internal/engine/scale_test.go`),
100k-row table, 1000 owners (100 rows each), commodity desktop (i7-13700KF):

| query | per op | memory |
|-------|--------|--------|
| direct `where owner = $me` (index) | **~0.21 ms** | 210 KB |
| ref-path `where project.owner = $me` (index) | **~0.34 ms** | 239 KB |
| naive full scan (the O(n) it replaces) | ~70 ms | 78 MB |

~200–330× faster, ~330× less memory, and the scoped cost tracks the owner's
share, not the table. Both the direct scope and the harder **indirect-ownership**
scope ("tasks in projects I own") are served: the ref-path index resolves the
target ids the actor owns (`Project where owner = $me`) from the target's index,
then unions the local ref buckets (`Task.project`) — a two-level join that stays
O(owned). `TestPermittedSetNarrowsAtScale` and `TestRefPathPermittedSetNarrows`
assert the deterministic guarantee: the candidate set is exactly the owner's
(in)direct rows, leaks nothing, and an unscoped role still falls back to a scan.

The ref-path index requires the TARGET index to be complete, so `core.*` targets
(projected from the identity registry, not in `records`) deliberately fall back
to a scan rather than risk omitting a permitted row.

**Consequences:** ABAC scopes of the form `field = $me[.attr]` and
`ref.field = $me[.attr]` are scale-safe in v0 without a database. Remaining
edges, correct but not yet maximally narrowed: multi-term `and` (uses the first
indexed term's bucket — already a valid superset, just not the intersection),
`or` disjunctions, and `core.*`-targeted ref-paths (all fall back to a scan).
Horizontal sharding (one client = one node, ADR-001) keeps per-node tables within
the RAM envelope this index assumes.
