# Revision of the type system and primitives to the level of "a full-featured application" (like Jira)

Founder request (2026-06-14): enrich with data types, storage types,
and functions — to enable building full-featured applications, like Jira on the entity-engine
of OFBiz.

Rule to avoid breaking guarantees: every new primitive must pass the T8 test —
either it is fully checkable by the compiler (then it goes into DSL/core), or it requires
arbitrary computation (then it is a worker or WASM escape hatch), but NOT
a loose expression that an agent could corrupt.

## What Jira actually builds from primitives — and what we are missing

| Jira capability | What is missing in kalita v0 | Level |
|---|---|---|
| Custom fields of different types | richer scalars | DSL type |
| User/group as a field | real core.User + groups | core pack |
| Issue links (blocks/relates) | many-to-many references + link types | DSL |
| Comments, history, attachments | comment primitive, history (exists), file (exists) | core pack |
| Labels, components | tags/multiselect | DSL type |
| Boards, swimlanes, filters | board exists; need saved filters, swimlanes | DSL/UI |
| Formulas (story points roll-up) | aggregate computations | worker/function |
| Notifications, @mentions | notify (basic exists), mentions | core pack |
| Dashboards, reports | aggregates (in V1-GATE) | DSL/worker |
| SLA, escalations | stuck (exists), business calendar | DSL+data pack |

## A. Richer SCALARS (DSL, checkable by compiler) — accepted
The closed list is extended: `decimal(p,s)` (more precise than money), `duration`
(2d4h — normalized), `email`/`url`/`phone` (validated strings),
`json` (constrained, for arbitrary metadata without a schema — use carefully),
`color`, `geo`(lat,lng), `percent`. Each — with its own validation in the core.
**tags: `array[string]`** and **multiselect `array[enum[...]]`** — Jira labels/
components. All are added additively; they do not break existing packs.

## B. LINKS (DSL) — accepted, this is the heart of Jira
- `array[ref[Entity]]` already exists (one-directional many-to-many).
- **Named bidirectional links** with a type are needed:
  `link Task <-> Task as blocks/blocked_by` — Jira issue links. Checkable:
  the link type is declared, the reverse side is generated. This is a new top-level
  `links:` block, not an expression → guarantees are preserved.

## C. STORAGE BACKENDS (not in grammar — config/worker) — accepted per ADR-002
The founder is right about "storage types", but they do NOT belong in the DSL:
- blob: Disk (exists) | S3/MinIO (BlobStore interface — implement the adapter)
- projections: RAM (exists) | SQL (ADR-002 threshold) | external sink workers
- vectors: Qdrant (exists) — worker
Storage choice is a node deployment/config concern and worker contracts, not the
pack author's business. The DSL stays about MEANING, not infrastructure.

## D. FUNCTIONS — the sharpest question; this is the boundary
Jira-style formulas (roll-up story points, computed fields) are desirable, but arbitrary
functions in the DSL = end of guarantees (an agent could write anything). Solution — TWO kinds:
1. **Declarative aggregates in the core** (checkable): `computed = sum(children.points)`,
   `count(where ...)`, `rollup`. A closed list of aggregate functions, like computed
   fields today. Covers 80% of Jira "formulas". — accepted (some in V1-GATE).
2. **WASM escape hatch** for the rest (arbitrary logic): typed
   in→out contract, sandbox. The author declares `component my_calc(in)->out`,
   implementation in WASM. Guarantees: cannot bypass permissions/log. — V1.
Loose inline formulas will NEVER be added — that is the "lego" principle.

## E. core PACK grows (system DSL) — accepted
Comment (polymorphic attachment to any entity), real User/Group,
Reaction/Mention, Attachment wrapper over file, Numerator (KEY-123 as in Jira).
This is a data+system pack, not the core.

## Queue (after the current KnowVault box)
1. Scalars A + tags (fast, additive)
2. links: bidirectional links (gives Jira task model)
3. aggregates D.1 (computed roll-up) — closes "formulas" at 80%
4. core pack: Comment, User/Group, Numerator
5. S3 BlobStore (storage, interface implementation)
6. WASM escape hatch D.2 (arbitrary functions, safely)

## Success test
Build an issue tracker "like Jira" as a single pack: projects, tasks, link types,
custom fields, labels, comments, boards, story-point roll-up, KEY numbering —
one page of DSL, not a single line in the core. This is dogfood #4 and proof of
primitive completeness.
