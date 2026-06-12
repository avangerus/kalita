# Kalita — High-Level Design

Version 0.1 (2026-06-12). Status: draft for discussion.

Kalita is an executable runtime for business systems built for the agentic era: agents and humans describe the system in a constrained DSL, the runtime executes the description (no code generation to the outside), every change is a signed diff, and every action is an event in the audit log.

---

## 1. Principles (architectural constitution)

1. **The spec is executed, not compiled to code.** Generated code as an external artifact is forbidden (lesson from MDA/CASE: spec-and-code drift kills the single source of truth).
2. **Lego, not welding.** Anything that cannot be expressed by the DSL grammar does not exist. Extending capabilities is done through components with a typed contract (escape hatch), not by growing the grammar until "anything goes".
3. **Event sourcing at the core.** Truth is an append-only event log; current state is a projection. Without this, the following are impossible: simulation before signing, time travel, agent reputation, insurability (layers 2–3).
4. **Signature is a first-class cryptographic entity.** Who, what, when, and on what basis. The log must have evidentiary force.
5. **Agent = employee.** An agent has an identity, a job title (role), permissions, a rating, and a log. The runtime does not care what the agent thinks with (Claude/LangGraph/other) — the runtime constrains what the agent can do.
6. **Think big, commit small.** Every system change is a small DSL diff through the pipeline: validate → simulate → sign → apply with migration.
7. **On-prem first.** A single binary/container; data never leaves the perimeter. Cloud is a deployment option, not an architectural one.

---

## 2. Overall architecture

```
                    ┌─────────────────────────────────────────────┐
  Agents            │                KALITA NODE                  │
 (Claude Code,      │                                             │
  custom)     ──────┤  ┌──────────────┐      ┌─────────────────┐  │
        │ MCP/REST  │  │ Agent Gateway│      │   Web UI (auto) │◄─┼── Humans (browser)
        ▼           │  │ identity,    │      │ lists, forms,   │  │
  ┌───────────┐     │  │ scopes, rate │      │ boards, signing │  │
  │ External  │     │  └──────┬───────┘      └────────┬────────┘  │
  │ systems   │◄────┤         ▼                       ▼           │
  │ (webhooks,│     │  ┌─────────────────────────────────────┐    │
  │  REST)    │     │  │            API CORE (REST)          │    │
  └───────────┘     │  └──────┬──────────────────────┬───────┘    │
                    │         ▼                      ▼            │
                    │  ┌─────────────┐      ┌────────────────┐    │
                    │  │ CHANGE       │      │ RUNTIME KERNEL │    │
                    │  │ PIPELINE     │      │                │    │
                    │  │ diff→validate│      │ Entity Engine  │    │
                    │  │ →simulate    │      │ Workflow Engine│    │
                    │  │ →sign→apply  │      │ Permission Eng.│    │
                    │  │ (+migrations)│      │ Automation Eng.│    │
                    │  └──────┬───────┘      └───────┬────────┘    │
                    │         ▼                      ▼            │
                    │  ┌──────────────────────────────────────┐   │
                    │  │  EVENT STORE (append-only, signed)    │   │
                    │  │  + Projections (current state)        │   │
                    │  │  + Meta Store (DSL versions, packs)   │   │
                    │  └──────────────────────────────────────┘   │
                    │                  PostgreSQL                  │
                    └─────────────────────────────────────────────┘
```

---

## 3. Components

### 3.1 DSL Layer
- **Grammar** (EBNF, versioned): `entity`, `workflow`, `permissions`, `automation`, `ui`, `pack` (module). Minimal by principle #2.
- **Packs** — the unit of distribution: a set of DSL files + version + dependencies + manifest. The format is marketplace-ready from day one (semver, author signature).
- The grammar is published in machine-readable form (for constrained decoding: an agent generates DSL under the grammar — invalid constructs are inexpressible at the token level).

