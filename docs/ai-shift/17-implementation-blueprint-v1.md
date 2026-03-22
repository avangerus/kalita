# Kalita implementation blueprint v1

## 1. Target package structure

### `internal/eventcore`
- **Purpose**: define immutable runtime envelopes shared by every ingress path and every execution path: business events, execution events, commands, stream metadata, and append/read interfaces for the event log.
- **Key types**:
  - `Event`
  - `Command`
  - `ExecutionEvent`
  - `EnvelopeMeta`
  - `StreamRef`
- **Key interfaces**:
  - `EventLog`
  - `EventPublisher`
  - `CommandPublisher`
  - `Clock`
  - `IDGenerator`
- **Dependencies allowed**:
  - standard library
  - narrow internal utility packages only if they stay transport-free
- **Dependencies forbidden**:
  - `gin`, HTTP request/response types
  - schema validation logic for CRUD payloads
  - storage mutation logic from current `internal/http`
  - digital employee selection, policy evaluation, or queue planning rules

### `internal/command`
- **Purpose**: validate and admit commands into the new runtime center; convert ingress-specific requests into command envelopes and route them to handlers.
- **Key types**:
  - `Handler`
  - `Registry`
  - `AdmissionResult`
  - `DispatchRequest`
- **Key interfaces**:
  - `CommandHandler`
  - `AdmissionPolicy`
  - `CommandBus`
- **Dependencies allowed**:
  - `internal/eventcore`
  - `internal/caseruntime`
  - `internal/policy`
  - `internal/audit`
- **Dependencies forbidden**:
  - direct use of `gin.Context`
  - direct writes into `runtime.Storage.Data`
  - direct JSON binding and HTTP status selection

### `internal/caseruntime`
- **Purpose**: hold the durable business-operational center: case lifecycle, case state transitions, execution state, and correlation between events, work items, and case-owned actions.
- **Key types**:
  - `Case`
  - `CaseStatus`
  - `ExecutionRef`
  - `CaseSnapshot`
- **Key interfaces**:
  - `CaseRepository`
  - `CaseResolver`
  - `ExecutionCoordinator`
- **Dependencies allowed**:
  - `internal/eventcore`
  - `internal/workplan`
  - `internal/policy`
  - `internal/audit`
- **Dependencies forbidden**:
  - HTTP types
  - direct DSL parsing
  - blob transport specifics

### `internal/workplan`
- **Purpose**: represent queue intake, work assignment, and transport-free coordination so events land in operational structures before tool execution.
- **Key types**:
  - `WorkItem`
  - `WorkQueue`
  - `Assignment`
  - `QueueMetrics`
- **Key interfaces**:
  - `QueueRepository`
  - `CoordinationService`
  - `AssignmentStrategy`
- **Dependencies allowed**:
  - `internal/eventcore`
  - `internal/caseruntime`
  - `internal/policy`
- **Dependencies forbidden**:
  - direct HTTP handler concerns
  - direct schema/meta rendering concerns
  - tool invocation code

### `internal/employee`
- **Purpose**: define digital employees as bounded operational actors with subscriptions, capabilities, and allowed action surfaces.
- **Key types**:
  - `DigitalEmployee`
  - `Capability`
  - `Subscription`
  - `EmployeeSelection`
- **Key interfaces**:
  - `Directory`
  - `Selector`
  - `CapabilityChecker`
- **Dependencies allowed**:
  - `internal/eventcore`
  - `internal/workplan`
  - `internal/policy`
- **Dependencies forbidden**:
  - LLM prompt execution concerns
  - HTTP concerns
  - direct persistence mutation outside repository interfaces

### `internal/policy`
- **Purpose**: evaluate action proposals, approvals, and runtime gates before any side effect or state transition is committed.
- **Key types**:
  - `DecisionProposal`
  - `PolicyDecision`
  - `ApprovalRequest`
  - `RiskLevel`
  - `DecisionReason`
- **Key interfaces**:
  - `Evaluator`
  - `ApprovalRouter`
  - `ApprovalStore`
- **Dependencies allowed**:
  - `internal/eventcore`
  - `internal/caseruntime` read models
  - `internal/employee`
- **Dependencies forbidden**:
  - direct HTTP response formatting
  - direct CRUD storage mutation
  - direct tool implementations

