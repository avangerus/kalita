# Founder's thought log → strategic decisions

Process: the founder drops thoughts into the chat, they are recorded here with analysis: what
we change in the system, in what form, when. Statuses: 🔥 accepted into work ·
📐 designed, waiting in queue · 🧊 frozen with reason.

---

## 2026-06-13 — batch 1

### T1. Connectors to external systems and messengers 🔥
**Thought:** connections to various systems, messengers, etc. are needed.
**Analysis:** critical, but NOT in the core (rule: a new outward channel = declaration
in DSL, the word `integration` is reserved). Form: connector = worker-agent
(like the knowvault indexer) with its own identity and deny. Strategically the most important —
**Telegram**: for RU-SMB the interface == messenger; "an accounting system whose
front end is Telegram" — an empty niche. A client portal in Telegram = a bot-worker: the client
writes to the bot → a record/request in kalita → statuses back to the chat. A candidate for
the killer feature of the polygon. Built one connector per paying vertical.

### T2. KnowVault ingest: text extraction, connecting external sources 📐
**Thought:** the ingest service fetches data, first extracts text,
indexes into RAG; a way to connect external sources is needed.
**Analysis:** already designed (KNOWVAULT-INTEGRATION): ingest = worker-agent,
picks up start_index tasks, text extraction — internal to the worker. "Connect
external" = create a Source record (kind: Files/Mail/Repo/Database/Chat) — the UI
already supports this. Remaining: execute v0.2 point 5.

### T3. Legacy absorption: mimicry without perpetuation 🔥
**Thought:** a kalita module absorbs legacy, mimics it via agents,
but does not become "a more advanced legacy".
**Analysis:** this is an agentic strangler-fig — and it is OUR enterprise wedge (window #1,
the legacy calendar wave). "Strangler mode" form: (1) bridge-agents read/
write legacy interfaces (files, ODBC, API), records are mirrored into kalita;
(2) processes migrate to DSL one at a time, legacy becomes read-only for them;
(3) mimicry — a TEMPORARY phase with a recorded end date in the absorption plan
(otherwise we build a permanent emulator = "more advanced legacy", which the founder
rightly fears). Artifact: a methodology + absorption-pack on the pilot.

### T4. Managing git commands 🧊→📐
**Thought:** kalita can manage git, "I don't know why".
**Analysis:** why — I know: packs are text, their home is git. Useful subset:
**export/sync of node definitions to a git repo** (backup, versioning, external
PR review of proposals). The core does not need git; this is a worker. Cheap, but not before
the polygon. Git operations as pipeline steps in the development pipeline — already covered by the
dev_department pack (its workers already call git themselves).

### T5. Deploy and mutations without deploy 📐 (already 80% there)
**Thought:** deploy and mutate, possibly without a deploy.
**Analysis:** mutation without deploy — that IS the ESSENCE of kalita: the change pipeline applies
definitions to the live node, no restart needed. Deploy only remains for
the platform binary → what is missing: the platform update path (kalita upgrade,
blue-green node) — v1.

### T6. "What is actually needed" — an independent list against market pain points
1. **Demo in 10 minutes**: docker run → live accounts-receivable with data and an agent.
   Time-to-wow determines the fate of the GitHub launch more than any feature. 🔥 before
   publishing.
2. **Result dashboard for the director**: "during the week agents completed N tasks,
   M signatures, K rejected, X collected" — an artifact that sells renewals.
   The pain "pilots never reach production" is cured by visible results. (v1
   `metric`/`dashboard`, but a minimal summary — sooner.)
3. **Outcome metrics in packs** (how much collected/closed) — foundation for
   value-pricing "selling work".
4. **Export of checkpoints to the client** ("your notary": the log was not rewritten) —
   cheap, strong trust argument. P1.
5. **T1 Telegram** — see above: underestimated market entry point.

### T7. Large data volumes; separate databases for separate modules 🔥→📐
**Thought:** kalita must handle large event volumes — like mature
enterprise event sourcing with projections; can different databases be used for different modules.
**Analysis:** accepted and designed — ADR-002: one journal (PG, partitions,
snapshots), two-tier core projections (RAM → SQL above threshold), module databases —
yes, at the worker level (Qdrant/ClickHouse/DuckDB), populated by replay via
Store.Since and disposable; journal = internal bus, no brokers.

