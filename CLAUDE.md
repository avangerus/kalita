# CLAUDE.md — Kalita Development Rules

## Stack

- **Language:** Go 1.22+
- **HTTP:** Gin
- **Architecture:** modular monolith, event-driven core
- **Storage:** interface-based repositories (in-memory default, DB-ready)
- **Test framework:** standard `testing` package + testify

## Project Structure

```
kalita/
├── cmd/                    # entrypoints (server, cli)
├── internal/
│   ├── app/                # bootstrap and wiring only
│   ├── http/               # thin handlers — NO business logic here
│   ├── controlplane/       # read models and aggregations ONLY
│   ├── domain/
│   │   ├── cases/          # Case runtime
│   │   ├── work/           # WorkItem, WorkQueue
│   │   ├── coordination/   # CoordinationDecision logic
│   │   ├── policy/         # allow/require_approval/deny
│   │   ├── execution/      # ExecutionRuntime, WAL, compensation
│   │   ├── actor/          # Actor selection, capability, profile
│   │   └── trust/          # TrustLayer, metrics
│   ├── demo/               # demo scenarios — isolated from domain
│   └── workplan/           # ActionPlan, ActionRegistry
├── plan/                   # pipeline artifacts (not compiled)
└── docs/                   # architecture docs
```

## Absolute Rules (never break these)

1. **No logic in HTTP handlers.** Handlers only: parse input → call service → return response.
2. **No logic in controlplane.** Controlplane only aggregates read models. No decisions.
3. **Actor ≠ LLM.** Never wire LLM directly to execution path.
4. **Proposal ≠ Execution.** LLM may propose, runtime decides and executes.
5. **No duplication of runtime decisions.** Each decision lives in exactly one layer.
6. **Deterministic ordering everywhere.** No random, no time-based ordering in domain logic.
7. **No DAG/workflow engine patterns.** State machines, not DAGs.
8. **Read models stay separated from domain.** controlplane/ reads, never writes domain state.

## Code Conventions

```go
// Handler pattern — always this shape
func (h *Handler) HandleSomething(c *gin.Context) {
    var req SomeRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }
    result, err := h.service.DoSomething(c.Request.Context(), req)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }
    c.JSON(200, result)
}

// Repository pattern — always interface first
type CaseRepository interface {
    Save(ctx context.Context, c *Case) error
    FindByID(ctx context.Context, id CaseID) (*Case, error)
    FindAll(ctx context.Context) ([]*Case, error)
}

// Domain errors — typed, not strings
type CoordinationError struct {
    Code    string
    Reason  string
    Context map[string]any
}
```

## Naming Conventions

- Types: `PascalCase` — `CoordinationDecision`, `ExecutionSession`
- Interfaces: noun, no `I` prefix — `CaseRepository`, `ActorSelector`
- Constructors: `New` prefix — `NewCoordinationService`
- Errors: `Err` prefix — `ErrCaseNotFound`, `ErrPolicyDenied`
- Tests: `TestServiceName_MethodName_Scenario`

## Testing Requirements

- Every new service method needs at least one test
- Use table-driven tests for coordination and policy logic
- Test file lives next to source: `coordination_test.go`
- Mock repositories in tests — never touch demo/ from tests
- Run: `go test ./...` must pass before commit

## What NOT to touch without explicit instruction

- `internal/demo/` — isolated demo layer, do not import from domain
- Core pipeline interfaces in `internal/domain/` — only extend, never remove
- WAL and compensation logic in `internal/execution/` — extremely sensitive
- Trust update logic — breaks determinism if modified incorrectly

## Commit Convention

```
feat(coordination): add queue-aware decision scoring
fix(execution): correct WAL entry ordering on compensation
refactor(actor): extract capability matching to separate func
test(policy): add table tests for approval threshold logic
```

## When in doubt

- Prefer explicit over implicit
- Prefer boring over clever
- If a change touches more than 3 packages — stop and ask
- If unsure about invariant — read ARCHITECTURE.md first
