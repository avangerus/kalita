# Kalita — Architecture

## Current State (M1 — Done)

Working system: Event → Case → Work → Coordination → Policy → Execution pipeline.
Control Plane functional. Demo with multi-case scenarios. Trust system working.
Approvals with idempotent handling. Server-rendered HTML UI.

---

## Core Pipeline

```
Event
→ Case (caseruntime)
→ WorkItem / CoordinationDecision (workplan)
→ ExecutionConstraints (executioncontrol)
→ Actor Selection — capability + profile + trust (employee / capability / profile / trust)
→ Proposal → ActionPlan (proposal / actionplan)
→ ExecutionRuntime — WAL + compensation (executionruntime)
→ Trust Update (trust)
```

---

## Layers

### 1. Event Core (`internal/eventcore/`)
- Core event and command abstractions for the event-driven backbone
- Types: `Event`, `Command`, `ExecutionEvent`, `ActorContext`
- Interfaces: `IDGenerator`, `Clock`
- All state changes originate here; provides correlation/causation tracking

### 2. Case Runtime (`internal/caseruntime/`)
- Case = unit of operational work
- Lifecycle: open → closed
- Types: `Case`, `CaseStatus`
- Interfaces: `CaseRepository`, `CaseResolver`
- Resolves commands to cases by correlation, subject reference, and other criteria

### 3. Work & Coordination Layer (`internal/workplan/`) — CRITICAL
- WorkItem = executable unit within a Case; WorkQueue = ordered backlog
- Coordination decides: `execute_now` / `defer` / `escalate` / `block`
- Types: `WorkItem`, `WorkItemStatus`, `WorkQueue`, `CoordinationDecision`
- Interfaces: `QueueRepository`, `AssignmentRouter`, `Coordinator`, `Planner`
- No probabilistic decisions — deterministic only

### 4. Execution Constraints (`internal/executioncontrol/`)
- Defines risk levels, token limits, step caps, duration limits per execution
- Types: `ExecutionConstraints`, `RiskLevel`, `ExecutionMode`
- Interfaces: `ConstraintsRepository`, `ConstraintsPlanner`, `ConstraintsService`
- `AdjustForTrust()` tightens/loosens constraints based on actor trust score

### 5. Execution Runtime (`internal/executionruntime/`) — SENSITIVE
- ExecutionSession lifecycle with per-step tracking
- WAL (write-ahead log) — append-only, never UPDATE
- Compensation log for rollback
- Types: `ExecutionSession`, `StepExecution`, `WALRecord`, `WALRecordType`
- Interfaces: `ExecutionRepository`, `WAL`, `ActionExecutor`, `Runner`, `Service`
- Do not modify without explicit instruction

### 6. Actor Model (`internal/employee/`)
- Digital employees — NOT LLM agents; hard invariant
- Selected by: capability + profile + trust score
- Types: `DigitalEmployee`, `Assignment`
- Interfaces: `Directory`, `AssignmentRepository`, `Selector`, `Service`

### 7. Capability (`internal/capability/`)
- Skills and tools modelled as capabilities with levels
- Types: `Capability`, `CapabilityType`, `ActorCapability`
- Interfaces: `CapabilityRepository`, `ActorCapabilityRepository`, `Matcher`, `Service`

### 8. Profile (`internal/profile/`)
- Execution style profiles: careful / fast / balanced / strict / exploratory
- Types: `CompetencyProfile`, `CapabilityRequirement`, `ExecutionStyle`
- Interfaces: `Repository`, `RequirementRepository`, `Matcher`, `Service`

### 9. Trust Layer (`internal/trust/`)
- Updated from execution outcomes (success / failure / compensation)
- Affects actor eligibility and autonomy tier
- Types: `TrustProfile`, `TrustLevel`, `AutonomyTier`, `TrustMetrics`, `ExecutionOutcome`
- Interfaces: `Repository`, `Scorer`, `Service`
- Deterministic updates only

### 10. Proposal (`internal/proposal/`)
- Captures action intent from an employee before execution
- Lifecycle: draft → validated → compiled
- Types: `Proposal`, `ProposalType`, `ProposalStatus`
- Interfaces: `Repository`, `Validator`, `CompilerAdapter`, `Service`
- Proposal ≠ Execution — always separated

