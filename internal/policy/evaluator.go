package policy

import (
	"context"
	"fmt"

	"kalita/internal/workplan"
)

type DeterministicEvaluator struct{}

func NewEvaluator() *DeterministicEvaluator {
	return &DeterministicEvaluator{}
}

func (e *DeterministicEvaluator) EvaluateCoordinationDecision(_ context.Context, d workplan.CoordinationDecision) (PolicyOutcome, string, error) {
	switch d.DecisionType {
	case workplan.CoordinationExecuteNow:
		return PolicyAllow, fmt.Sprintf("coordination decision %s authorized execution for work item %s", d.ID, d.WorkItemID), nil
	case workplan.CoordinationDefer:
		return PolicyRequireApproval, fmt.Sprintf("coordination decision %s deferred work item %s pending approval or rescheduling", d.ID, d.WorkItemID), nil
	case workplan.CoordinationEscalate, workplan.CoordinationBlock:
		return PolicyDeny, fmt.Sprintf("coordination decision %s blocked automatic execution for work item %s", d.ID, d.WorkItemID), nil
	default:
		return PolicyDeny, fmt.Sprintf("coordination decision %s has unsupported decision type %q", d.ID, d.DecisionType), nil
	}
}