### T8. Criteria for placing functions at each layer 🔥→📐
**Thought:** what belongs in the core (database, indexes), what belongs at the "core-function"
level (users, reference data), what is modular (accounts-receivable → logistics/TSP → CRM
for kalita itself).
**Analysis — four layers, each with a membership test:**

| Layer | Test | Examples |
|---|---|---|
| **Core** (Go) | "needed for ANY pack to work and be unable to lie" + inexpressible in DSL | journal, chains, compiler, permissions, workflow, tasks, indexes, MCP |
| **Core-pack** (system DSL) | "needed by almost every pack, expressible in DSL, changes only with a platform update" | users, reference data (currencies/countries/org structure), attachments, document sequence generators, notifications |
| **Product pack** | "can be sold or deleted without breaking neighbors" (linked via depends) | accounts-receivable, contracts, logistics, CRM, knowvault-orchestration |
| **Worker** | "needs a process, library, secret, or the external world" | TSP solver, embeddings, Telegram bridge, indexer |

The TSP/logistics example shows the full pattern: domain — pack (Deliveries,
Routes, statuses, permissions), math — worker-agent Router (picks up
optimize_routes task, returns routes, everything in the journal). Domain in DSL,
computation in the worker — always.

CRM for kalita itself (leads, partners, community) — built as a normal
product pack on our own node: dogfood #3, after dev_department and boards.
Rule against the temptation: if a feature pulls toward the core "because it's faster that way"
— that is the signal to design it as a pack or worker.

### T9. Multilingualism + pre-populated reference data 📐
**Thought:** i18n; immediately populate units/weights, currencies, countries, time zones
(IANA), production calendar.
**Analysis:** i18n = label layer on top of the grammar (word reserved, v1;
grammar stays in English — an HLD decision). Reference data — core level
per T8, and they introduce a new format capability: **data-packs** (a pack carries not only
a schema but also seed data with a version): units (SI), currencies ISO 4217, countries ISO
3166, time zones IANA tzdata. **Production calendar** — a separate gem:
by country, updated annually → this is subscription data (a ready-made product!) and
the foundation for business-days in expressions/SLA (`stuck 5 business days`) — v1.
Action: lay out the seed-data format in the pack spec before the marketplace.

### T10. ClickHouse, Redis, Kafka, vector databases 📐 (closed by ADR-002)
**Thought:** connect vector databases, ClickHouse, Redis, Kafka, "whatever exists".
**Analysis:** the answer is already written in ADR-002, one pattern — **sink/projection-worker**:
journal → Store.Since(seq) → worker feeds its own database. Qdrant at knowvault (done),
ClickHouse = analytics sink, full-text = sink, Redis not needed by the core
(projections in RAM), workers — at their discretion. Kafka AS THE CORE BUS is prohibited
(second truth); if a client has Kafka and wants events there — that is
a "journal→Kafka" sink-worker, legitimate. A generic "journal-sink" worker with a
destination config — v1 candidate: one codebase, N databases.

### T11. "Market pain → feature" map 🔥 (systematic pass)
Against verified pain points from research. ✅ = present, ⭐ = new feature in queue.

**Pain 1: pilots never reach production (>80% without effect; fear + invisible result)**
- ✅ HITL signatures, journal, rollback — address fear
- ⭐ **Shadow mode**: the agent role runs "dry" — all actions go as
  proposal-diffs without being applied; the director watches for a week what the agent
  WOULD HAVE DONE, then enables it. Onboarding threshold → zero. Killer deployment feature.
- ⭐ **Agent role autonomy dial** (trust dial): everything requires signature → only critical requires signature → autonomous. Trust is built in steps, visible in the journal.
- ⭐ "Week in numbers" report for the director (T6.2).

**Pain 2: "almost right", silent corruption (66%, DELEGATE-52)**
- ✅ grammar, gates, fact-check on reports
- ⭐ **Rationale in actions**: act/complete carry a brief "why" — the journal
  reads as an explained history, not a log.

