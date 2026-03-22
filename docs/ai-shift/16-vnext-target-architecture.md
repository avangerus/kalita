# Kalita vNext target architecture

## Purpose
This document defines Kalita's target architecture as an event-driven enterprise agent runtime. It supersedes CRUD-first framing and treats the current schema-driven CRUD platform as a transitional access surface rather than the core operating model.

## 1. Core paradigm shift

### From what -> to what
Kalita must evolve:
- from a schema-driven CRUD/meta platform with partial workflow proposal ideas
- to an event-driven enterprise agent runtime built around `Event -> Case -> Plan -> Execution`

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

That requires an event-driven runtime with durable execution state, explicit commands, and replayable history. The runtime must be the place where decisions become controlled actions, but it cannot be the only business construct. Real departments operate through owned cases, staffed queues, and daily plans, so Kalita needs an operational layer between signals and execution.

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

### Layer 2 - Case-Centric Operations
The operational layer converts events into department work before any execution starts. It is the missing business department model between raw signals and runtime mechanics.

#### Why a case-centric layer is required
A real department does not react to every event by immediately executing tools. It groups related facts into business matters, decides who owns them, places them into work queues, and plans the day before workers perform actions. Kalita therefore needs first-class operational objects for:
- cases as the unit of departmental responsibility
- queues as the unit of work intake and balancing
- daily plans as the unit of managerial prioritization
- assignments as the unit of accountability

#### Case model
A `Case` is the durable business container for a matter that may span many events, reviews, approvals, promises, and executions over time. Examples include:
- a debt collection case for one debtor and invoice cluster
- a dispute resolution case
- a missing-document follow-up case

Cases are not generic workflow records. They represent the department's active responsibility for an outcome.

#### Work model
The operational layer creates work items from cases instead of handing events directly to executors. This enables:
- queue-based intake
- employee assignment
- manager-directed rebalancing
- SLA monitoring by queue and stage
- controlled carry-over from prior days

#### Planning model
A department works from a daily plan rather than a flat stream of triggers. Planning decides:
- what must be handled today
- what can wait
- which employee owns which cases
- which escalations or high-risk actions require manager attention

Only after the case is in plan does Kalita authorize concrete execution steps.

### Layer 3 - Execution Runtime
The execution runtime converts planned case work into controlled multi-step work.

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

### Layer 4 - Digital Employees
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

### Layer 5 - Policy & Control
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

### Layer 6 - Tool System
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

### Layer 7 - Observability
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

### Layer 8 - Access Layer (NOT CORE)
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

### Case
**Purpose**
- Durable business container representing a matter the department is responsible for progressing to an outcome.

**Fields**
- `case_id`
- `case_type`
- `case_reference`
- `status` (`open`, `active`, `waiting_customer`, `waiting_external`, `planned`, `in_progress`, `resolved`, `closed`, `cancelled`)
- `stage` (department-specific stage such as `new`, `collector_review`, `promise_to_pay`, `escalated`, `legal_hold`)
- `priority`
- `severity`
- `department`
- `queue_id`
- `owning_employee_id` (optional)
- `managing_team_id`
- `subject_type`
- `subject_id`
- `account_id` or `counterparty_id`
- `opened_at`
- `last_activity_at`
- `next_review_at`
- `sla_deadline_at`
- `resolution_code` (optional)
- `correlation_id`
- `source_event_ids`

**Lifecycle role**
- Aggregates business events into a managed matter, holds responsibility and stage, and supplies the operational context from which plans and executions are derived.

### WorkQueue
**Purpose**
- Department intake and balancing surface where cases compete for attention under explicit operational rules.

**Fields**
- `queue_id`
- `name`
- `department`
- `queue_type` (`intake`, `specialist`, `manager_review`, `exception`, `follow_up`)
- `entry_criteria`
- `priority_policy`
- `assignment_policy`
- `capacity_policy`
- `sla_policy`
- `manager_role`
- `status`

**Lifecycle role**
- Receives case-derived work, orders demand, and exposes assignable backlog to digital and human employees.

### WorkItem
**Purpose**
- Concrete unit of departmental effort representing what an employee needs to advance on a case now.

