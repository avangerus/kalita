# Kalita as a Microsoft Dynamics analogue — analysis (2026-06-14)

The founder's main idea: build a MS Dynamics 365 (ERP+CRM) analogue on kalita.
This raises the bar higher than Jira: Dynamics is not a single application, it is a FAMILY
of business applications on a shared platform (Dataverse) with a shared data model,
roles, BI, and low-code extensibility. That is exactly what kalita is by design —
which makes the overlap deep, not coincidental.

## What Dynamics is architecturally (and why kalita is the right foundation)

| Dynamics layer | kalita analogue | Status |
|---|---|---|
| Dataverse (shared DB, tables, relations) | core: entity/links/journal | ✅ present |
| Role/row/field-level security | permission engine | ✅ present |
| Business Process Flows (stages) | workflow + HITL | ✅ present |
| Power Automate (flows) | automation + workers | ✅ present |
| Model-driven apps (generated UI) | meta-driven UI | ✅ present |
| Power Apps (canvas, custom UI) | SDK + escape hatch | ⚠ partial (SDK present, WASM absent) |
| Power BI (dashboards) | aggregates + dashboards | ⚠ aggregates present, dashboards absent |
| Modules (Sales/Finance/SCM/HR) | product packs | ✅ model present, packs absent |

Conclusion: **architecturally kalita = Dataverse + Power Platform in embryo.**
The overlap is not contrived — both platforms build a family of applications on a shared
core with roles and low-code. This is the strongest confirmation that we have been building
the right thing.

## What Dynamics requires that we do NOT have (honest gaps)

### Critical for ERP/CRM
1. **Serious money:** decimal (present), but currencies with exchange rates,
   multi-currency, rounding rules are needed. → data-pack currencies + money type
   with currency (money already stores currency — expose it fully).
2. **Document numbering** (INV-2026-00042) — a sequence generator. → core-pack (in queue).
3. **Document line items** (order → order lines) — master-detail. Here we have
   ref + aggregates (sum of lines = order total). ✅ already expressible.
4. **Ledger entries/registers** (accounting: debit/credit, balances) — this is
   event-sourcing, our STRONG side: entry = event, balance =
   projection/aggregate. kalita is structurally better here than relational Dynamics.
5. **Calendar/working days** for SLAs and payment due dates. → data-pack (planned in T9).
6. **Reports/dashboards** — NOT cloud BI. Summaries are computed by the core from aggregates
   and rendered by our frontend, all within the perimeter. → aggregates present, dashboard block needed.

### Important but not blockers for launch
7. Calculation engines (pricing, taxes, discounts) → WASM escape hatch.
8. Data import/export (migration from legacy) → export/import + absorption is available.
9. Integrations (banks, EDI, marketplaces) → connector-workers.
10. Interface multilingualism → i18n (planned).

## Strategic fork — my independent view

Dynamics is NOT "just another vertical" — it is a claim to become a platform family.
Honestly: building a "full Dynamics analogue" as a product is a trap (Dynamics was
written by thousands of people over 20 years; catching up head-on is impossible). But that is also
NOT necessary. The correct framing is: **kalita provides the foundation on which a Dynamics-module
analogue is assembled as a pack in days, while the platform does not compete with all of
Dynamics as a whole.**

The path: not "Dynamics-killer", but prove the model on ONE module that hurts
the market and where Dynamics is too expensive/heavy for mid-market (Russia: Dynamics is gone,
1C dominates — a window is open!). First-module candidates:
- **CRM (Sales)** — leads/deals/contacts/pipeline. The most common entry point, easy
  to demo, we already build it for ourselves (dogfood CRM).
- **Accounts receivable/treasury** — collections pack already exists, close.
- **Procurement/warehouse** — master-detail + balances (our event-sourcing strength).

## What this changes in the primitives queue
Elevates in priority (needed for ERP/CRM class):
1. **Sequence generator** (documents) — already next
2. **money with currency** expose fully + data-pack currencies/countries (T9)
3. **dashboard block** (summaries on top of aggregates) — on-prem, zero external
   services; no cloud BI — data never leaves the perimeter
4. **Comment + User/Group** (core-pack) — in-record collaboration
Everything else (WASM, calendar, i18n) — per plan.

## Success test
Assemble the CRM module "like Dynamics Sales" as a single pack: Lead → Deal (pipeline
stages) → Contact/Organization → Activities (calls/emails) → pipeline dashboard
with aggregates. If it fits on one page of DSL — the "Dynamics analogue" claim
is justified. This is dogfood #5.