**Pain 3: work is sold against payroll cost (Sequoia 6:1)**
- ⭐ **Cost of work in money**: role rate in Settings × task work-hours =
  "agents completed work worth X ₽ this month". A report that sells renewals on its own.
- ⭐ Pack outcome metrics (collected/closed) — value pricing (T6.3).

**Pain 4: data within the perimeter**
- ✅ core. ⭐ **Egress report**: "what left the perimeter this month" — auto-generated
  from the journal (outgoing = only declared webhooks, already events).
  Compliance artifact for free.

**Pain 5: the legacy wave**
- 📐 T3 absorption. ⭐ First step — **read-only mirror**: a worker
  replicates legacy data into a pack; the client gets search/dashboards/journal
  on top of the dying system with no write access. Easy sale, zero risk.

**Pain 6: distribution and trust — the primary deficit**
- ⭐ demo node in 10 minutes (T6.1), checkpoint export (T6.4)
- ⭐ **Pack documentation generation from DSL**: kalita pack describe → README with
  screenshots/schema — pack authors get a showcase for free (marketplace).

**Pain 7: giants bundle thin — depth in the process is what survives**
- ⭐ **Excel eater**: upload the department's main Excel → the agent proposes
  a pack (schema+permissions+workflow from real data). Entry into process depth
  where bundled copilots cannot reach.

Priority of cheap/powerful: shadow mode → cost of work in ₽ → egress report →
rationale. All four — weeks, not months.

### T12. KnowVault — Trojan product: kalita is invisible until the reveal moment 🔥
**Thought (founder, 2026-06-14):** the company installs KnowVault as an application,
not knowing about kalita. They create workspaces, upload documents, indexing runs, they happily
ask questions in the interface, connect their 1C via API — and only then
discover that they have had kalita running all along and "you can do anything with it".
**Analysis:** confirmed as the main sales strategy (door #1 + land-and-
expand). Product implications:
1. **Delivery = KnowVault product**: `docker compose up` → branded
   KnowVault, the word kalita appears nowhere. Inside: node + workers + Qdrant.
2. **Product UI is required** (the very "one custom screen"): search +
   RAG answers + drag-drop document upload. The universal kalita-UI
   remains as the "Administration" section (sources, permissions, journal) under
   the KnowVault brand.
3. **File upload elevated in priority** (from V1-GATE): "uploading documents"
   — that means upload in the UI, not just folder paths.
4. **Q&A worker**: search_perimeter + answer generation (LLM from VaultSettings);
   questions are already logged (SearchQuery).
5. **1C via API**: Source kind=Database + 1C connector-worker (HTTP services/
   ODATA) — per the first client's request.
6. **Reveal mechanic** ("you already have kalita"): "Automation" section in
   the admin panel → offers packs/agents on top of their own data. This is the upsell moment
   and the transition to a platform subscription.
Price: KnowVault is sold as a product at its own price; the kalita reveal is an upsell.

### T13. ABAC and complex queries — "bone in the throat" (like Jira) 🔥 CRITICAL
**Thought:** beyond types, the main problem is ABAC and complex queries; that is exactly
the bone in the throat of Jira.
**Analysis — the founder is right, this is the sharpest architectural gap.** Honest
review of the current state:

WHAT EXISTS (ABAC partially): row-level `where field = $self/$me`, field-level
deny, deny>allow, roles. This is already entry-level ABAC.

WHAT IS MISSING (and it hurts):
1. **Complex permission conditions:** only `field = value` and `and`. No `or`,
   parentheses, two-field comparison, check via a related record
   (`where project.owner = $me` — see tasks in projects where I am the owner).
   The exact Jira pain: "grant access if (I am the reporter OR I am on the project team)
   AND status is not Closed".
2. **Complex queries:** Query only supports equality filters. No ranges
   (`points > 5`), `or`, multi-field sorting, text search, relation queries,
   aggregate filters. Jira's JQL — that is what we lack.
3. **Saved filters/views** as objects (there is `view` in ui, but it is static).

PLAN (priority — above dashboard, this is the ERP/CRM foundation):
- **A. Rich condition language** (shared across permission `where`, query filters, guards,
  aggregates): or/and/parentheses, comparisons, in, field-to-field, path via ref
  (`project.owner`), `$me`/`$self`/`$now`. ONE verifiable evaluator,
  closed grammar — NOT arbitrary code (otherwise we lose guarantees). This is
  "JQL, but safe and unified for permissions and queries".