**Fields**
- `work_item_id`
- `case_id`
- `queue_id`
- `work_type`
- `status` (`ready`, `planned`, `assigned`, `in_progress`, `blocked`, `done`, `cancelled`)
- `required_role`
- `assigned_employee_id` (optional)
- `planned_for_date` (optional)
- `priority_score`
- `reason`
- `due_at`
- `created_from_event_id`
- `created_at`
- `completed_at` (optional)

**Lifecycle role**
- Bridges case state into daily actionable effort without losing case ownership or queue accountability.

### DailyPlan
**Purpose**
- Explicit operational plan for what an employee or team should handle during a working day.

**Fields**
- `daily_plan_id`
- `plan_date`
- `department`
- `employee_id` or `team_id`
- `queue_scope`
- `planning_status` (`draft`, `published`, `in_progress`, `completed`, `superseded`)
- `planning_inputs`
- `capacity_budget`
- `priority_rules_snapshot`
- `manager_overrides`
- `planned_work_item_ids`
- `created_at`
- `published_at` (optional)

**Lifecycle role**
- Converts backlog into a bounded, prioritized sequence of case work for the day and records manager direction.

### DigitalEmployee
**Purpose**
- Configured enterprise actor that owns a bounded responsibility domain and operates like a department worker, not merely an executor.

**Fields**
- `digital_employee_id`
- `name`
- `role`
- `description`
- `subscriptions`
- `queue_memberships`
- `case_types_owned`
- `planning_policy`
- `capacity_profile`
- `capability_set`
- `allowed_command_types`
- `allowed_tool_ids`
- `default_policy_profile`
- `proposal_mode`
- `status`
- `version`

**Lifecycle role**
- Takes events into owned queues and cases, participates in plan formation, accepts assignments, and executes allowed actions while remaining accountable for case progression.

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

## 4. Operational flow: Event -> Case -> Plan -> Employee -> Execution
1. **Event occurs**
   - A business fact such as `invoice_overdue_detected`, `payment_promise_broken`, or `document_uploaded` is appended to the event log with correlation, causation, and actor context.

2. **Event is correlated into a case**
   - Case rules decide whether the event opens a new `Case`, reactivates an existing one, or enriches a current matter.
   - The event updates case stage, deadlines, activity timestamps, and operational indicators such as broken-promise count or dispute status.
   - A single case may accumulate many events over days or weeks; the department works the case, not isolated events.

3. **Case generates work in a queue**
   - Operational rules create or refresh one or more `WorkItem` objects for the case.
   - The case is routed into a `WorkQueue` based on stage, risk, amount, customer segment, SLA pressure, or exception status.
   - Queue membership expresses departmental demand and backlog, not yet execution.

4. **Daily planning selects what gets worked today**
   - At planning time, Kalita builds `DailyPlan` objects per employee or team using queue backlog, SLA deadlines, capacity, priorities, and carry-over work.
   - Prioritization is not just event recency; it reflects departmental intent such as collector cadence, promised follow-up dates, customer value, and manager escalations.
   - Managers can override the machine-generated plan by pinning urgent cases, reserving capacity for a campaign, or rerouting high-risk matters to specialists.

5. **An employee accepts assigned case work**
   - A `DigitalEmployee` or human employee works from its published queue slice and daily plan.
   - Assignment rules can be round-robin, skill-based, relationship-based, territory-based, or manager-directed, but responsibility always resolves to a named owner.
   - The employee becomes accountable for advancing the case stage, not merely executing one command.

6. **Execution is started from planned work**
   - When the employee begins a `WorkItem`, the runtime creates an `ExecutionInstance` tied to the case, work item, employee, and triggering event set.
   - The runtime assembles `ActorContext`, case history, queue metadata, plan position, policy metadata, and referenced artifacts into a deterministic execution context.

7. **LLM (optional) produces proposal**
   - If the selected employee has proposal capabilities that benefit from model assistance, the LLM receives bounded case and plan context and returns a structured `DecisionProposal`.
   - The proposal may recommend next-best actions, commands, evidence links, and rationale, but it does not execute tools or mutate state.

8. **Policy and approvals evaluate the proposed action**
   - Proposed commands and tool intents are checked against `PolicyRule` objects together with case stage, owner role, amount at risk, and managerial constraints.
   - The runtime records a `PolicyDecision` explaining whether the proposal is allowed, denied, or requires approval.
   - If needed, the runtime emits `approval_requested`, persists an `ApprovalRequest`, and pauses the execution instance in `waiting_approval` until an approval outcome event arrives.

