# First-slice test guidance

Keep first-slice verification narrow.

## Relevant tests
- `internal/schema/workflow_test.go`: workflow DSL parsing and lint coverage.
- `internal/runtime/actions_test.go`: proposal-only workflow execution and version checks.
- `internal/http/actions_test.go`: action-route request/response checks, including required `record_version`, stale-version rejection, ignored-payload rejection, and existing PATCH compatibility.

## Recommended focused commands
- `go test ./internal/schema -run Workflow -count=1`
- `go test ./internal/runtime -run 'TestExecuteWorkflowAction(ReturnsProposalForValidTransition|RejectsDisallowedState|RejectsUnknownAction|RejectsVersionMismatch)$' -count=1`
- `go test ./internal/http -run 'TestActionHandler(ReturnsProposalAndMetaWorkflow|ReturnsConflictOnStaleVersion|RejectsMissingRecordVersion|RejectsIgnoredPayloadFields)$' -count=1 -v`

## Timeout notes for this environment
- Cold-cache runs are materially slower here because the first `go test` pays most of the compile cost.
- Warm-cache runs are much more representative for the schema and runtime packages; after cache warm-up, those focused commands complete quickly.
- Short fixed assumptions such as "this package should finish within 10-20s" are unreliable here, especially for the first run and especially for `./internal/http`.
- Do not treat a cold-cache timeout in this environment as proof that the slice logic is broken.

## Approval baseline for future slices
Before approving another workflow slice, run the focused schema/runtime commands above locally, and run the focused HTTP action tests plus the normal CI suite in CI or a normal developer environment where compile caches and package timing are more stable.