### 3.2 Compiler / Validator
- Parse → typed model (AST → Semantic Model).
- Checks: referential integrity (all `ref`s, roles, and transitions exist), non-contradictory permissions, workflow is reachable with no dead ends.
- Output: `SystemDefinition vN` + **MigrationPlan** (diff between vN-1 and vN at the schema and data level).

### 3.3 Runtime Kernel
- **Entity Engine:** CRUD over projections, validation, computed fields, referential integrity. Storage: PostgreSQL (jsonb + indexed columns); projections are rebuildable from events.
- **Workflow Engine:** states, transitions, guards, `assignee` (human/agent/auto), `requires approval(role)` — a HITL transition creates a signing task.
- **Permission Engine:** RBAC + ABAC (role, ownership, field-level, classification). Single choke point: every action by any subject (human, agent, automation) passes through it. Denials can be expressed explicitly (`deny [...] `).
- **Automation Engine:** triggers (`when` conditions), schedules, SLA/escalations, invoking agents as step executors.

### 3.4 Event Store + Signatures
- Append-only log: `{event_id, ts, actor (human|agent|system), action, payload, basis (rule/hint/ADR), signature?}`.
- Signature: Ed25519, subject keys; DSL changes and HITL decisions are signed. Hash chain over the log (tamper-evident).
- Built from events: current state, history of any object, simulations, agent ratings (layer 2).

### 3.5 Change Pipeline (the platform's main conveyor)
```
Proposal(diff DSL, author, basis)
  → Validate (compiler: green/red)
  → Simulate (optional: run the rule over historical events → impact report)
  → Sign (role with signing authority; UI "signing queue")
  → Apply (atomic: new SystemDefinition version + data migration + event)
  → Rollback (any version is restorable: definitions are versioned, data is event-sourced)
```
Directly modifying the system bypassing the pipeline does not exist as an API operation.

### 3.6 Agent Gateway (MCP)
- MCP server — a first-class interface (equal to the Web UI): tools `describe_system`, `query/mutate entities`, `propose_change(diff)`, `take_task / complete_task`, `read_journal`.
- Each agent: identity + role + scopes + rate limits + rating. No anonymous agents.
- Agent mutate operations = the same Permission Engine rights as a human in that role.

### 3.7 UI (generated)
- From `ui` sections of the DSL: lists (filters, sorts, views), forms (sections, widgets by type), status boards, **signing queue** (the main human screen), object journal.
- Implementation: SPA (React) on top of the Meta API; no manual layout per application. Custom widgets — via escape hatch.

### 3.8 Escape Hatch (components)
- External functions/components with a typed contract (typed input/output, side-effect declaration), called from DSL (`agent:` steps, computed fields, integrations, custom widgets).
- A component cannot: bypass the Permission Engine, write to the store directly, or modify definitions. Execution is sandboxed (separate process/WASM — to be decided in detailed design).

---

## 4. Key flows

**A. System creation:** an agent interviews users or reads Excel → `propose_change(full DSL)` → validation → human reviews DSL + UI preview → sign → runtime deploys (tables, API, UI) — minutes.

**B. Change:** "don't contact VIPs" → agent sends a 3-line diff → simulation: "would have affected 12 requests over 90 days" → sign → applied with migration.

**C. Agent as worker:** trigger `when overdue_days=14` → task assigned to agent → agent acts within scopes → event written to log with basis → a critical transition creates a signing task for a human.

**D. Rollback:** any applied change is reverted as a new proposal (also goes through signing).

---

## 5. Technology stack (proposed)

| Layer | Choice | Why |
|---|---|---|
| Core | **Go** | best-in-class single static binary; the language of open-source infrastructure (Kubernetes, Terraform, Grafana, Temporal) → maximum contributor pool; small footprint for on-prem; simple language = smaller drift surface for agent-generated code. Founder's C# background is deliberately NOT a criterion: code is written by an agent pipeline; the language is chosen for the ecosystem |
| Storage | PostgreSQL 16 | event log + jsonb projections + full-text; single dependency |
| DSL parser | custom, hand-written recursive descent | the grammar is core IP; perfect error control and messages for agents are required |
| Sandbox (escape hatch) | WASM (wazero, pure-Go) | determinism, capability-based, cross-platform (including Windows), language-agnostic components, ready for a marketplace of untrusted packs. Process isolation was rejected: not portable |
| UI | React + Meta API | generated from metadata |
| MCP | in core (HTTP + stdio) | first-class interface |
| Deploy | Docker Compose (kalita + postgres); one client = one node | on-prem in 10 minutes; isolation "your node is your perimeter" instead of a shared schema |