9. **Tool or action executes under control**
   - Only after policy allows and approvals clear does the runtime invoke a `ToolDefinition` or emit a side-effecting command through the execution adapter.
   - The invocation uses typed inputs, idempotency keys, timeout budgets, retry semantics, and the current case responsibility envelope.

10. **Results update both execution and operations**
   - Tool outcomes and business results are turned into durable events such as `contact_attempt_logged`, `promise_to_pay_recorded`, `patch_applied`, or `tool_execution_failed`.
   - Those results update the `ExecutionInstance`, the `Case` stage/lifecycle, queue ordering, next review date, and the next day's planning inputs.
   - Kalita closes the work item only when the employee's operational responsibility for that step is satisfied; the case remains open until the business matter is actually resolved.

## 5. Operational model details

### Case lifecycle
A case lifecycle must reflect departmental operations rather than generic workflow statuses. A typical lifecycle is:
- `open`: the matter has been created from one or more triggering events
- `active`: the department has accepted responsibility and the case is in an owned queue
- `planned`: the case has scheduled work in an employee or team daily plan
- `in_progress`: a worker is currently performing an execution step on behalf of the case
- `waiting_customer`: the department is waiting on debtor/customer response or promised action
- `waiting_external`: the department is waiting on another system, department, or legal dependency
- `resolved`: the operational outcome has been achieved and no further work is expected
- `closed`: the case is administratively complete and removed from active queues
- `cancelled`: the case was invalidated or merged into another case

Stages sit inside lifecycle states and are domain-specific. For example, an accounts receivable case may move through `new -> collector_review -> contact_due -> promise_to-pay -> escalation_review -> external_agency`. Stages explain what kind of departmental work is needed now, while lifecycle status explains whether the case is still operationally active.

### Event-to-case relationship
Events do not disappear after case creation. They continue to:
- open cases when no matching responsibility container exists
- enrich the evidence and history of an open case
- change stage, urgency, or due dates
- create new work items for follow-up
- close or reopen cases when business conditions change

The key correction is that execution is usually not triggered straight from the event. The event first changes operational reality, and then operational reality determines if and when execution is warranted.

### Work queues and assignment rules
Queues should feel like a department manager's whiteboard, not a transport retry table. Useful queue rules include:
- **intake queue** for newly opened cases awaiting first touch
- **follow-up queue** for cases whose next promised action date is today
- **exception queue** for disputes, bounced emails, or policy blocks
- **manager review queue** for high-value or high-risk matters
- **specialist queue** for legal, multilingual, or enterprise-account handling

Assignment rules should combine business reality and fairness:
- fixed owner when an account already has a responsible collector
- relationship-based routing to preserve continuity with the customer
- skill-based routing for disputes, legal sensitivity, or language needs
- capacity-aware balancing across employees in the same queue
- manager-directed assignment for escalations or strategic accounts

### Daily planning and manager influence
`DailyPlan` is the mechanism that makes Kalita feel like a staffed department. Planning should account for:
- backlog and SLA deadlines across queues
- fixed-date follow-ups such as promised callbacks or payment dates
- employee capacity budgets and shift constraints
- aging, amount, customer tier, strategic importance, and collection strategy
- carry-over work not completed yesterday

Managers influence planning by:
- pinning specific cases to the top of today's plan
- reserving capacity for campaigns or month-end pushes
- changing queue priority weights
- approving reassignment between employees or teams
- forcing specialist review before any execution step proceeds

A digital employee can auto-draft a plan, but managerial policy and overrides remain first-class operating signals.

### DigitalEmployee upgrade
A `DigitalEmployee` must now behave like a department worker with the following operational properties:
- **work queue membership**: it belongs to one or more queues and only draws work from those operational backlogs
- **planning behavior**: it helps draft and execute its daily plan according to capacity, priority rules, and manager overrides
- **case ownership**: it can become the named owner of a case for a period of responsibility, including follow-up deadlines
- **responsibility continuity**: it must maintain case history, next actions, and accountability across multiple executions rather than acting as a stateless tool caller
- **execution discipline**: it starts executions only from assigned or self-planned case work, except for explicitly allowed emergency triggers

