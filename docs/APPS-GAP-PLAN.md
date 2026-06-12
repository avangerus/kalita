# What to Finish in the Core Before Real Applications (2026-06-14)

Method: three applications (HR, CRM, Service desk+portal) were walked through
REAL usage scenarios. Gaps named independently by ≥2 applications = shared
foundation. Goal — not "1.0 in general", but "several applications that are
actually used, on a Dynamics-class core".

## Summary: Recurring Gaps (= true priority)

| Gap | HR | CRM | Desk | Verdict |
|---|---|---|---|---|
| **Comment/thread inside a record** | ✓ | ✓ | ✓ BLOCKER | **B1** all three |
| **File upload in UI** | ✓ BLOCKER | — | ✓ | **B2** |
| **array[file]** (multiple attachments) | ✓ | — | ✓ | **B3** |
| **Field arithmetic in computed** | ✓ (balance) | ✓ BLOCKER (forecast) | — | **B4** |
| **null in expressions** | ✓ BLOCKER | — | — | **B5** |
| **report/dashboard** (summaries) | ✓ | ✓ BLOCKER | — | **B6** |
| **hours_since/minutes_since** | — | — | ✓ (SLA<1 day) | **B7** |
| **calendar-view** | ✓ | ✓ | — | W |
| **business calendar (working days)** | ✓ important | — | ✓ | W |
| **datetime trigger (reminders)** | — | ✓ | — | W |
| **core.User real** | ✓ | indirect | indirect | W (tech-debt) |
| **i18n labels** | ✓ | — | — | W |
| **polymorphic reference** | — | ✓ | — | D |
| **swimlanes/saved filters** | ✓ | ✓ | — | D |
| **in-portal notifications** | — | — | ✓ | D |

## What Turned Out to Already Be Done (agents did not know)
- serial numbering — IMPLEMENTED (agents doubted) ✅
- two-level ABAC ref-path (`ticket.customer = $me`) — IMPLEMENTED (resolvePath
  does multi-hop dereference) ✅ — removes the Desk agent's fear about bypass
- file upload in API + content-addressed — EXISTS; **form-UI widget EXISTS**
  (FileInput drag-drop) — agents were reading the old V1-GATE, actually done ✅
- invites/self-registration — IMPLEMENTED (POST /api/register) ✅
- Query v2 (where/sort/search) — EXISTS ✅
So Desk blockers B1(registration) and part of file — already closed. The real,
unresolved blockers are below.

## CORE IMPROVEMENT PLAN (by criticality, accounting for facts)

### Blockers — Applications Are Not "Real" Without These
1. **B1. Comment as a primitive** (core pack + UI timeline section). Polymorphic
   attachment to any record, is_internal visibility via ABAC, native thread in
   detail-view. Needed by ALL THREE. Most important.
2. **B4. Arithmetic in computed**: `amount * probability / 100`, `limit - used`.
   Extend the expression language: +,-,*,/ between fields of the same record. Needed by CRM
   (forecast) and HR (balance). The condition language already exists — add arithmetic to
   the computed evaluator.
3. **B5. null in the expression language**: `where certificate != null`, `field = null`.
   Conditional transitions based on field presence. Small, but a blocker for HR.
4. **B6. report/dashboard block**: group-by summaries with aggregates over ALL records
   (not only `where reffield=$self`). On-prem, from the core. Needed by CRM (funnel),
   HR (metrics), and this is the Dynamics wrapper.
5. **B3. array[file]**: multiple attachments. Desk (screenshots+logs), HR (resumes).
6. **B7. hours_since/minutes_since**: SLA < 1 day. Desk.

### Important — Application Is Incomplete Without These
7. **calendar-view**: leave schedule (HR), activities (CRM). New view type.
8. **business calendar** (data-pack of working days + business_days in duration/stuck):
   legally correct HR calculations, Desk SLA.
9. **datetime trigger** `on Activity.remind_at approaching`: personal reminders (CRM).
10. **core.User as a real system entity** + i18n labels (RU).

### Nice to Have
polymorphic references, swimlanes, runtime saved-filters, in-portal notifications.

## ⭐ Opus magnum: Jira Where Agents Do the Tasks (founder's vision)
"Jira where agents do the tasks and you can assign tasks to them." This is not a new
gap — it is a COMBINATION of everything we have been building, in one application. Almost
everything already exists: tracker pack (tasks/links/funnel) ✅, agents as assignee=agent ✅,
tasks-in-pool for agents ✅, MCP (agent takes task, works, reports) ✅,
HITL signature on merge ✅, dev_department pack (development department on kalita) ✅,
dogfood (kalita develops itself through itself) ✅.
What is missing for "assigned task → agent did it → you accepted" as a PRODUCT:
(a) Comment (B1) — dialog with the agent inside the task; (b) report/dashboard (B6) — seeing
progress; (c) reliable agent-runner worker (Claude Code as a service, taking
tasks from the pool and actually coding/doing them) — this is a worker, not the core;
(d) UI "assign task to agent" — a button creating a Task with assignee=agent.
Conclusion: opus magnum = tracker pack + Comment + agent-runner worker. The core is almost
ready; this is ASSEMBLY, not a new foundation. Do it AFTER blockers B1/B4/B6 — then
it will come together as pack+worker, proving the entire model at once. This is the final
dogfood: kalita managing its own development with agents, as a product.

## Execution Order (autopilot)
Comment✅ → arithmetic in computed✅ → null✅ → report/dashboard✅ → hours_since✅ →
array[file]✅ → **(all blockers B1–B7 closed)** → calendar-view → business calendar →
datetime trigger → core.User+i18n.
array[file] (this commit, B3): new TyArrayFile type throughout the pipeline (parser,
validate, meta-UI, MCP describe, compose, grammar); Incident.attachments
verified via dogfood (multiple attachments round-trip, element without hash rejected).
Done (0e94f74): null presence; dashboard block (count/sum/avg/min/max,
group by, where) with per-row ABAC; MCP/REST; grammar returned to MCP instead of
pangram. Done (02f6d75): Service Desk pack (ITSM) as dogfood —
10 entities, state machines, RBAC across 10 roles, HITL on approve/CAB; fix — Create
returns enriched record (serial+computed). Done (this commit, B7):
hours_since/minutes_since; live SLA in pack (sla_left via ref-path
sla_policy.resolution_minutes − minutes_since(opened), "SLA Overdue" tile);
fixes — $now default takes engine time (was zero-time), ref-path in computed
normalizes dots (two-hop deref in arithmetic). Next blocker — array[file].
After every 2-3 — assemble the corresponding pack as dogfood and verify live.
Success test: HR / CRM / Desk are assembled as a pack and pass a real E2E scenario.