### 11. Action Plan (`internal/actionplan/`)
- Compiled, validated plan of actions with reversibility and idempotency metadata
- Types: `Action`, `ActionPlan`, `ActionDefinition`, `ReversibilityType`, `IdempotencyType`
- Interfaces: `Registry`, `Compiler`, `Validator`, `Service`

---

## Integration Layer (`internal/integration/`)

Orchestrates end-to-end incident ingestion from external systems:

```
ExternalIncident
→ Case creation (caseruntime)
→ WorkItem intake + coordination (workplan)
→ ExecutionConstraints (executioncontrol)
→ ExecutionSession start (executionruntime)
```

Types: `ExternalIncident`, `IngestResult`
Interfaces: `ProcessedIncidentStore`, `IncidentService`

---

## AIS Otkhody Integration (`internal/integrations/aisotkhody/`)

Adapter for real operational data from AIS Otkhody. Domain runtime is unchanged — this layer sits entirely outside domain packages.

### Data flow

```
AIS API (HTTP)
→ DataFetcher.FetchMissedPickups(ctx, date) → []PickupEvent
→ AisEventMapper.MapPickupEvent(pickup)     → eventcore.Event
→ EventInjector.IngestExternalEvent(ctx, ExternalEvent)
→ integration.IncidentService (existing pipeline)
```

### Idempotency

Each `PickupEvent` carries an `ExternalID`. `IngestExternalEvent` deduplicates by `external_id` — repeated ingests of the same data produce no duplicates.

### Key types and interfaces

| Symbol | Description |
|---|---|
| `DataFetcher` | Interface — `FetchMissedPickups(ctx, date) ([]PickupEvent, error)` |
| `PickupEvent` | Normalized AIS record: `ExternalID`, `RouteID`, `ContainerSite`, `MissedAt` |
| `AisEventMapper` | Pure mapper — converts `PickupEvent` → `eventcore.Event`; no HTTP/DB knowledge |
| `IngestionService` | Interface — `IngestDate`, `IngestNow`, `Start(ctx)` |
| `AisIngestionService` | Implementation; wires fetcher + mapper + injector |
| `IngestBatchResult` | `Fetched`, `Ingested`, `Duplicates`, `Errors` per batch |

### Configuration (env)

| Var | Purpose |
|---|---|
| `AIS_API_URL` | Base URL of AIS HTTP API |
| `AIS_API_KEY` | API key for authentication |

`AIS_SCHEDULE_ENABLED=true` starts background polling; default interval is 15 minutes.

### HTTP trigger

`POST /api/integrations/ais/ingest` — manual ingest of current date. Returns `IngestBatchResult`.

### Test support

`MockDataFetcher` with recorded responses in `testdata/` (`YYYY-MM-DD_missed-pickups.json`). Mapper tests use real AIS data formats.

---

## Schema & Validation (`internal/schema/`, `internal/validation/`)

- `schema/`: entity definitions with typed fields, enum/ref/array constraints, and workflow state machines (`Entity`, `Field`, `Constraints`, `Workflow`)
- `validation/`: validates and coerces objects against schemas; strict type checking and unique constraint enforcement

---

## HTTP Layer (`internal/http/`)

Thin handlers only — Gin framework:

```
parse input → call service → return response
```

No logic. No domain decisions. No direct repo access.
Routes: CRUD, actions, metadata, file uploads, bulk ops, integration, operator endpoints.

> **Status note (reviewed March 24, 2026):** current implementation does not fully match this target.  
> `handlers.go` and `actions.go` still contain orchestration/domain logic, record mutation, and storage access directly in handlers.

---

## Application Bootstrap (`internal/app/`)

Wires all domain services, repositories, and persistence into a ready-to-run `BootstrapResult`.
`Bootstrap(cfgPath string) (*BootstrapResult, error)` is the single entry point for startup.

---

## Demo Layer (`internal/demo/`)

Isolated from domain — domain never imports from demo.
Contains scripted scenarios with fixed IDs and deterministic clocks for reproducible demonstrations.

---

## Storage

### In-Memory (current default)
All repositories are interface-first; in-memory implementations ship by default.

```go
type CaseRepository interface {
    Save(ctx context.Context, c *Case) error
    FindByID(ctx context.Context, id CaseID) (*Case, error)
    FindAll(ctx context.Context) ([]*Case, error)
}
```

