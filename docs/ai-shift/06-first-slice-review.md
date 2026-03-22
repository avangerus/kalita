# First workflow-action slice review

## Verdict

The slice is **not acceptable as-is**.

The implementation is small and mostly additive, and the parser/lint/meta work is directionally sound. However, there are a few concrete risks inside this slice that should be addressed **before the next slice**:

1. the new action path performs an immediate committed state change even though the slice brief describes proposal-only transition requests;
2. the new mutating endpoint allows blind writes when `record_version` is omitted, which weakens the optimistic concurrency model used elsewhere;
3. the action request contract advertises request payload data that is currently ignored entirely.

## Prioritized issues

### Correctness risks

#### 1. Action execution is mutating, not proposal-only
- **Severity:** high
- **Affected files:** `internal/runtime/actions.go`, `internal/http/actions.go`, `docs/ai-shift/04-first-slice.md`, `docs/ai-shift/05-change-report.md`
- **Why it matters:** the runtime updates the record status, increments the version, and marks the result as committed immediately. That behavior contradicts the documented first-slice intent of proposal-only transition requests. This is not just a documentation mismatch; it changes the functional safety boundary of the feature.
- **Fix timing:** **must fix now**

#### 2. Workflow transitions bypass record-level validation for non-inline-enum status fields
- **Severity:** medium
- **Affected files:** `internal/schema/lint.go`, `internal/runtime/actions.go`, `dsl/core/entities.dsl`
- **Why it matters:** lint only verifies `from`/`to` states when the status field is an inline `enum[...]`. If a workflow is attached to another status representation (for example a plain string or catalog-backed string), the runtime will still write the target state directly without any schema-level validation of allowed values. That makes transition correctness depend on convention rather than enforcement.
- **Fix timing:** **must fix now** if non-inline-enum status fields are intended in this slice; otherwise **fix later** but document the restriction explicitly

#### 3. The HTTP action request advertises `payload`, but the implementation silently ignores it
- **Severity:** medium
- **Affected files:** `internal/http/actions.go`, `internal/http/actions_test.go`
- **Why it matters:** clients can send structured justification/comment data, but the handler discards it completely and the runtime never sees it. Silent acceptance is riskier than explicit rejection because callers may believe comment/audit context was applied when it was not.
- **Fix timing:** **must fix now** if clients are expected to send payload data in this slice; otherwise **fix later** by removing or explicitly rejecting unsupported fields

### Backward-compatibility risks

#### 4. The example workflow entity is shipped in the main DSL set, so it changes loaded schemas and meta output for all environments
- **Severity:** low
- **Affected files:** `dsl/core/entities.dsl`, `internal/schema/workflow_test.go`
- **Why it matters:** adding `test.WorkflowTask` to the checked-in DSL changes the result of `LoadAllEntities`, `/api/meta`, and any downstream tooling that enumerates entities from the repository DSL. That is additive, but it is still a behavior change outside isolated test fixtures.
- **Fix timing:** **fix later** unless production environments load `dsl/core/entities.dsl` directly and treat the entity list as stable

### Concurrency/versioning risks

#### 5. The action endpoint allows blind state changes when `record_version` is omitted
- **Severity:** high
- **Affected files:** `internal/http/actions.go`, `internal/runtime/actions.go`, `internal/http/actions_test.go`
- **Why it matters:** `ActionHandler` only enables optimistic concurrency if `record_version > 0`. Without it, the transition executes unconditionally. Existing update paths already support version-aware writes, so the new endpoint introduces an easier lost-update path exactly where state transitions are most sensitive.
- **Fix timing:** **must fix now**

#### 6. The action endpoint uses a different concurrency contract than existing update endpoints
- **Severity:** medium
- **Affected files:** `internal/http/actions.go`, `internal/http/handlers.go`
- **Why it matters:** `PATCH`/`PUT` already support `If-Match` and body version parsing, but the new action route only looks at `record_version` in JSON. That inconsistency increases client error rates and makes version handling harder to apply uniformly across write APIs.
- **Fix timing:** **fix later**, unless the endpoint is going to be exposed immediately to existing API clients

### API design risks

#### 7. The request body duplicates `action`, but only the path parameter is authoritative
- **Severity:** low
- **Affected files:** `internal/http/actions.go`, `internal/http/actions_test.go`
- **Why it matters:** the test request sends both a path action and a body action, but the body field is ignored. This creates an ambiguous contract and leaves room for mismatched requests being accepted without feedback.
- **Fix timing:** **fix later**

#### 8. Meta workflow exposure uses unsorted action maps, so output ordering is unstable
- **Severity:** low
- **Affected files:** `internal/http/meta.go`
- **Why it matters:** action metadata is emitted from Go maps. The API shape is still valid, but clients that snapshot or diff meta responses will get nondeterministic ordering noise.
- **Fix timing:** **fix later**

### Security/authorization risks

#### 9. The new endpoint introduces a direct mutating path with no action-level authorization check
- **Severity:** medium
- **Affected files:** `internal/http/actions.go`, `internal/runtime/actions.go`, `internal/http/router.go`
- **Why it matters:** this route changes workflow state immediately once the transition is structurally valid. Even if the platform is currently light on auth overall, the new endpoint increases the write surface and would be the natural place where approval/action permissions need to attach later.
- **Fix timing:** **fix later** for this minimal slice, but it should block any wider rollout of workflow actions

### Test gaps

#### 10. No HTTP test covers the unsafe no-version path
- **Severity:** high
- **Affected files:** `internal/http/actions_test.go`
- **Why it matters:** current tests cover the stale-version conflict path, but not the case where the request omits `record_version` and succeeds. That is the most important concurrency regression in the slice and it is currently unpinned by tests.
- **Fix timing:** **must fix now**

#### 11. No test covers workflow entities whose status field is not an inline enum
- **Severity:** medium
- **Affected files:** `internal/schema/workflow_test.go`, `internal/runtime/actions_test.go`
- **Why it matters:** the implementation currently relies on inline-enum linting for state validation. There is no test that documents whether other status-field shapes are supported or intentionally unsupported.
- **Fix timing:** **fix later**, unless those entities are expected immediately

#### 12. HTTP tests appear to hang in the current environment, so this slice does not have a clean executable proof at the transport layer
- **Severity:** medium
- **Affected files:** `internal/http/actions_test.go`
- **Why it matters:** schema and runtime tests pass, but `go test ./internal/http` did not complete in this review environment. That leaves the new endpoint coverage weaker in practice than it appears on paper.
- **Fix timing:** **must fix now** if the hang reproduces for the team; otherwise investigate before relying on these tests as release evidence

## Concise summary

The slice is close, but it currently behaves more like an immediate workflow-state mutation endpoint than the proposal-only action request described in the slice brief. The biggest implementation risks are the committed write behavior, the optional version check on a new mutating route, and the misleading request contract around ignored payload data.

## Top 3 risks

1. Immediate committed transition instead of proposal-only behavior.
2. Blind state changes when `record_version` is omitted.
3. Silent acceptance of request payload fields that are ignored.

## Recommendation

**Patch before next slice.**