### `internal/audit`
- **Purpose**: centralize execution history, correlation, metrics hooks, and structured audit records that are independent of transport.
- **Key types**:
  - `AuditRecord`
  - `ExecutionTrace`
  - `MetricLabelSet`
- **Key interfaces**:
  - `Recorder`
  - `Tracer`
  - `Metrics`
- **Dependencies allowed**:
  - `internal/eventcore`
- **Dependencies forbidden**:
  - business mutation logic
  - HTTP handlers
  - schema parsing

### `internal/access`
- **Purpose**: host transport and integration adapters that expose runtime capabilities through HTTP/admin/CRUD compatibility paths without making them the architectural center.
- **Key types**:
  - `CommandRequest`
  - `CaseResponse`
  - `LegacyMutationRequest`
- **Key interfaces**:
  - `HTTPAdapter`
  - `AdminAdapter`
  - `CRUDFacade`
- **Dependencies allowed**:
  - `internal/command`
  - `internal/caseruntime`
  - `internal/workplan`
  - existing validation/meta packages where needed for compatibility
- **Dependencies forbidden**:
  - embedding runtime rules directly in handlers
  - direct policy bypasses

### `internal/legacy`
- **Purpose**: isolate incremental compatibility shims that translate old CRUD-first and workflow-action behavior into the new command/event center while preserving current API behavior.
- **Key types**:
  - `WorkflowActionAdapter`
  - `RecordMutationAdapter`
  - `SchemaProjection`
- **Key interfaces**:
  - `LegacyActionBridge`
  - `LegacyMutationBridge`
- **Dependencies allowed**:
  - current `internal/runtime`
  - `internal/command`
  - `internal/eventcore`
  - `internal/access`
- **Dependencies forbidden**:
  - new packages depending back on `internal/http`
  - new packages reading raw `gin.Context`

## 2. Core types for v1

### `Event`
- **Fields**:
  - `ID string`
  - `Type string`
  - `OccurredAt time.Time`
  - `Source string`
  - `CorrelationID string`
  - `CausationID string`
  - `ExecutionID string`
  - `CaseID string`
  - `Actor ActorContext`
  - `Payload map[string]any`
- **Why needed now**: the repo currently has no transport-independent fact model, so event envelopes are required before queues, policy, or digital employees can share a common input.
- **What can wait until later**:
  - schema registry for every event type
  - partitioning/sharding metadata
  - payload version negotiation

### `Command`
- **Fields**:
  - `ID string`
  - `Type string`
  - `RequestedAt time.Time`
  - `CorrelationID string`
  - `CausationID string`
  - `ExecutionID string`
  - `CaseID string`
  - `Actor ActorContext`
  - `TargetRef string`
  - `Payload map[string]any`
  - `IdempotencyKey string`
- **Why needed now**: commands create a mandatory admission boundary between incoming intent and any runtime action, replacing direct handler-side mutations and workflow execution calls.
- **What can wait until later**:
  - command priority classes
  - delayed-not-before scheduling metadata
  - command batching envelopes

### `ActorContext`
- **Fields**:
  - `ActorID string`
  - `ActorType string` (`human`, `service`, `digital_employee`, `system`)
  - `DisplayName string`
  - `Roles []string`
  - `Capabilities []string`
  - `RequestID string`
- **Why needed now**: current handlers have no reusable actor context, which blocks policy, audit, and approval decisions from being attributed consistently.
- **What can wait until later**:
  - tenant scoping
  - auth claims snapshots
  - impersonation chains

### `Case`
- **Fields**:
  - `ID string`
  - `Kind string`
  - `Status string`
  - `Title string`
  - `SubjectRef string`
  - `CorrelationID string`
  - `OpenedAt time.Time`
  - `UpdatedAt time.Time`
  - `OwnerQueueID string`
  - `CurrentPlanID string`
  - `Attributes map[string]any`
- **Why needed now**: cases are the first durable business container between raw events and direct execution; without them the repo remains handler-driven and record-centric.
- **What can wait until later**:
  - SLA policy snapshots
  - multi-subject linking
  - archival and closure analytics

### `WorkItem`
- **Fields**:
  - `ID string`
  - `CaseID string`
  - `QueueID string`
  - `Type string`
  - `Status string`
  - `Priority string`
  - `Reason string`
  - `AssignedEmployeeID string`
  - `PlanID string`
  - `DueAt *time.Time`
  - `CreatedAt time.Time`
  - `UpdatedAt time.Time`
