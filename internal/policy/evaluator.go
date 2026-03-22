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
	switch d.Strategy {
	case "requires_manager_approval":
		return PolicyRequireApproval, fmt.Sprintf("strategy %s requires manager approval before execution", d.Strategy), nil
	case "blocked_strategy":
		return PolicyDeny, fmt.Sprintf("strategy %s is blocked from execution", d.Strategy), nil
	}
	if d.Outcome == workplan.CoordinationSelected {
		return PolicyAllow, fmt.Sprintf("coordination decision %s selected work item %s for execution", d.ID, d.WorkItemID), nil
	}
	return PolicyAllow, fmt.Sprintf("coordination strategy %s allowed by default for work item %s", d.Strategy, d.WorkItemID), nil
}
