# Kalita vNext target architecture

## Purpose
This document defines Kalita's target architecture as an event-driven enterprise agent runtime. It supersedes CRUD-first framing and treats the current schema-driven CRUD platform as a transitional access surface rather than the core operating model.

## 1. Core paradigm shift

### From what -> to what
Kalita must evolve:
- from a schema-driven CRUD/meta platform with partial workflow proposal ideas
- to an event-driven enterprise agent runtime built around `Event -> Command -> Execution`

The operating center of gravity therefore moves:
- from HTTP handlers mutating records directly
- to a durable runtime that reacts to events, evaluates policy, runs approvals, executes controlled tool steps, and emits auditable execution events

### Why CRUD is not the core
CRUD describes storage interaction, not enterprise work. Digital employees do not exist to "create rows" or "patch fields"; they exist to respond to business signals, make bounded decisions, and carry out controlled actions across systems.

CRUD can still exist, but only as an adapter layer for:
- admin maintenance
- reference-data updates
- human fallback operations
- compatibility with existing integrations

If CRUD remains the core model, Kalita will inherit the wrong architecture:
- business intent will stay trapped in transport handlers
- execution will remain synchronous and request-bound
- approvals and policy will be optional wrappers instead of mandatory control points
- observability will stop at API requests rather than business executions

### Why an event-driven model is required for digital employees
Digital employees are long-lived operational actors. They must:
- react to enterprise events from many sources, not just HTTP requests
- correlate multiple signals over time
- make proposals without executing implicitly
- wait for approvals, timers, or external callbacks
- retry safely after transient failures
- expose deterministic execution history for audit and debugging

That requires an event-driven runtime with durable execution state, explicit commands, and replayable history. The runtime must be the place where decisions become controlled actions.

## 2. Core architecture layers

### Layer 1 - Event Core
The event core is the source of truth for what happened and why execution started.

#### Event model
An `Event` is an immutable fact about something that already happened or was observed. Examples:
- `document_uploaded`
- `issues_detected`
- `approval_requested`
- `tool_execution_failed`

Events must be:
- immutable
- timestamped
- typed
- schema-validated
- attributable to a source actor or system

#### Command model
A `Command` is an explicit request for the runtime to attempt an action. Commands are not facts; they are intents such as:
- `analyze_document`
- `apply_patch`
- `request_approval`
- `retry_tool_step`

Commands must be explicit runtime envelopes so every action can pass through policy, approval, and audit.

#### Event log
A durable append-only event log records:
- domain events emitted by business sources
- runtime execution events emitted by Kalita itself
- policy and approval outcomes
- tool invocation requests and results

The event log is the observability and replay substrate, not just a debugging aid.

#### Correlation / causation
Every event and command must carry:
- `correlation_id`: ties related activity into one business thread
- `causation_id`: identifies the immediate parent event or command that caused this item
- `execution_id`: ties activity to a concrete execution instance when applicable

This turns opaque automation into traceable enterprise execution graphs.

### Layer 2 - Execution Runtime
The execution runtime converts events and commands into controlled multi-step work.

#### ExecutionInstance
An `ExecutionInstance` is the durable state machine for one business execution. It tracks:
- selected digital employee
- triggering event or command
- current step
- runtime status
- pending approvals
- retries, timeouts, and emitted results

#### Step processing
Execution progresses via explicit steps such as:
- build context
- plan/propose
- evaluate policy
- await approval
- invoke tool
- emit result event
- schedule follow-up step

Each step must have deterministic inputs and recorded outputs.

#### Scheduling / triggering
The runtime must support triggers from:
- external business events
- internal execution events
- timers / scheduled deadlines
- approval callbacks
- retry queues

The scheduler chooses what to run next; HTTP is only one possible ingress.

#### Retry / timeout
Retries and timeouts are runtime semantics, not ad hoc handler code. Each executable step should declare:
- retry policy
- idempotency key or replay safety rule
- timeout budget
- escalation path after repeated failure