- **Why needed now**: work items create the queue/plan seam required by the target architecture and prevent events from invoking actions immediately.
- **What can wait until later**:
  - SLA timers
  - cost estimation
  - multi-step dependencies between work items

### `WorkQueue`
- **Fields**:
  - `ID string`
  - `Name string`
  - `Department string`
  - `Purpose string`
  - `AllowedCaseKinds []string`
  - `DefaultEmployeeIDs []string`
  - `PolicyRef string`
- **Why needed now**: queues are necessary to represent ownership and intake routing before the system can support coordination, assignment, and multiple execution modes.
- **What can wait until later**:
  - staffing forecasts
  - dynamic backlog thresholds
  - multi-region routing

### `CoordinationDecision`
- **Fields**:
  - `ID string`
  - `WorkItemID string`
  - `CaseID string`
  - `Mode string`
  - `Decision string`
  - `AssigneeRef string`
  - `Priority int`
  - `ReasonCodes []string`
  - `NotBefore *time.Time`
  - `ExpiresAt *time.Time`
  - `DecidedBy ActorContext`
  - `CreatedAt time.Time`
- **Why needed now**: a minimal coordination object lets the runtime distinguish intake from execution readiness without forcing every department into day-based planning.
- **What can wait until later**:
  - richer planning artifacts such as `DailyPlan`
  - capacity optimization models
  - manager note history and scenario simulation

### `DigitalEmployee`
- **Fields**:
  - `ID string`
  - `Code string`
  - `Role string`
  - `Subscriptions []string`
  - `AllowedCommands []string`
  - `AllowedTools []string`
  - `HomeQueueID string`
  - `PolicyProfile string`
  - `Enabled bool`
- **Why needed now**: the target model requires explicit employee definitions rather than hidden handler logic or ad hoc action invocations.
- **What can wait until later**:
  - model/provider settings
  - prompt bundles
  - learning/feedback loops

### `DecisionProposal`
- **Fields**:
  - `ID string`
  - `CaseID string`
  - `WorkItemID string`
  - `ExecutionID string`
  - `EmployeeID string`
  - `Command Command`
  - `Justification string`
  - `RiskHints []string`
  - `CreatedAt time.Time`
- **Why needed now**: policy needs a stable object to evaluate before action; this separates proposal from execution and makes approval durable.
- **What can wait until later**:
  - confidence scoring
  - alternative proposals
  - attached evidence bundles

### `PolicyDecision`
- **Fields**:
  - `ID string`
  - `ProposalID string`
  - `Outcome string` (`allow`, `allow_with_audit`, `require_approval`, `deny`)
  - `RiskLevel string`
  - `Reasons []string`
  - `RequiredApproval bool`
  - `DecidedAt time.Time`
  - `Decider string`
- **Why needed now**: current code has no mandatory policy checkpoint; this is the smallest durable enforcement result that other packages can honor.
- **What can wait until later**:
  - policy rule trace trees
  - weighted risk calculations
  - exception tokens

### `ApprovalRequest`
- **Fields**:
  - `ID string`
  - `ProposalID string`
  - `CaseID string`
  - `QueueID string`
  - `Status string`
  - `RequestedFromRole string`
  - `ExpiresAt *time.Time`
  - `CreatedAt time.Time`
  - `ResolvedAt *time.Time`
  - `ResolutionNote string`
- **Why needed now**: approvals must be durable runtime objects instead of temporary HTTP/UI interactions.
- **What can wait until later**:
  - multi-stage approvals
  - delegation chains
  - approval reminders/escalation automation

### `ExecutionEvent`
- **Fields**:
  - `ID string`
  - `ExecutionID string`
  - `CaseID string`
  - `Step string`
  - `Status string`
  - `OccurredAt time.Time`
  - `CorrelationID string`
  - `CausationID string`
  - `Payload map[string]any`
- **Why needed now**: the repo needs runtime-native audit history for step progression before adding durable tool execution or retries.
- **What can wait until later**:
  - replay checkpoints
  - step-level latency histograms in the record itself
  - large blob attachments to event payloads

## 3. Runtime boundaries

