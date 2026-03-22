#!/usr/bin/env bash
set -euo pipefail

run_step() {
  local description="$1"
  shift

  echo
  echo "==> ${description}"
  echo "+ $*"
  "$@"
}

run_step \
  "Running schema workflow tests" \
  go test ./internal/schema -run Workflow -count=1

run_step \
  "Running runtime workflow action tests" \
  go test ./internal/runtime -run 'TestExecuteWorkflowAction(ReturnsProposalForValidTransition|RejectsDisallowedState|RejectsUnknownAction|RejectsVersionMismatch)$' -count=1

run_step \
  "Running HTTP workflow handler tests" \
  go test ./internal/http -run 'TestActionHandler(ReturnsProposalAndMetaWorkflow|ReturnsConflictOnStaleVersion|RejectsMissingRecordVersion|RejectsIgnoredPayloadFields)$' -count=1 -v

echo

echo "First workflow slice verification passed."