---

## 6. Security
- Subjects: humans (OIDC/local), agents (keys), automations. All actions go through the Permission Engine and all are written to the log.
- Classification on entities/fields (`deny [view Contracts with classification]`).
- Integration secrets are stored in the core's local vault, not in the DSL.
- Network: one ingress port; outbound only to integrations declared in the DSL (important for the perimeter: "what goes out" is visible in the spec).

## 7. Deployment and scale
- MVP: single node, vertical scaling; target systems are tens to hundreds of users per node (a 1C-style profile, not Netflix).
- Later: read replicas for projections; rebuilding projections from events = free migration/recovery.

---

## 8. Phases

### MVP (8 weeks) — layer 1
Included: grammar v0 (entity, workflow, permissions, minimal automation, minimal ui), compiler + additive migrations (no destructive), Entity/Workflow/Permission engines, Event Store **with signatures (day 1!)**, Change Pipeline without simulation, MCP server, UI generation (list/form/board/signing queue), Docker.
Not included: simulation, analytics/dashboards, i18n, marketplace, cross-org, escape hatch (stub contract), destructive migrations (manual procedure only).

### Layer 2 (6–18 months): simulation before signing, agent ratings, pack library with survivability telemetry, drift detector on the signature corpus.

### Layer 3 (18+ months): cross-organizational contracts, portable agent reputation, signer marketplace, insurability.

**Two irreversible day-1 decisions that protect layers 2–3:** event sourcing as the single source of truth; cryptographic signatures in the log schema. They cost a page of code now — and are impossible to add retroactively.

---

## 8a. Scaling and federation (principle, 2026-06-13)

One node = one domain of one company (bounded context): writes —
hundreds of events/sec (serialized via advisory lock), reads from in-memory projections,
startup — replay (budget ADR-001). More than sufficient for ICP. Sharding
a single domain is never done; HA — PG replica + standby replay (v1).

Growth = domain decomposition: "microservices from kalitas". The log is a ready-made outbox
(seq + idempotency = exactly-once); the bridge between nodes is an agent with identity and
deny-boundaries on both sides; schema exchange — packs through the change pipeline
(contract drift is inexpressible). **Rule: no synchronous RPC between nodes —
only asynchronous events through the bridge agent.** Reserved:
`remote[node.Entity]` (DSL-SPEC §10b), layer 3.

## 9. Main risks (honestly)
1. **Migration engine** — the most complex engineering in the project (not the parser, not the UI). Limit the MVP to additive migrations.
2. **DSL expressiveness ceiling** — cured by the escape hatch, not grammar growth; discipline determines the fate of the project.
3. **Generated UI quality** — "good enough" for record-keeping systems is achievable; pixel-perfect is not the goal.
4. **Window:** Tessl/Kiro/Supabase are one step away from the niche. Speed > completeness.
5. **Solo maintainer** — bus factor 1; mitigation: open core + early contributors.

## 10. Resolved detailed design questions
- **DSL syntax: English keywords only.** Bilingualism was rejected: two dialects = drift and pack fragmentation; international market and model training data are in English. Label/message localization is an i18n layer, not grammar.
- **Sandbox: WASM** (see stack). Stub in the MVP, but the component contract is designed for WASM from the start.
- **Multi-tenancy: one client = one node** (MVP and cloud: node-per-tenant, not shared schema — simpler and more honest on security).

Open question: simulation format (full replay vs. selective over affected rules) — to be decided based on event store performance.
