# Kalita — Architecture

## Current State (M1 — Done)

Working system: Event → Case → Work → Coordination → Policy → Execution pipeline.
Control Plane functional. Demo with multi-case scenarios. Trust system working.
Approvals with idempotent handling. Server-rendered HTML UI.

---

## Core Pipeline

```
Event
→ Case
→ WorkItem
→ CoordinationDecision (execute_now / defer / escalate / block)
→ PolicyDecision / ApprovalRequest
→ Actor Selection (capability + profile + trust)
→ ActionPlan
→ ExecutionRuntime (WAL + compensation)
→ Trust Update
```

---

## Layers

### 1. Event Core (`internal/domain/events/`)
- Append-only event log
- System backbone — all state changes start here

### 2. Case Runtime (`internal/domain/cases/`)
- Case = unit of operational work
- Lifecycle: created → active → resolved / blocked

### 3. Work Layer (`internal/domain/work/`)
- WorkItem = executable unit within a Case
- WorkQueue = ordered backlog

### 4. Coordination Layer (`internal/coordination/`) — CRITICAL
- Decides: execute_now / defer / escalate / block
- Must be queue-aware (M2)
- No probabilistic decisions — deterministic only

### 5. Policy Layer (`internal/policy/`)
- allow / require_approval / deny
- Creates ApprovalRequest when needed
- Approval is idempotent

### 6. Execution Runtime (`internal/execution/`) — SENSITIVE
- ExecutionSession lifecycle
- WAL (write-ahead log) — append-only, never UPDATE
- Compensation log for rollback
- Do not modify without explicit instruction

### 7. Actor Model (`internal/domain/actor/`)
- Digital employees — NOT LLM agents
- Selected by: capability + profile + trust score
- Actor ≠ LLM — hard invariant

### 8. Trust Layer (`internal/domain/trust/`)
- Updated from execution outcomes (success/failure/compensation)
- Affects actor eligibility and autonomy level
- Deterministic updates only

---

## Control Plane (`internal/controlplane/`)

Read-only aggregations for operator view:
- Case list + detail
- WorkItem status
- Approval inbox
- Decision timeline (audit trace)
- System summary

**Rules:**
- No business logic here — aggregations only
- No writes to domain state
- Read models separated from domain

---

## HTTP Layer (`internal/http/`)

Thin handlers only:
```
parse input → call service → return response
```
No logic. No domain decisions. No direct repo access.

---

## Demo Layer (`internal/demo/`)

Isolated from domain — domain never imports from demo.
Contains: deterministic scenarios, seeded data, domain mapping for AIS Otkhody.

---

## Storage

Current: in-memory repositories behind interfaces.
Future (M3): PostgreSQL via pgx/v5, same interfaces.

All repositories are interface-first:
```go
type CaseRepository interface {
    Save(ctx context.Context, c *Case) error
    FindByID(ctx context.Context, id CaseID) (*Case, error)
    FindAll(ctx context.Context) ([]*Case, error)
    FindByStatus(ctx context.Context, status CaseStatus) ([]*Case, error)
}
```

---

## Invariants (never break)

1. No direct LLM execution in runtime
2. No logic in HTTP handlers
3. No logic in controlplane — read models only
4. Actor ≠ LLM
5. Proposal ≠ Execution — always separated
6. WAL is append-only — no UPDATE in execution log
7. No duplication of runtime decisions
8. Deterministic ordering everywhere
9. Demo layer is isolated — no domain imports from demo/

---

## Sprint History

### Sprint 1 / M1 — Operational Demo (Done)
- Full pipeline implemented
- Control plane functional
- Multi-case demo scenarios
- Trust system
- Approvals with idempotent handling
- Server-rendered HTML UI
