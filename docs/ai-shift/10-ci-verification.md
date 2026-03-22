# First-slice CI verification

## Local verification command
- Run `./scripts/verify-first-slice.sh` before merge to verify the first workflow-action slice in a normal local environment.
- The script prints each verification step, stops on the first failure, and returns a non-zero exit code when any command fails.

## Commands run by the local script
- `go test ./internal/schema -run Workflow -count=1`
- `go test ./internal/runtime -run 'TestExecuteWorkflowAction(ReturnsProposalForValidTransition|RejectsDisallowedState|RejectsUnknownAction|RejectsVersionMismatch)$' -count=1`
- `go test ./internal/http -run 'TestActionHandler(ReturnsProposalAndMetaWorkflow|ReturnsConflictOnStaleVersion|RejectsMissingRecordVersion|RejectsIgnoredPayloadFields)$' -count=1 -v`

## How to use this before approving future slices
- Treat `./scripts/verify-first-slice.sh` as the minimum local gate for the first workflow slice.
- Run the script before merge instead of depending on a dedicated GitHub Actions workflow for this narrow verification slice.
- Keep broader test expansion separate from this check unless a future slice explicitly widens the approval baseline.