### What belongs in runtime
- Correlating events, commands, cases, work items, plans, and execution steps.
- Creating or updating the minimal state machines for `Case`, `WorkItem`, approvals, and execution progression.
- Enforcing idempotency, correlation, causation, and transition invariants.
- Emitting `ExecutionEvent` records for each state transition.
- Selecting the next eligible action only after a work item is planned and policy says the action may proceed.

### What belongs in policy
- Evaluating `DecisionProposal` objects against actor type, employee capabilities, command type, case attributes, queue context, and side-effect class.
- Deciding allow/deny/require-approval outcomes.
- Routing approval requirements into durable `ApprovalRequest` objects.
- Producing policy reason codes usable by audit, operators, and tests.
- Policy must not own execution progression, queue assignment, or CRUD transport concerns.

### What belongs in access adapters
- HTTP request parsing, auth extraction, response shaping, ETag headers, and compatibility route handling.
- Translating request payloads into `Command` or legacy bridge calls.
- Calling runtime/application services and serializing their outputs.
- Access adapters may keep current CRUD response contracts for compatibility, but they must stop deciding business execution rules.

### What must be removed from HTTP handlers
- Direct writes to `storage.Data` as the primary business orchestration path for workflow-like operations.
- Any decision about whether an action is allowed, planned, assigned, approved, or denied based on handler-local conditionals.
- Runtime state machine logic tied to request lifecycle.
- Construction of workflow approval semantics as transport-only resources.
- Cross-cutting audit and policy behavior implemented ad hoc in individual handlers.

## 4. Mapping from current repo to new packages

| current file/package | keep/refactor/extract/deprecate | target package | notes |
| --- | --- | --- | --- |
| `internal/runtime/storage.go` | refactor | `internal/legacy`, `internal/caseruntime`, `internal/eventcore` | Keep in-memory store initially, but split record store concerns from new event/case/work stores. |
| `internal/runtime/names.go` | keep | `internal/legacy` | Entity normalization is still needed for CRUD compatibility and schema lookup. |
| `internal/runtime/actions.go` | extract | `internal/command`, `internal/policy`, `internal/legacy` | Current workflow action execution becomes a command proposal plus legacy bridge instead of direct action evaluation. |
| `internal/runtime/action_requests.go` | extract | `internal/policy` | Existing action request objects map naturally to first durable approval requests, but need generic naming and command linkage. |
| `internal/runtime/query.go` | keep | `internal/legacy` | Listing/sorting stays as CRUD support, not part of the new core. |
| `internal/http/handlers.go` | refactor | `internal/access`, `internal/legacy` | CRUD handlers should delegate to compatibility services and lose orchestration responsibilities. |
| `internal/http/actions.go` | refactor | `internal/access` | Action endpoint should create a command/proposal and return runtime state rather than call runtime logic directly. |
| `internal/http/action_requests.go` | refactor | `internal/access`, `internal/policy` | Route becomes approval request adapter on top of generic policy objects. |
| `internal/http/admin.go` | keep/refactor | `internal/access` | Reload remains an admin adapter, but later should trigger schema/projection refresh events. |
| `internal/http/router.go` | refactor | `internal/access` | Router should wire adapters to application services, not to storage mutation functions. |
| `internal/app/bootstrap.go` | refactor | `internal/app` | Bootstrap must construct the new event, case, work, policy, and legacy bridges while preserving current startup flow. |
| `internal/validation/*` | keep | `internal/legacy`, `internal/access` | Validation remains critical for CRUD compatibility and can later be reused for command payload validation. |
| `internal/schema/*` | keep | `internal/schema` | Still the static contract source; no v1 package move required. |
| `internal/catalog/*` | keep | `internal/catalog`, `internal/policy` | Enum catalogs can start supplying policy vocabulary without changing current loading. |
| `internal/blob/*` | keep | `internal/blob`, `internal/access` | Blob store remains an adapter/capability boundary. |
| `internal/postgres/*` | keep | `internal/postgres` | Leave as schema projection in v1; do not turn it into the new runtime center yet. |
| `cmd/server/main.go` | keep/refactor | `cmd/server`, `internal/app` | Main stays thin; only wiring changes. |

## 5. Slice plan

### Slice 1: establish event/command center with in-memory log
- **Goal**: add a transport-independent event and command package, plus a small in-memory log and command admission service that existing code can call without changing CRUD semantics.
- **Exact package/files likely affected**:
  - new: `internal/eventcore/*.go`
  - new: `internal/command/*.go`
  - update: `internal/app/bootstrap.go`
  - optional bridge update: `internal/http/actions.go`