### Layer 3 - Digital Employees
Digital employees are enterprise actors defined by responsibility boundaries, not chat personas.

#### Role
A digital employee has a business role such as:
- document reviewer
- invoice triage specialist
- contract compliance checker
- access request coordinator

The role defines what business outcomes the actor is accountable for.

#### Capabilities
Capabilities specify what the actor can do at runtime, for example:
- classify documents
- propose patches
- request approval
- call specific tools
- emit defined commands

Capabilities are explicit and policy-bound, not inferred from prompt text.

#### Subscriptions (to events)
A digital employee subscribes to event types and predicates, for example:
- `document_uploaded` when `document_type = contract`
- `invoice_received` when `amount > threshold`

Subscriptions determine when the runtime considers that employee for execution.

#### Allowed actions
Allowed actions enumerate which commands and tool contracts the employee may request. This prevents accidental overreach and creates a deterministic control surface.

### Layer 4 - Policy & Control
Policy is mandatory infrastructure between proposal and execution.

#### Policy rules
Policy rules evaluate execution context, actor identity, tool class, data sensitivity, and business state. Rules can produce outcomes such as:
- allow
- allow_with_audit_marker
- require_approval
- require_human_review
- deny

#### Risk levels
Risk should be first-class, using controlled classes such as:
- low
- medium
- high
- critical

Risk can be derived from entity sensitivity, tool type, operation type, affected scope, or external side effects.

#### Approval model
Approvals are durable runtime objects, not UI dialogs. The model must support:
- approval policies by risk and action type
- approver routing by role or queue
- expiry / timeout
- approve / reject / request_changes outcomes
- linkage back to the execution instance

#### Enforcement points
Policy enforcement must occur at every action boundary:
- command admission
- execution start
- tool invocation
- side-effectful step completion
- retry escalation

No execution path may bypass policy because of transport choice or LLM involvement.

### Layer 5 - Tool System
Tools are controlled execution adapters, not arbitrary model permissions.

#### Tool definition
A `ToolDefinition` describes a callable enterprise capability such as:
- source code patch application
- document OCR
- email dispatch
- ERP update
- ticket creation

#### Contracts
Every tool must expose a typed contract:
- input schema
- output schema
- side-effect classification
- idempotency behavior
- timeout and retry policy
- required policy gates

#### Execution rules
Tools execute only when:
- selected by runtime step logic
- permitted for the digital employee
- approved by policy
- approved by humans when required
- executed through the runtime adapter

LLMs may recommend a tool call, but they never invoke tools directly.

### Layer 6 - Observability
Observability must explain business execution, not only infrastructure events.

#### Event log
Operators must see the event sequence that led to each action.

#### Execution timeline
Each `ExecutionInstance` should expose a timeline of:
- trigger
- selected employee
- context build
- proposal generation
- policy decisions
- approval wait states
- tool attempts
- emitted result events

#### Reasoning visibility
Kalita should store bounded reasoning artifacts such as:
- proposal summary
- evidence references
- policy explanation
- approval justification
- tool input/output summaries

The goal is visibility into why the system proposed or executed something, without making prompts the system of record.

#### Debugging
Debugging should support:
- replay from event history
- inspection by `correlation_id` / `execution_id`
- deterministic re-run of non-side-effecting steps
- side-by-side comparison of proposal vs approved execution

### Layer 7 - Access Layer (NOT CORE)
The access layer provides compatibility and operator access, but it must not own execution semantics.

#### CRUD (optional)
CRUD remains useful for:
- administrative entity maintenance
- managing reference data
- exposing transitional APIs for existing clients
- manual correction workflows

CRUD should write through command boundaries where appropriate and should no longer be considered the primary business model.

#### Admin API
The admin API manages runtime metadata, diagnostics, registrations, policies, and replay tooling.

#### UI adapters
UI can include:
- operator consoles
- approval inboxes
- execution dashboards
- entity maintenance screens