### File-Based Persistence (`internal/persistence/`)
- Event sourcing with append-only event log (`FileEventStore`)
- Snapshot store for system state recovery (`FileSnapshotStore`)
- Types: `SystemState`, `Manager`, `Restorer`
- Interfaces: `EventStore`, `SnapshotStore`, `Collector`

### PostgreSQL (`internal/postgres/`)
- Connection management via pgx driver; same repository interfaces as in-memory layer
- `Open(url string) (*sql.DB, error)` — connection pooling + health checks

---

## Invariants (never break)

1. No direct LLM execution in runtime
2. No logic in HTTP handlers
3. Actor ≠ LLM
4. Proposal ≠ Execution — always separated
5. WAL is append-only — no UPDATE in execution log
6. No duplication of runtime decisions
7. Deterministic ordering everywhere
8. Demo layer is isolated — no domain imports from demo/

---

## Architecture Review Findings (March 24, 2026)

### ✅ What is aligned

- Proposal and execution remain separated by dedicated packages (`proposal`, `actionplan`, `executionruntime`).
- Execution runtime still keeps explicit WAL concepts and compensation separation.
- Demo layer remains isolated from domain packages by dependency direction.

### ⚠️ Invariant drift and architectural issues

1. **HTTP handlers are not thin (Rule 1 violation).**  
   `internal/http/handlers.go` and `internal/http/actions.go` perform validation, business orchestration, policy/error branching, direct storage mutation, and execution triggering inside transport handlers.

2. **Control plane is not read-only (Rule 2 + Rule 8 drift).**  
   `internal/controlplane/service.go` resolves approval requests, persists state, appends events, and can trigger re-coordination. This is domain write behavior, not read-model aggregation.

3. **Determinism gap in repository/read paths (Rule 6 drift).**  
   Some code paths iterate maps when returning ordered results or picking the “first” match (`caseruntime.InMemoryCaseRepository.List`, `runtime.Storage.FindIncomingRefs`), producing non-deterministic outcomes across process runs.

4. **Runtime decisions are split across multiple layers (Rule 5 drift risk).**  
   Coordination/policy/execution decisions are partly in domain services and partly inside HTTP action handlers, increasing risk of duplicated or diverging decision logic.

### Recommended refactor direction

1. Extract an `internal/orchestration` (or similar) application service that owns end-to-end action flow:
   `command -> case -> work -> coordination -> policy -> constraints -> plan -> execution`.
2. Reduce handlers to strict adapter shape: decode/validate request envelope, call one service method, map result to HTTP.
3. Split control plane into:
   - **read service** (queries only), and
   - **approval command service** (state changes/events), outside `controlplane`.
4. Make deterministic ordering explicit in all repository/query methods that return slices or first-match semantics (sort keys or use insertion-order indexes consistently).

---

## Sprint History

### Sprint 1 / M1 — Operational Demo (Done)
- Full pipeline implemented
- Control plane functional
- Multi-case demo scenarios
- Trust system
- Approvals with idempotent handling
- Server-rendered HTML UI

### Sprint 2 — Coordination 2.0 (Done)
- `WorkQueueSnapshot` interface + in-memory implementation
- Queue-aware scoring via `QueuePressureScorer`
- Department-level load coordination (`DepartmentLoadProvider`)
- Control plane summary extended with `queue_pressure` per department

### Sprint 3 — External Persistence (Done)
- Repository audit documented in `doc/repositories.md`
- PostgreSQL connection pool (`internal/storage/postgres/`)
- `PostgresCaseRepository`, `PostgresWorkItemRepository`, `PostgresExecutionSessionRepository`
- WAL and compensation log backed by Postgres; append-only invariant enforced at DB level
- Bootstrap wiring: `DATABASE_URL` selects Postgres repos; in-memory remains default
- `/health` endpoint with DB connectivity check

### Sprint 4 — AIS Otkhody Integration (Done)
- `internal/integrations/aisotkhody/` adapter: `DataFetcher`, `AisEventMapper`, `AisIngestionService`
- `PickupEvent` → `eventcore.Event` mapping with `source = ais_otkhody`
- Idempotent ingestion via `external_id` deduplication
- `POST /api/integrations/ais/ingest` for manual trigger
- Optional background scheduler (15 min default, `AIS_SCHEDULE_ENABLED`)
- Mock fetcher + recorded testdata for deterministic tests