- **New tests needed**:
  - event log append/read test
  - command admission sets correlation/causation IDs test
  - bootstrap wiring test for in-memory implementations
- **What becomes possible after this slice**:
  - every new runtime behavior can emit auditable envelopes
  - action endpoints can start using commands without immediate full runtime replacement
  - future case/work/policy slices share one canonical envelope model
- **What must explicitly wait**:
  - case persistence
  - queue planning
  - approvals and policy enforcement beyond pass-through admission

### Slice 2: create minimal case runtime and event-to-case resolver
- **Goal**: add a case aggregate and repository so important events/commands resolve into a `Case` before execution.
- **Exact package/files likely affected**:
  - new: `internal/caseruntime/*.go`
  - update: `internal/app/bootstrap.go`
  - update: `internal/command/*.go`
  - new tests for case open/update/idempotent lookup
- **New tests needed**:
  - case opens on first correlated event
  - same correlation or subject maps back to existing case
  - command admission can attach `CaseID`
- **What becomes possible after this slice**:
  - business work is attached to durable case context
  - audit and policy can evaluate case data
- **What must explicitly wait**:
  - queue assignment
  - daily planning
  - digital employee selection

### Slice 3: add work queues and coordination handoff primitives
- **Goal**: introduce queue intake and work-item gating so case work becomes schedulable rather than immediately executable.
- **Exact package/files likely affected**:
  - new: `internal/workplan/*.go`
  - update: `internal/caseruntime/*.go`
  - update: `internal/app/bootstrap.go`
  - adapter updates in `internal/http/actions.go` or new admin/read endpoints
- **New tests needed**:
  - work item creation from case event
  - assignment to queue by case kind
  - command execution blocked until work item has a coordination outcome
- **What becomes possible after this slice**:
  - backlog and assignment concepts exist in code
  - future digital employees have an operational home
- **What must explicitly wait**:
  - actual employee registry
  - approval routing
  - tool execution runtime

### Slice 4: add coordination decisions and execution eligibility
- **Goal**: formalize `CoordinationDecision` and a minimal coordination service so work can become ready, held, deferred, or assigned without hardcoding day-based planning.
- **Exact package/files likely affected**:
  - new: `internal/workplan/*.go`
  - update: `internal/caseruntime/*.go`
  - update: `internal/app/bootstrap.go`
  - optional adapter updates for read visibility
- **New tests needed**:
  - latest decision lookup by `WorkItem`
  - execution eligibility derived from coordination outcome
  - manager override or assignee update does not require a daily plan model
- **What becomes possible after this slice**:
  - continuous backlog-to-execution release
  - later batch planning as an optional mode
  - future digital-manager and SLA-driven coordination
- **What must explicitly wait**:
  - rich planning artifacts such as `DailyPlan`
  - advanced capacity optimization
  - policy proposal and approval routing

### Slice 5: add policy proposals and approval requests
- **Goal**: formalize `DecisionProposal`, `PolicyDecision`, and `ApprovalRequest` so side-effectful commands stop bypassing policy.
- **Exact package/files likely affected**:
  - new: `internal/policy/*.go`
  - update: `internal/command/*.go`
  - update: `internal/runtime/action_requests.go` or move logic into policy store
  - update: `internal/http/action_requests.go`
- **New tests needed**:
  - policy evaluator returns allow/deny/require-approval
  - approval request is durable and idempotent
  - legacy workflow action path produces a proposal before approval request creation
- **What becomes possible after this slice**:
  - current workflow requests become a generic approval mechanism
  - runtime can block execution until approval resolves
- **What must explicitly wait**:
  - employee-driven execution
  - advanced risk scoring
  - external approver integrations

### Slice 5: add digital employee registry and legacy action bridge
- **Goal**: define digital employees and connect selected planned work to legacy action execution through a compatibility layer.
- **Exact package/files likely affected**:
  - new: `internal/employee/*.go`
  - new: `internal/legacy/*.go`
  - update: `internal/runtime/actions.go`
  - update: `internal/http/actions.go`
  - update: `internal/app/bootstrap.go`
- **New tests needed**:
  - employee capability allows only declared commands
  - employee selection by queue/subscription
  - legacy workflow action bridge emits command/event/audit records around existing action behavior