These are adapters onto the runtime and event model, not the architectural center.

## 3. Core domain entities

### Event
**Purpose**
- Immutable fact representing something observed or completed.

**Fields**
- `event_id`
- `event_type`
- `aggregate_type`
- `aggregate_id`
- `payload`
- `occurred_at`
- `recorded_at`
- `source`
- `actor_context_id`
- `correlation_id`
- `causation_id`
- `execution_id` (optional)
- `schema_version`

**Lifecycle role**
- Starts executions, advances workflows, records outcomes, and provides replay/audit history.

### Command
**Purpose**
- Explicit instruction for the runtime to attempt an action under policy control.

**Fields**
- `command_id`
- `command_type`
- `target_type`
- `target_id`
- `payload`
- `requested_by`
- `actor_context_id`
- `correlation_id`
- `causation_id`
- `execution_id` (optional)
- `requested_at`
- `idempotency_key`
- `risk_hint`

**Lifecycle role**
- Admitted by runtime, evaluated by policy, transformed into execution steps, then either completed or rejected with explicit results.

### ExecutionInstance
**Purpose**
- Durable state machine for one runtime execution.

**Fields**
- `execution_id`
- `execution_type`
- `status` (`pending`, `running`, `waiting_approval`, `waiting_timer`, `succeeded`, `failed`, `cancelled`)
- `digital_employee_id`
- `trigger_event_id`
- `trigger_command_id`
- `current_step`
- `context_snapshot_ref`
- `policy_state`
- `approval_state`
- `attempt_count`
- `started_at`
- `updated_at`
- `completed_at`
- `correlation_id`

**Lifecycle role**
- Anchors the full execution history and coordinates retries, waiting states, and completion.

### ExecutionEvent
**Purpose**
- Runtime-internal fact describing execution progress.

**Fields**
- `execution_event_id`
- `execution_id`
- `step_name`
- `event_type`
- `status`
- `payload`
- `occurred_at`
- `causation_id`
- `correlation_id`

**Lifecycle role**
- Captures step transitions, failures, retries, waits, and completion inside the runtime timeline.

### ActorContext
**Purpose**
- Stable security and accountability envelope for whoever initiated or influenced execution.

**Fields**
- `actor_context_id`
- `actor_type` (`human`, `service`, `digital_employee`, `system`)
- `actor_id`
- `tenant_id`
- `roles`
- `capabilities`
- `authn_context`
- `request_origin`
- `impersonation_chain`
- `policy_tags`
- `created_at`

**Lifecycle role**
- Travels with events, commands, approvals, and tool calls so policy and audit are actor-aware.

### DigitalEmployee
**Purpose**
- Configured enterprise actor that owns a bounded responsibility domain.

**Fields**
- `digital_employee_id`
- `name`
- `role`
- `description`
- `subscriptions`
- `capability_set`
- `allowed_command_types`
- `allowed_tool_ids`
- `default_policy_profile`
- `proposal_mode`
- `status`
- `version`

**Lifecycle role**
- Selected by runtime for qualifying events, then used to scope planning, proposals, and allowed actions.

### DecisionProposal
**Purpose**
- Structured proposal generated by deterministic logic and optionally LLM-assisted reasoning.

**Fields**
- `proposal_id`
- `execution_id`
- `proposed_by`
- `proposal_type`
- `input_evidence_refs`
- `recommended_commands`
- `recommended_tool_calls`
- `reasoning_summary`
- `confidence`
- `created_at`
- `superseded_by` (optional)

**Lifecycle role**
- Serves as advisory output for policy and human review; never executes directly.

### PolicyRule
**Purpose**
- Declarative rule that constrains execution and side effects.

**Fields**
- `policy_rule_id`
- `name`
- `scope`
- `match_conditions`
- `risk_level`
- `decision_mode`
- `required_approver_role` (optional)
- `effect`
- `version`
- `status`

**Lifecycle role**
- Evaluated at control points to determine whether an action may proceed, must pause, or must be denied.

