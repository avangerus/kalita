# Second slice implementation review

## Concise summary
The second slice mostly matches the narrowed plan from the prior review: it keeps the request artifact in-memory, reuses the existing workflow proposal validator, leaves the proposal endpoint unchanged, and limits the new API surface to create + get-by-id. The implementation is directionally correct and additive.

The main remaining concern is not basic happy-path behavior; it is what happens at the edges that the next slice will build on. In particular, request creation validates and inserts in separate steps, so duplicate pending requests can be created concurrently for the same record/version/action. The new global get-by-id route also hard-codes an unscoped request resource shape before any auth or tenant boundary exists. Finally, the tests are good for the happy path but still do not cover several failure paths the slice description claims to preserve.

## Verdict
**patch before next slice**

## Findings

### Correctness risks

#### 1. Duplicate pending requests can be created from the same record version under concurrency
- **severity:** medium
- **affected files:** `internal/runtime/action_requests.go`, `internal/runtime/actions.go`, `internal/runtime/actions_test.go`
- **why it matters:** `CreateWorkflowActionRequest` first calls `ExecuteWorkflowAction` and only later acquires the storage write lock to insert the request. That means two concurrent callers can both validate against the same unchanged record version and both store separate `pending` requests for the same entity/id/action/version combination. This slice does not execute requests yet, but the next slice will inherit ambiguous control-plane state unless the request-creation boundary is made atomic or intentionally deduplicated.
- **fix now or later:** fix before next slice

### Backward-compatibility risks

#### 1. No immediate backward-compatibility break found inside the scoped slice
- **severity:** low
- **affected files:** `internal/http/action_requests.go`, `internal/http/router.go`, `docs/ai-shift/13-second-slice-change-report.md`
- **why it matters:** The existing proposal route remains proposal-only, the new route is additive, and the implementation does not mutate target records on request creation. That is the right compatibility posture. The remaining risk is mainly future API commitment rather than a present break.
- **fix now or later:** later

### API/design risks

#### 1. The new read API commits to a global unscoped request identifier before access scoping exists
- **severity:** medium
- **affected files:** `internal/http/router.go`, `internal/http/action_requests.go`, `docs/ai-shift/13-second-slice-change-report.md`
- **why it matters:** `GET /api/_action_requests/:request_id` makes request lookup independent of module/entity/record context. That is acceptable for a narrow internal slice, but it also fixes a top-level resource shape that will be harder to evolve once authorization, multi-tenant separation, or record-scoped visibility rules arrive. The change report mentions future scoping/auth concerns, so this is already a known architectural seam rather than a theoretical one.
- **fix now or later:** later, unless the next slice introduces auth/scoping work

### Misleading durability risks

#### 1. The documentation still uses durability-adjacent language more strongly than the implementation guarantees
- **severity:** low
- **affected files:** `docs/ai-shift/13-second-slice-change-report.md`, `docs/ai-shift/11-second-slice.md`, `internal/runtime/storage.go`
- **why it matters:** The implementation is plainly runtime-memory only, and `13-second-slice-change-report.md` does note restart loss. Even so, phrases such as “persisted action request” and “server-tracked request artifact” can still read stronger than the actual guarantee unless every mention is paired with “within the running instance only.” That wording gap can mislead downstream readers into treating the slice as more durable than it is.
- **fix now or later:** later; a documentation tightening is enough

### Test gaps

#### 1. The HTTP tests do not cover several failure paths the slice claims to preserve
- **severity:** medium
- **affected files:** `internal/http/actions_test.go`, `docs/ai-shift/13-second-slice-change-report.md`
- **why it matters:** The change report says validation reuse is covered, but the HTTP tests only assert the stale-version branch for the new create route. They do not cover unknown action, disallowed source state, or get-by-id not found for the new endpoint. Because the slice’s value proposition is “same validation rules, new artifact,” those missing cases leave the compatibility claim under-tested.
- **fix now or later:** fix before next slice

#### 2. There is no regression test for concurrent duplicate request creation
- **severity:** medium
- **affected files:** `internal/runtime/actions_test.go`, `internal/http/actions_test.go`, `internal/runtime/action_requests.go`
- **why it matters:** The implementation currently allows concurrent duplicate pending requests, and there is no test that documents whether that is intentional or accidental. Before adding execution or approval semantics, the repository should either lock down the intended behavior or prevent duplicates explicitly.
- **fix now or later:** fix before next slice

## Top 3 risks
1. Concurrent request creation can produce multiple pending requests for the same validated intent.
2. The global get-by-id route commits to an unscoped request resource shape ahead of auth/visibility design.
3. The test suite does not yet prove the new endpoint preserves all key validation/error behaviors it claims to reuse.
