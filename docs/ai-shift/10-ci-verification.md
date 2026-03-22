# First-slice CI verification

## Added CI check
- Added a dedicated GitHub Actions workflow: `.github/workflows/first-slice-verification.yml`.
- The workflow is intentionally narrow: it only verifies the first workflow-action slice test commands and can be run in GitHub Actions through pull requests, pushes to `main`, or `workflow_dispatch`.

## Commands run by CI
- `go test ./internal/schema -run Workflow -count=1`
- `go test ./internal/runtime -run 'TestExecuteWorkflowAction(ReturnsProposalForValidTransition|RejectsDisallowedState|RejectsUnknownAction|RejectsVersionMismatch)$' -count=1`
- `go test ./internal/http -run 'TestActionHandler(ReturnsProposalAndMetaWorkflow|ReturnsConflictOnStaleVersion|RejectsMissingRecordVersion|RejectsIgnoredPayloadFields)$' -count=1 -v`

## How to use this before approving future slices
- Treat this workflow as the minimum CI gate for the first workflow slice.
- Before approving follow-on workflow slices, confirm this GitHub Actions check passes in CI instead of relying only on local timing in timeout-prone environments.
- Keep broader test expansion separate from this check unless a future slice explicitly widens the approval baseline.