### PolicyDecision
**Purpose**
- Recorded outcome of applying policy rules to a concrete action.

**Fields**
- `policy_decision_id`
- `execution_id`
- `command_id` or `tool_invocation_id`
- `matched_rule_ids`
- `decision`
- `risk_level`
- `explanation`
- `requires_approval`
- `decided_at`

**Lifecycle role**
- Becomes part of the audit trail and gates the next runtime transition.

### ApprovalRequest
**Purpose**
- Durable request for human authorization of a risky or exceptional action.

**Fields**
- `approval_request_id`
- `execution_id`
- `subject_type`
- `subject_id`
- `requested_action`
- `risk_level`
- `requested_from_role`
- `status`
- `decision`
- `decision_reason`
- `requested_at`
- `responded_at`
- `expires_at`

**Lifecycle role**
- Pauses execution until resolved, then emits approval outcome events back into the runtime.

### ToolDefinition
**Purpose**
- Typed description of an executable integration or action adapter.

**Fields**
- `tool_id`
- `name`
- `category`
- `input_schema`
- `output_schema`
- `side_effect_level`
- `idempotency_mode`
- `timeout_seconds`
- `retry_policy`
- `required_policy_profile`
- `status`
- `version`

**Lifecycle role**
- Defines the only legal path for runtime tool execution and gives policy enough structure to control it.

## 4. Execution lifecycle
1. **Event occurs**
   - A business fact such as `document_uploaded` is appended to the event log with correlation, causation, and actor context.

2. **Runtime selects executor**
   - Subscription rules identify candidate digital employees.
   - Runtime resolves one execution plan owner based on role, capability, scope, and policy.

3. **Context is built**
   - The runtime assembles `ActorContext`, event payload, relevant entity state, prior execution history, policy metadata, and referenced artifacts into a deterministic execution context.

4. **LLM (optional) produces proposal**
   - If the selected employee has proposal capabilities that benefit from model assistance, the LLM receives bounded context and returns a structured `DecisionProposal`.
   - The proposal may recommend commands, evidence links, and rationale, but it does not execute tools or mutate state.

5. **Policy evaluates**
   - Proposed commands and tool intents are checked against `PolicyRule` objects.
   - The runtime records a `PolicyDecision` explaining whether the proposal is allowed, denied, or requires approval.

6. **Approval (if needed)**
   - If policy requires approval, the runtime emits `approval_requested`, persists an `ApprovalRequest`, and pauses the execution instance in `waiting_approval`.
   - Approval outcomes re-enter as events such as `approval_granted` or `approval_rejected`.

7. **Tool executes**
   - Only after policy allows and approvals clear does the runtime invoke a `ToolDefinition` through the execution adapter.
   - The invocation uses typed inputs, idempotency keys, timeout budgets, and retry semantics.

8. **Result emitted as events**
   - Tool outcomes and business results are turned into durable events such as `issues_detected`, `patch_applied`, or `tool_execution_failed`.
   - These events may trigger downstream employees or follow-up steps.

9. **Execution state updated**
   - The runtime records `ExecutionEvent` entries, updates the `ExecutionInstance` status, schedules retries or timers if needed, and marks the execution completed only when all required steps are finalized.

## 5. Mapping from current system

