```
       ██   ██  █████  ██      ██ ████████  █████
       ██  ██  ██   ██ ██      ██    ██    ██   ██
       █████   ███████ ██      ██    ██    ███████
       ██  ██  ██   ██ ██      ██    ██    ██   ██
       ██   ██ ██   ██ ███████ ██    ██    ██   ██

       an executable runtime for business systems
       ·  in the agent era  ·
```

> Agents and humans **describe** a business system in a constrained DSL —
> entities, workflows, permissions, automation, dashboards, UI. Kalita
> **executes the description directly** (no code generation): every change is a
> signed diff, every action is an event in a tamper-evident journal, every
> agent is an employee with an identity, permissions and an audit trail.

**Why.** LLM agents silently corrupt what they are trusted with when the artifact
is free-form (code, documents). Kalita replaces *welding* with *Lego*: a grammar
where drift does not compile, critical transitions require a human signature,
and nothing happens silently.

---

## How it fits together

```
            humans                          AI agents
              │                                 │
              ▼                                 ▼
      ┌──────────────┐                  ┌──────────────┐
      │   Web UI     │                  │ MCP gateway  │   same engine,
      │ (notation-   │                  │   /mcp       │   same checks,
      │  driven)     │                  │  ~22 tools   │   same journal
      └──────┬───────┘                  └──────┬───────┘
             │           REST  /  JSON-RPC     │
             └────────────────┬────────────────┘
                              ▼
            ┌─────────────────────────────────────┐
            │               ENGINE                 │
            │  DSL compiler · ABAC permissions ·   │
            │  workflows (HITL) · automation ·     │
            │  computed fields · dashboards        │
            └─────────────────┬───────────────────┘
                              │   every change → signed diff
                              │   every action → one event
                              ▼
            ┌─────────────────────────────────────┐
            │      append-only event journal       │
            │  SHA-256 hash chain · DB-immutable   │
            │  projections replay from here alone  │
            └─────────────────────────────────────┘
```

The DSL is a closed grammar, not arbitrary code, so the guarantees hold: a
permission can't fail open, an agent role without a `deny` block won't compile,
and the workflow state field can only move through declared transitions.

## Lego, not welding

A whole module is one `.dsl` file. This is a slice of the Service Desk pack:

```dsl
entity Incident "Инцидент":
    number:    serial format="INC-{year}-{seq:6}" label="Номер"
    title:     string required label="Тема"
    priority:  enum[P1, P2, P3, P4] default=P3 label="Приоритет"
    source:    enum[Manual, Tivoli, Email, Portal] default=Manual
    assignee:  ref[core.User] label="Исполнитель"
    sla_policy: ref[SLAPolicy] label="Политика SLA"
    opened:    datetime default=$now
    # live SLA: minutes left before the linked policy's threshold is breached
    sla_left:  int computed = sla_policy.resolution_minutes - minutes_since(opened)
    status:    enum[New, Investigating, Identified, Resolved, Closed] default=New label="Статус"

workflow Incident on status:
    New           -> Investigating: investigate assignee=OperatorL2 label="Взять в работу"
    Investigating -> Identified:    identify label="Причина найдена"
    Identified    -> Resolved:      resolve_incident label="Решить"
    Resolved      -> Closed:        close_incident label="Закрыть"

dashboard OperatorBoard "Очередь оператора":
    tile "Открытые инциденты": count Incident where status != Closed and status != Resolved
    tile "Просрочка SLA":      count Incident where sla_left < 0
    tile "По приоритету":      count Incident group by priority
```

## Human-in-the-loop is a first-class gate

```
   agent ── act("approve_change") ──▶  engine
                                          │
                          requires approval(ChangeManager)
                                          ▼
                              ┌───────────────────────┐
                              │  PENDING  ───────────  │   nothing happens
                              │  a human signs it      │   until a person
                              │  (Ed25519, offline-    │   signs — the agent
                              │   verifiable)          │   cannot rush it
                              └───────────┬───────────┘
                                          ▼
                                     applied ✓   (one journaled event)
```