## 6. Mapping from current system

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

## 7. Minimal migration strategy
Do not rewrite the repository. Introduce runtime-first seams in small steps.

### Introduce event model
- Add a first-class event envelope and append-only event persistence abstraction.
- Start by emitting events for current HTTP write paths and action flows.
- Keep existing CRUD behavior while adding event capture as the new source of execution truth.

### Introduce command envelope
- Define an explicit command object for any operation that intends side effects.
- Route existing action endpoints and selected CRUD mutations through command admission before execution.
- Preserve current API shapes by translating transport requests into commands internally.

### Introduce case and queue operations
- Add case-correlation rules so triggering events open or enrich cases before execution begins.
- Persist `Case`, `WorkQueue`, and `WorkItem` state as operational records instead of treating events as self-sufficient work orders.
- Expose queue and case projections for operators and managers.

### Introduce daily planning
- Add `DailyPlan` generation for one department with capacity, due-date, and manager-override support.
- Ensure work is executed from planned or explicitly assigned items rather than raw event arrival.
- Record plan publication and assignment decisions as durable operational facts.

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

## 8. First 7 implementation slices

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

### Slice 3 - Case and queue skeleton
**Goal**
- Create durable `Case`, `WorkQueue`, and `WorkItem` records with event-to-case correlation.

**Scope**
- Open or enrich a case from one selected business event.
- Route the resulting work into a queue with a visible priority and owner policy.

**Why it matters**
- Establishes the operational layer so work is owned as a department matter before execution begins.

**Testability**
- Unit test case correlation, reopening, and queue routing rules.
- Integration test that repeated related events enrich the same case rather than spawning disconnected executions.

### Slice 4 - Daily planning and assignment
**Goal**
- Add a minimal `DailyPlan` generator and assignment mechanism for one queue.

**Scope**
- Produce a dated plan from queue backlog, deadlines, and employee capacity.
- Allow a manager override and publish assignments to a named employee.

**Why it matters**
- Makes Kalita operate like a real staffed department instead of an always-fire execution engine.

**Testability**
- Unit test prioritization and manager override behavior.
- Integration test that only planned or assigned work can start execution.

### Slice 5 - Execution runtime skeleton
**Goal**
- Create durable `ExecutionInstance` and `ExecutionEvent` records with a minimal step runner for planned work.

**Scope**
- Start a runtime execution from an assigned `WorkItem` rather than directly from raw event arrival.
- Record statuses such as `pending`, `running`, `waiting_approval`, `succeeded`, and `failed`.

**Why it matters**
- Preserves the operational layer while still moving orchestration responsibility out of HTTP and into a dedicated runtime core.

**Testability**
- Unit test state transitions and work-item-to-execution linkage.
- Integration test crash-safe resume semantics for a simple non-side-effecting planned execution.

### Slice 6 - Actor context injection
**Goal**
- Ensure all ingress paths produce `ActorContext` and propagate it through runtime records.

**Scope**
- Add actor metadata capture in HTTP and admin flows.
- Store actor context IDs on commands, events, cases, and executions.

**Why it matters**
- Enables policy, audit, assignment, and approval to be actor-aware from the beginning.

**Testability**
- Unit test actor context construction and propagation.
- Integration test that audit/event records include actor identity and origin.

### Slice 7 - Policy hook before tool/action execution
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

## 9. Anti-patterns
Kalita vNext must not do the following:
- **Chat-first architecture**: do not model digital employees as conversational agents waiting for user prompts.
- **Direct LLM tool execution**: LLM output must never invoke tools or mutate systems without runtime control.
- **Business logic in handlers**: transport handlers must not own execution policies, retries, or workflow rules.
- **State in prompts**: prompts may assist proposal generation, but durable execution state must live in runtime records and events.
- **CRUD as core model**: record mutation cannot remain the primary representation of enterprise work.
- **Approval as UI-only behavior**: approvals must be runtime objects and events, not front-end conventions.
- **Opaque automation**: every proposal, decision, action, and outcome must be attributable and auditable.
- **In-memory-only execution**: long-running digital employee work must survive process restarts.

## 10. Accounts Receivable example: debt collection department

### Department definition
An `Accounts Receivable Collection Digital Employee` owns debt collection work for a bounded customer segment. It does not simply react to overdue events by firing actions. It operates like a collector inside a department with queues, daily targets, promises to follow up, and manager oversight.