- **B. Query API v2:** filter expressions in this language, sort[], full-text on
  text fields, pagination (present). Possibly a separate POST /api/query with a body.
- **C. ABAC on related records:** allow a path through ref in `where`
  (one-to-two hops), this closes 90% of Jira use cases.
Risk: performance (scans) — but in-RAM projections + indexes on hot
fields (ADR-002) hold up; complex queries — to the analytics sink-worker at scale.
**This is the next major block after current types — it is more important than dashboard.**

### T14. Small business application portfolio — "spreading the platform" 🔥
**Thought:** build systems for small businesses on kalita and spread the platform
widely: requests, client portals, HR (leave/sick-leave/hiring),
document management, CRM, website with online store — "like a rubberduck on the street".
**Analysis — this is a GTM strategy, not just a list.** Each item = a pack (one page
of DSL), and together they form a vertical catalog on one core. Matches
the T8 model (domain=pack, heavy=worker) and the strategy (verticals sell,
platform is inside):

| Application | Pack complexity | Workers | Primitive readiness |
|---|---|---|---|
| Requests/inquiries | low | — | ✅ present (collections-like) |
| Client portal | low | — | ✅ (invites + row-level present) |
| HR: leave/sick-leave | medium | — | ✅ (workflow+approval+calendar*) |
| Hiring (ATS) | medium | — | ✅ (pipeline stages = workflow) |
| Document management | medium | OCR? | ✅ (file+workflow+sequence generator) |
| CRM | medium | — | ✅ (everything for dogfood #5) |
| Online store | high | payments, storefront | ⚠ needs escape hatch + payment worker |

*working days calendar — planned in T9.
Conclusion: 6 of 7 applications are expressible with EXISTING primitives (+ABAC from T13
will strengthen them). The store is the only one requiring an escape hatch (storefront) and
a payment worker. "Spread" strategy: a set of ready packs in the repository
as a showcase of capabilities → partners/vibe-coders pick them up and deploy (door #2).
Each pack = demo + marketplace item.
PACK QUEUE (after ABAC): requests → portal → CRM → HR → document management →
hiring → store (last, it is the heaviest).

### T15. Admin panel view types — closed set (like Refine/enterprise) 📐
**Thought:** different view types for the admin panel (like Refine etc.); in
enterprise the set is sparse — board, detail, list, report, custom block.
**Analysis — the founder is right, and "sparse set" is a FEATURE, not a bug.** A closed list
of view types = the UI stays verifiable (an agent cannot render a broken screen), just like
the rest of the grammar. Refine/AdminJS/React-Admin proved: 95% of enterprise
UI = a handful of patterns. Canonical kalita set:

| View | What | Status |
|---|---|---|
| **list** | table: columns, filters, sorting, saved-views | ✅ present |
| **board** | kanban by enum field, swimlanes | ✅ (swimlanes — add) |
| **detail** | record card: sections, relations, journal, actions | ✅ present (form+RecordView) |
| **report** | summary: metrics/groupings/chart (= dashboard T-above) | ⚠ in progress |
| **calendar** | records by date field (leave, deadlines, activities) | ⛔ absent — needed |
| **custom** | escape hatch: own component (SDK/WASM) | ⚠ SDK screen present, formalize |

Principle: `ui Entity:` declares WHICH of these views are available and their config;
the renderer (built-in or SDK) knows all types. A new view type is added to
the core rarely and deliberately (like a new primitive), not by a pack author.
ADD to UI queue: calendar-view (important for HR/CRM/deadlines),
swimlanes in board, formalize custom-block as the SDK/WASM extension point.
Relation: detail-view benefits from relations (T-links, present) and ABAC (T13).

---

Related documents: ROADMAP-TREE.md (queue), PORTAL-VISION.md,
KNOWVAULT-INTEGRATION.md, SECURITY.md, adr/ADR-002-tiered-storage.md,
DYNAMICS-VISION.md, TYPE-SYSTEM-V1.md.
