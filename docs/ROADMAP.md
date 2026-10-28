# Kalita — build order

> **Target baseline N1, 2026-10-07:** Этот build order сохранён как история. Активный план N1: next/delivery/ROADMAP.md, RELEASES.md, ITERATIONS.md и TASKS.json. Gate labels не версии существующего продукта. Для новой разработки при конфликте применяется [карта N1](next/README.md) и её [решения](next/DECISIONS.md). Текст ниже сохранён как предшествующая спецификация; implementation/release evidence не переобозначается.


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
- **Durability** — **VERIFIED**: against a real Postgres 16, a record survives a
  node restart (pure journal replay, identity included), a 100k-event journal
  replays in **~1.07 s** (ADR-001's 5 s budget holds), and DB-level immutability
  (`UPDATE`/`DELETE` blocked) is confirmed. Remaining: the projection recovery
  story for a corrupt/partial journal, and tiered storage (ADR-002) for entities
  too large for the in-memory projection — both later, only where measured.
- **Security P0** (SECURITY.md §3) — **DONE**: token-only auth (dev headers
  off by default behind `--insecure-dev-auth`), TLS via `--tls-cert/--tls-key`
  with a non-loopback-plaintext start refusal, same-origin enforcement + CSP.
  **P1 — DONE**: WebAuthn for human non-repudiation, secrets
  management (config file / docker secrets), emergency token revocation.
- **Deploy unit** — **DONE**: `docker compose up --build` brings up a node that
  serves HTTPS itself (self-signed cert generated on first boot), with the journal
  in Postgres and the node key persisted in a volume. Verified end-to-end: a
  record created over HTTPS survives a restart (journal replay), the same token
  still authenticates, and the cert/node key are reused (audit continuity), not
  regenerated. See `deploy/QUICKSTART.md`.

**Language (DSL)**
- **Lock the spec** — **DONE**: DSL-SPEC-v0 reconciled to the shipped compiler
  (rich scalars, serial, mdg, array[file]/tags/multiselect, dashboards, i18n
  labels, aggregates + `*_since`/`*_until`/business calendars, ref-path scopes)
  and marked frozen. All 16 packs + 3 acceptance examples compile clean.
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
  - **audit timeline + diff + "rewind to here"** — **DONE**: REST diff/revert/
    rewind endpoints + `can_revert` in meta; the record view's History panel
    renders readable changes (field old→new, state moves), an "Undo last" and a
    per-event "rewind here" for supervisors (gated on `full`). Verified live over
    REST; api regression test added. (Visual pass pending — browser backend was
    down in-session.)
  - **agent work board** — **DONE**: `GET /api/agent-board` (humans only) returns
    every task on an agent role; the UI groups them waiting → working → done (with
    a failed lane), a card opens the record the agent is on, auto-refreshing.
    Verified live on devtrack (a queued pick_up shows as waiting, then taken_by the
    agent once taken); api regression test added.
  - **HITL approval inbox** — **DONE**: each signature request expands to "what am
    I signing?" — the agent's work product (long-text fields like assessment/result
    in full) plus a compact field summary of the record, so a human signs informed,
    not blind. Verified live (a legal agent's assessment surfaces in the Lawyer's
    approval card before clear_legal is signed).
  - **customer portal** — **DONE**: the backend (invite → public register →
    record binding for row-level `where field = $me`) gains its UI — a login
    "I have an invite" redemption flow and a staff invite form (role + optional
    record binding, all selects). The portal is the same notation-driven UI under
    a scoped actor — no separate app. Verified live on servicedesk: a customer
    redeems an invite bound to their request and sees only that one row.

**L1 exit criteria met:** any compiled pack is fully operable by a human in the
browser — lists, forms, boards, dashboards, approvals (with agent-work preview),
the audit timeline with undo/rewind, the agent board, and an invite-based
customer portal — with zero pack-specific UI code, served from disk (not embedded
in the binary). Remaining L1 is polish (white-label depth, side-by-side diffs).
- **White-label / branding.** Per-client theming for the studio's multi-tenant
  on-prem model (one client = one node).

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