- **What becomes possible after this slice**:
  - the repo has a real architectural center for case-driven digital employee execution
  - legacy HTTP workflow routes become adapters instead of core logic
- **What must explicitly wait**:
  - durable external event bus
  - production-grade retry scheduler
  - multi-step tool orchestration

## 6. First slice in detail

### Package skeleton
- `internal/eventcore/types.go`
  - defines `Event`, `Command`, `ExecutionEvent`, `ActorContext`, and metadata helpers.
- `internal/eventcore/log.go`
  - defines `EventLog` interface and an in-memory implementation for append/read-by-correlation.
- `internal/eventcore/ids.go`
  - defines minimal `IDGenerator` and `Clock` abstractions with default implementations.
- `internal/command/service.go`
  - defines the admission service used to accept a `Command`, assign IDs/timestamps, and append a command-admitted execution event.
- `internal/command/interfaces.go`
  - defines `AdmissionPolicy`, `CommandBus`, and pass-through default implementations.
- `internal/app/bootstrap.go`
  - wires the in-memory event log and command service into application bootstrap result.

### File names
- `internal/eventcore/types.go`
- `internal/eventcore/log.go`
- `internal/eventcore/log_test.go`
- `internal/eventcore/ids.go`
- `internal/command/interfaces.go`
- `internal/command/service.go`
- `internal/command/service_test.go`
- `internal/app/bootstrap.go` (updated only to expose/wire the new services)

### Interfaces
- `type EventLog interface { AppendEvent(context.Context, Event) error; AppendExecutionEvent(context.Context, ExecutionEvent) error; ListByCorrelation(context.Context, string) ([]Event, []ExecutionEvent, error) }`
- `type AdmissionPolicy interface { Admit(context.Context, Command) error }`
- `type CommandBus interface { Submit(context.Context, Command) (Command, error) }`
- `type IDGenerator interface { NewID() string }`
- `type Clock interface { Now() time.Time }`

### Minimal test strategy
- **Unit tests in `internal/eventcore/log_test.go`**:
  - append two events with same correlation and verify retrieval order is append order.
  - append execution event and verify it is queryable independently from domain events.
- **Unit tests in `internal/command/service_test.go`**:
  - when a command lacks IDs/timestamps, service fills them deterministically using fake clock/ID generator.
  - pass-through policy allows command and causes a command-admitted execution event to be written.
  - if policy rejects, no event is appended.
- **Bootstrap smoke test**:
  - `Bootstrap` returns non-nil event log and command service without changing existing storage setup.

### Migration strategy from existing code
1. Add the new packages with no routing changes first.
2. Extend `internal/app.BootstrapResult` with the event log and command service while leaving existing storage and HTTP behavior untouched.
3. In the same slice or immediately after, update only `internal/http/actions.go` to emit a `Command` admission record before or alongside current `runtime.ExecuteWorkflowAction` so the new center is exercised by one compatibility path.
4. Do not move CRUD handlers yet; they continue using `runtime.Storage` directly until case/work/policy slices exist.
5. Keep all data in memory for v1 slice 1 so the repo gains architectural direction without forcing durable storage decisions too early.

Slice 1 is intentionally small: it introduces the new architectural center as shared envelopes plus append-only admission logging, while preserving the existing server, storage, DSL, and CRUD behavior.

## 7. Anti-patterns during implementation
- Do not move business rules from one HTTP handler to another; move them into runtime/application packages or leave them where they are until a real extraction target exists.
- Do not create package cycles such as `policy -> access -> command -> policy`.
- Do not let new packages depend on `gin.Context`; adapters may use it, core packages may not.
- Do not overload current `runtime.Record` with case, queue, approval, and employee semantics just to avoid new types.
- Do not hide approvals inside boolean flags on commands; use durable typed objects.
- Do not let policy directly mutate records or execute legacy actions.
- Do not treat current CRUD validation as the full policy system; validation remains integrity checking, not authorization or risk control.
- Do not introduce a second ad hoc event format in handlers or tests; all new runtime flows should converge on `internal/eventcore` envelopes.
- Do not prematurely replace in-memory storage with PostgreSQL-centric runtime design in the first slices; first establish boundaries, then persistence.
- Do not wire digital employee behavior through prompt strings or free-form config before capabilities, allowed commands, and queue ownership types exist.