### Case model in the scenario
A debt collection `Case` represents one actionable collection matter, typically scoped to an account, debtor, or invoice cluster. The case holds:
- overdue balance and aging
- debtor contact history
- promise-to-pay commitments
- dispute indicators
- next promised action date
- assigned collector or queue
- stage such as `new_overdue`, `contact_due`, `promise_monitoring`, `dispute_review`, `escalation_review`, or `resolved`

### Queue model in the scenario
The department operates several queues:
- `new-overdue-intake`: first-touch cases opened by newly overdue invoices
- `follow-up-today`: cases with callback or payment promises due today
- `broken-promise`: high-priority cases where a promised payment was missed
- `dispute-exception`: cases requiring evidence gathering or specialist review
- `manager-escalation`: high-balance or sensitive accounts requiring supervisory direction

### Daily planning in the scenario
At the start of each day, Kalita drafts a `DailyPlan` for each collector or digital employee using:
- cases due for follow-up today
- aging and amount-based priority
- broken promises and SLA breaches
- collector capacity and campaign targets
- manager-pinned strategic accounts

A collection manager can move a large debtor to the top of the plan, reserve time for month-end calls, or redirect disputed cases away from standard collectors to specialists.

### End-to-end flow
1. **`invoice_overdue_detected` event arrives**
   - ERP or billing emits an event that invoice `INV-10482` is now overdue.
   - Kalita correlates it to the debtor account and opens a collection `Case` if no active collection matter exists.

2. **Case is created or refreshed**
   - The case enters lifecycle state `open`, stage `new_overdue`, and queue `new-overdue-intake`.
   - If prior invoices already exist for the same debtor, the event may enrich the existing case instead of creating a new one.

3. **Work item enters queue**
   - Kalita creates a `WorkItem` such as `perform_first_contact` with due date, amount-based priority, and collector skill requirements.
   - No tool executes yet; the department now simply has work to manage.

4. **Daily plan selects the case**
   - During morning planning, the system places the work item into collector Ana's `DailyPlan` because she owns the territory and has capacity.
   - The manager raises its priority because the debtor is strategically important.

5. **Collector/digital employee works the case**
   - Ana's digital employee context opens the case, reviews history, and starts an `ExecutionInstance` for the assigned work item.
   - An LLM may propose the next best contact approach, but only within the case history and policy boundaries.

6. **Controlled execution happens**
   - The runtime may invoke an approved communication tool to send an email or prepare a call script.
   - Policy may require manager approval before sending a legal-warning template or before escalating to an external agency.

7. **Business results feed the case**
   - If the debtor promises payment by Friday, Kalita emits `promise_to_pay_recorded`, updates the case stage to `promise_monitoring`, and creates a follow-up work item for the promised date.
   - If Friday passes without payment, event `payment_promise_broken` re-prioritizes the case into the `broken-promise` queue for tomorrow's plan.
   - If payment arrives, event `payment_received` moves the case to `resolved` and removes remaining work items from active queues.

### Why this example matters
This example demonstrates the corrected architecture because:
- events create and evolve cases instead of directly triggering generic execution
- queues expose real departmental backlog
- daily plans express what gets worked today
- assignment creates named responsibility
- execution is subordinate to case operations, not the architectural center

## 11. Final recommendation
The first thing to implement in the repository right now is **a first-class event and command envelope plus the case-centric operational layer that sits in front of execution**.

Concretely, the immediate repo priority should be:
1. define `Event`, `Case`, `WorkQueue`, `WorkItem`, `DailyPlan`, `ExecutionInstance`, and `ActorContext` as core runtime and operational models
2. emit events from one existing action path and one document/file path, then correlate them into cases rather than direct execution alone
3. create a transport-agnostic operational service that owns case creation, queue routing, assignment, and daily planning
4. let the execution runtime start only from planned or explicitly assigned case work
5. store case history, queue state, execution history, policy decisions, and approvals durably while keeping CRUD as an access adapter

This is the smallest change that establishes Kalita as a true business department runtime rather than a more elaborate execution engine. Once that exists, policy, approvals, digital employees, and tool control can accumulate on the correct operational foundation instead of being bolted onto CRUD handlers later.