| Current Component | Future Role | Action |
| ----------------- | ----------- | ------ |
| `internal/schema` DSL parser and model | Schema contract for events, commands, tool inputs, and access-layer entities | Reuse and extend gradually so schema remains the static contract surface. |
| `internal/validation` | Command, event, and tool payload validator | Reuse as the validation engine; expand from CRUD payload checks into event/command contract enforcement. |
| `internal/runtime/storage.go` in-memory store | Transitional runtime state and eventually execution-state abstraction | Refactor away from record-only storage toward event log, execution persistence, and projections. |
| `internal/http/handlers.go` CRUD orchestration | Access-layer adapter only | Demote from business execution center; move orchestration into runtime services behind transport-agnostic interfaces. |
| `internal/http/actions.go` / action request flows | Early command envelope entry point | Generalize into first-class command submission rather than transport-bound action handling. |
| `internal/http/admin.go` reload/admin surface | Runtime administration and diagnostics | Extend to manage event runtime metadata, replay controls, policy diagnostics, and registrations. |
| `internal/catalog` YAML catalogs | Controlled vocabularies for policy, approval, risk, and role metadata | Reuse directly for risk classes, approver routing classes, tool categories, and policy enums. |
| `internal/postgres` DDL generation | Durable runtime persistence substrate and projections | Reuse as the persistence integration point, but pivot from schema-only DDL toward event/execution tables and read models. |
| Existing workflow proposal support | Proposal subsystem for digital employees | Generalize into structured `DecisionProposal` generation that feeds policy and approvals instead of directly shaping CRUD flows. |
| CRUD/meta APIs | Access-layer compatibility surface | Keep as optional adapters and projections; do not let them define core architecture. |

## 6. Minimal migration strategy
Do not rewrite the repository. Introduce runtime-first seams in small steps.

### Introduce event model
- Add a first-class event envelope and append-only event persistence abstraction.
- Start by emitting events for current HTTP write paths and action flows.
- Keep existing CRUD behavior while adding event capture as the new source of execution truth.

### Introduce command envelope
- Define an explicit command object for any operation that intends side effects.
- Route existing action endpoints and selected CRUD mutations through command admission before execution.
- Preserve current API shapes by translating transport requests into commands internally.

### Extract execution from HTTP
- Create a transport-agnostic runtime service that receives commands/events and manages execution instances.
- Make HTTP handlers thin adapters that submit commands and fetch projections.
- Move retries, approval waits, and orchestration state out of request handlers.

### Add actor context
- Introduce `ActorContext` in every ingress path.
- Propagate actor identity, role, tenant, and origin through events, commands, approvals, and tool calls.
- Use this as the foundation for policy and audit rather than relying on request-local metadata.

### Make execution durable
- Persist execution instances, execution events, approval requests, and policy decisions.
- Do not start with a full event-sourced rewrite; start with durable append-only records plus read projections.
- Ensure interrupted executions can resume without relying on in-memory request context.

## 7. First 5 implementation slices

### Slice 1 - Event envelope and event emission
**Goal**
- Introduce a typed event model and append-only event writer.

**Scope**
- Define `Event`, correlation metadata, and a basic persistence interface.
- Emit domain events from a narrow path such as document upload or action submission.

**Why it matters**
- Creates the foundation for runtime-driven execution without changing the whole system.

**Testability**
- Unit test event validation and persistence.
- Integration test that a triggering HTTP action emits the expected event.

### Slice 2 - Command admission layer
**Goal**
- Add a command envelope for side-effecting operations.

**Scope**
- Define `Command` and command admission service.
- Route one existing action path through command admission before current execution logic.

**Why it matters**
- Establishes that execution starts from explicit commands, not arbitrary handler code.

**Testability**
- Unit test command validation, idempotency behavior, and rejection scenarios.
- Integration test that admitted commands are recorded and correlated to source events.

### Slice 3 - Execution runtime skeleton
**Goal**
- Create durable `ExecutionInstance` and `ExecutionEvent` records with a minimal step runner.

**Scope**
- Start a runtime execution from a command or event.
- Record statuses such as `pending`, `running`, `succeeded`, and `failed`.

**Why it matters**
- Moves orchestration responsibility out of HTTP and into a dedicated runtime core.

**Testability**
- Unit test state transitions.
- Integration test crash-safe resume semantics for a simple non-side-effecting execution.

### Slice 4 - Actor context injection
**Goal**
- Ensure all ingress paths produce `ActorContext` and propagate it through runtime records.

**Scope**
- Add actor metadata capture in HTTP and admin flows.
- Store actor context IDs on commands, events, and executions.