## What works today

| Area | Capability |
|------|-----------|
| **DSL compiler** | entities · rich types (money, email, file, `array[file]`, serial, duration…) · enums · refs & bidirectional links · workflows · ABAC permissions · automation · computed fields (arithmetic, aggregates, `days/hours/minutes_since`) · dashboards · i18n labels. Errors are `{code, file:line, message, fix_hint}` for agent self-correction. |
| **Runtime** | CRUD + validation · guarded workflow transitions & auto-moves · approval queue (signature-gated) · TTL task pool · automation triggers (schedule / event / stuck) · row-level ABAC on reads, writes **and** dashboard aggregates. |
| **Event store** | append-only PostgreSQL journal · SHA-256 hash chain · DB-level immutability · node-key checkpoints · definitions and projections replay from the journal. |
| **MCP gateway** | `~22` tools at `/mcp`. An agent starts from an empty node, iterates DSL to green via `validate_dsl`, `propose_change`s a pack, and works inside it after a human signs — that loop is the acceptance test. |
| **Generated UI** | one notation-driven client (no build step) renders any pack from per-actor metadata: lists, 3-column forms, kanban boards, record timelines, dashboards, the approval inbox, async people pickers. Invite-based customer portal with row-level visibility. |
| **core.User** | built-in people directory projected from the identity registry — `ref[core.User]` pickers search it; no User table per pack. |

## Quick start

```bash
# native (in-memory journal — dev only):
go build ./cmd/kalita
./kalita serve --pack packs/servicedesk --ui-dir web --demo
#   UI + REST : http://127.0.0.1:8080         (--demo prints a token per role)
#   MCP       : http://127.0.0.1:8080/mcp     (Authorization: Bearer <token>)

# with a real journal:
KALITA_PG_DSN=postgres://… ./kalita serve --pack packs/servicedesk

# compile-check a pack with agent-grade diagnostics:
./kalita check --pack packs/servicedesk
```

A module is a directory of `.dsl` files. `serve --demo` seeds a token per role
and an empty node accepts its first pack through `propose_change` + a signature.

## Modules & examples

```
packs/servicedesk/   ITSM Service Desk (incidents, problems, changes, SLA, KB, CMDB)
packs/hr/            leave & balances           packs/tracker/   Jira-like issues
packs/knowvault/     RAG knowledge base box     packs/boards/    simple boards
examples/collections, examples/dev_department, examples/pangram (every construct)
```

The Service Desk pack is a functional ITSM core built from a real enterprise
spec — 10 entities, state machines, RBAC across 10 roles, HITL on approvals/CAB,
live SLA timers and operator dashboards — running on the kernel with **zero**
domain code.

## Design documents

[HLD](docs/HLD.md) · [DSL Spec](docs/DSL-SPEC-v0.md) ·
[MCP Contract](docs/MCP-CONTRACT-v0.md) · [Event Store](docs/EVENT-STORE-v0.md) ·
[Type System](docs/TYPE-SYSTEM-V1.md) · [Security threat model](docs/SECURITY.md)
*(read before deploying beyond localhost)* · [ADRs](docs/adr/)

## Layout

```
cmd/kalita/    single-binary entry point  (serve · check · agent/user add)
internal/      kernel: eventstore · dsl · engine · identity · api · mcp
web/           notation-driven UI (served from disk or embedded)
packs/         product modules — the kernel knows no domains
examples/      acceptance packs
docs/          design documents + ADRs
```

## Status

Pre-alpha. The MVP kernel is code-complete and the generated UI runs; the
Service Desk / HR / CRM cores build on it without kernel gaps. **Do not deploy
outside a trusted network** — see the P0 list in [SECURITY.md](docs/SECURITY.md).
