# Kalita — build order

**The principle: a loop, not a waterfall.** Build bottom-up, but treat the
functional packs as the *discovery engine* for what the core and the language
still lack. You cannot design the kernel to completeness in a vacuum — the market
tells you what is missing, and the packs are how the market's demands reach the
kernel. So: stabilise the foundation enough to build on, build the front
framework that renders any pack, then build packs chosen by **market need** — and
every gap a pack hits flows back down as a core/language requirement.

```
  L0  CORE + LANGUAGE  ──stable enough──▶  L1  FRONT FRAMEWORK  ──renders──▶  L2  PACKS (market-driven)
   ▲                                                                                      │
   └──────────────────  gaps discovered while building packs feed back  ◀────────────────┘
```

The order below is the sort. Within L0 we do only what we *know* is needed
(durability, security, spec lock) — not speculative language features. Those come
from L2.

---

## L0 — Core + Language (make the foundation trustworthy, then lock it)

Everything above rests on this; it must be durable and safe before a UI ships or
a customer touches it. Do the **known** work only.

**Core / runtime**
- **Durability.** Verify the PostgreSQL journal against a real Postgres (Docker),
  measure ADR-001's claim (100k-event restart < 5 s), define the projection
  recovery story. Today projections are in-memory — fine for the SMB profile, but
  the persistence path must be *proven*, not claimed.
- **Security P0** (SECURITY.md §3): remove the REST dev headers
  (`X-Actor-Id/Role` = impersonation), TLS, same-origin CORS/CSRF. These gate any
  deployment beyond localhost and any UI release.
- **Deploy unit.** One-command on-prem node (binary + Postgres + TLS), the
  "inside the client's contour" promise made real.

**Language (DSL)**
- **Lock the spec.** DSL-SPEC reflects what exists today (types, enums, refs,
  workflows, ABAC, automation, computed +`*_since`/`*_until`, dashboards, i18n,
  mdg, revert/rewind/diff). Freeze the grammar surface so packs build on stable
  ground.
- **Close only the *known* rough edges:** permission-scope narrowing (multi-term
  `and` intersection, `or`, `core.*` ref-paths — correct today, just scanning);
  constraints beyond `unique`. Resist adding speculative features here.
- **Keep the DSL concise — push complexity into the kernel.** The bar: a module
  is a short, declarative file; the engine does the hard part. Every new construct
  must pay for itself.

**Exit criteria for L0:** a node persists and recovers from a real journal, runs
over TLS with token-only auth, and the DSL spec is frozen. *Then* go up.

---

## L1 — Frontend framework (render any pack, surface the differentiators)

A pack-agnostic, notation-driven client so L2 packs render for free. Build the
*framework*, not screens.

- **Rendering kernel** (mostly exists, harden): lists, 3-column forms, kanban,
  dashboards, async pickers — all from per-actor pack metadata, no build step.
- **Differentiator surfaces** — the things that are in the kernel but invisible:
  the **audit timeline + diff + "rewind to here"**, the **HITL approval inbox**
  (Ed25519 signing), the **agent work board**, the journal viewer.
- **White-label / branding.** Per-client theming for the studio's multi-tenant
  on-prem model (one client = one node).
- **Customer portal.** Invite-based, row-level visibility (already specced).

**Exit criteria for L1:** any compiled pack is fully operable by a human in the
browser — including approvals, audit, and undo — with zero pack-specific UI code.

---

## L2 — Functional packs (market-driven, modular — the discovery engine)

This is where market need drives the work and the work reveals core gaps. **Step
0 of L2 is a market-needs pass** for the Russian mid-market back-office (ICP:
50+ employees, mature processes) to *rank* which modules to build first — because
the highest-value module reveals the most important missing core capability.

Break each catalog agent into modules; for each, the *likely* core/language gap
it will expose (hypotheses to validate against the market, not commitments):

| Catalog agent | Functional modules | Likely core gap it reveals |
|---|---|---|
| Договорная работа ✅ / Юристы | drafting, clause review, multi-party sign, versioning | document **generation**, e-signature, version diff (have diff ✓) |
| Дебиторка ✅ | invoicing, dunning sequences, settlement, reporting | payment **integration**, scheduled sequences, export/reporting |
| Техподдержка ✅ | incidents, SLA, CMDB, KB | inbound **connectors** (email/portal), SLA timers (have business-cal ✓) |
| HR | onboarding checklists, leave, documents | document gen, **integration** (payroll), multi-step task chains |
| Комплаенс | controls, evidence, policy versions, audits | **reporting/export**, scheduled controls, attestations |
| Лидогенерация | intake, enrichment, routing | external **intake** (forms/email), data enrichment connectors |
| Обработка заявок | request intake, triage, fulfilment | inbound connectors, routing rules |
| Жалобы | complaint intake, escalation, resolution SLA | sentiment/classification, inbound connectors |
| Легаси-агент *(technical)* | read/migrate from existing DBs/1C | **data import / migration**, schema mapping |
| Агент-интегратор *(technical)* | connect to git, mail, e-shop, ERP | **connector framework** — the dashed arrows in the README |
| Агент-исполнитель *(technical)* | run actions in external systems | outbound actions, capability/sandbox model |

**The pattern that will emerge:** the loudest missing core capabilities are
almost certainly (a) a **connector/integration framework** (the technical agents
and every intake module need it — it is the dashed half of the headline diagram,
and likely not built yet), (b) **document & report generation**, and
(c) **external intake** (email/web → records). The market-needs pass confirms the
order; building the first pack proves the gap; the gap flows back to L0.

**Exit criteria for L2 (per pack):** the pack runs end-to-end through MCP *and*
the UI, with its agent doing real work on a live model, and any kernel gap it hit
is filed as an L0 item — closing the loop.

---

## Immediate next

Per the sort: **L0 first.** The single highest-leverage start is
**durability + the security P0s** — verify the real Postgres journal and remove
the impersonation headers / add TLS / same-origin. That makes everything already
built *trustworthy*, which is the precondition for the front framework (L1) and
for any customer or investor demo. The DSL spec lock can run in parallel.