**Why it matters**
- Enables policy, audit, and approval to be actor-aware from the beginning.

**Testability**
- Unit test actor context construction and propagation.
- Integration test that audit/event records include actor identity and origin.

### Slice 5 - Policy hook before tool/action execution
**Goal**
- Introduce a policy decision point before executing side effects.

**Scope**
- Define `PolicyRule` / `PolicyDecision` models.
- Add a simple allow / require approval / deny evaluation for one execution path.

**Why it matters**
- Hardens the non-negotiable control boundary between proposal and execution.

**Testability**
- Unit test rule matching and decision outcomes.
- Integration test that high-risk commands pause for approval instead of executing immediately.

## 8. Anti-patterns
Kalita vNext must not do the following:
- **Chat-first architecture**: do not model digital employees as conversational agents waiting for user prompts.
- **Direct LLM tool execution**: LLM output must never invoke tools or mutate systems without runtime control.
- **Business logic in handlers**: transport handlers must not own execution policies, retries, or workflow rules.
- **State in prompts**: prompts may assist proposal generation, but durable execution state must live in runtime records and events.
- **CRUD as core model**: record mutation cannot remain the primary representation of enterprise work.
- **Approval as UI-only behavior**: approvals must be runtime objects and events, not front-end conventions.
- **Opaque automation**: every proposal, decision, action, and outcome must be attributable and auditable.
- **In-memory-only execution**: long-running digital employee work must survive process restarts.

## 9. MVP scenario: Document Review Digital Employee

### Actor definition
A `Document Review Digital Employee` owns first-pass review of uploaded documents and can propose safe patches, but cannot apply changes without runtime and policy clearance.

### Flow
1. **`document_uploaded`**
   - A document enters the system through upload or integration.
   - Kalita emits `document_uploaded` with document metadata, uploader actor context, and storage reference.

2. **`analyze_document`**
   - The runtime matches the document review employee subscription.
   - A command `analyze_document` starts an execution instance.

3. **`issues_detected`**
   - The employee builds context from the document, metadata, and relevant rules.
   - An optional LLM produces a `DecisionProposal` listing detected issues and evidence spans.
   - The runtime emits `issues_detected` as a fact about the analysis outcome.

4. **`patch_proposed`**
   - If remediations are possible, the proposal includes a structured patch recommendation.
   - The runtime records `patch_proposed` with the proposed change set and supporting rationale.

5. **`approval_requested`**
   - Policy evaluates the patch based on document class, impact, and side-effect level.
   - If human authorization is required, the runtime creates an `ApprovalRequest` and emits `approval_requested`.

6. **`patch_applied`**
   - After approval, the runtime invokes the patch application tool using the approved payload.
   - The tool result becomes `patch_applied` or `tool_execution_failed`.

7. **`audit recorded`**
   - Throughout the flow, Kalita records event history, execution timeline, proposal summary, policy decisions, approval outcome, and tool result summaries for audit and debugging.

### Why this is a good MVP
This scenario proves the target architecture because it requires:
- event trigger ingestion
- runtime ownership of execution
- optional LLM proposal generation
- policy gate before action
- approval pause/resume
- controlled tool execution
- durable audit trail

It demonstrates digital employee behavior without collapsing back into CRUD or chat patterns.

## 10. Final recommendation
The first thing to implement in the repository right now is **a first-class event and command envelope with a minimal durable execution runtime skeleton**.

Concretely, the immediate repo priority should be:
1. define `Event`, `Command`, `ExecutionInstance`, and `ActorContext` as core runtime models
2. emit events from one existing action path and one document/file path
3. create a transport-agnostic runtime service that starts and records execution instances
4. store execution and policy history durably
5. keep CRUD intact, but route new side-effecting flows through the runtime boundary

This is the smallest change that establishes Kalita's real architectural center of gravity. Once that exists, policy, approvals, digital employees, and tool control can all accumulate on the correct foundation instead of being bolted onto CRUD handlers later.
