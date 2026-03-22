# Test timeout diagnosis: first workflow slice

## Scope checked
- `internal/runtime`
- `internal/http`
- Workflow-related tests introduced by the first slice commit (`deaee03`, "Add workflow action transition slice") and the follow-up patch now present in the working tree.

## Exact tests involved
### `internal/runtime/actions_test.go`
- `TestExecuteWorkflowActionReturnsProposalForValidTransition`
- `TestExecuteWorkflowActionRejectsDisallowedState`
- `TestExecuteWorkflowActionRejectsUnknownAction`
- `TestExecuteWorkflowActionRejectsVersionMismatch`

### `internal/http/actions_test.go`
- `TestActionHandlerReturnsProposalAndMetaWorkflow`
- `TestActionHandlerReturnsConflictOnStaleVersion`
- `TestActionHandlerRejectsMissingRecordVersion`
- `TestActionHandlerRejectsIgnoredPayloadFields`
- `TestExistingPatchCompatibilityStillWorks`

## What is actually hanging
No individual workflow test body reproduced as a logical hang.

What did reproduce in this environment was a **cold-cache `go test` invocation for `./internal/http` with default settings**. That command remained busy for a long time before any test output appeared, while a sibling run with `-vet=off` completed normally and the actual tests all passed in milliseconds.

## Strongest root-cause hypothesis
The timeout is most likely **environment/tooling related, not a workflow test deadlock**:

1. `internal/runtime` workflow tests complete immediately.
2. `internal/http` workflow tests also complete immediately once the package is already built, or when `go test` is run with `-vet=off`.
3. On a cold cache, `go test ./internal/http ...` spends a long time in the build/vet phase because `internal/http` pulls in Gin and its transitive dependency graph.
4. During the stalled cold-cache run, the visible long-lived process was the top-level `go test` process, and a `vet` subprocess appeared transiently, which is consistent with the slowdown occurring before test execution rather than inside a test handler or request path.

## Classification
- **Primary problem location:** test environment / Go toolchain invocation
- **Not supported by evidence:** a hang in workflow test code, server lifecycle handling, leaked `httptest` server, unclosed body, deadlock in `internal/runtime`, or blocking network activity in the workflow tests themselves

## Why the tests themselves do not look like the culprit
- The HTTP tests use `gin.New()` + `httptest.NewRequest()`/`httptest.NewRecorder()` only; they do not start a real listener or background server.
- The runtime tests use in-memory storage only.
- No workflow-focused test opens files, sockets, or goroutines that would need explicit cleanup.
- The follow-up patch changed semantics from mutating/committing transitions to proposal-only responses, but that does not introduce a blocking path; the tests exercise direct function calls and in-process router calls only.

## Exact next fix recommendation
1. **Do not change production code for this issue.**
2. Treat the timeout as an **environmental cold-build / vet-cost problem** unless a future run captures a goroutine dump showing otherwise.
3. For CI or local diagnosis in this constrained environment, run the narrow HTTP workflow tests with one of these mitigations:
   - warm the Go build cache before the focused run, or
   - use `go test -vet=off` for this focused diagnostic slice if vet is not the subject of the check.
4. If CI must keep vet enabled, move vet to a separate step and keep the focused workflow test step limited to executing the tests.
5. Only pursue a code change if a future timeout includes stack traces proving a specific handler/test is blocked after the test binary has started running.

## Verdict
- **Hanging test/setup:** none proven inside the workflow tests
- **Best current explanation:** cold-cache build/vet latency for `internal/http`
- **Problem type:** environment / tooling cost, not workflow runtime correctness
