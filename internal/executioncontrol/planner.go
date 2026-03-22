package executioncontrol

import (
	"context"
	"fmt"
	"strings"

	"kalita/internal/policy"
	"kalita/internal/workplan"
)

const UIScopeMarker = "ui.operator"

type DeterministicPlanner struct{}

func NewPlanner() *DeterministicPlanner {
	return &DeterministicPlanner{}
}

func (p *DeterministicPlanner) PlanForPolicyDecision(_ context.Context, coordination workplan.CoordinationDecision, policyDecision policy.PolicyDecision) (ExecutionConstraints, error) {
	if policyDecision.Outcome == policy.PolicyDeny {
		return ExecutionConstraints{}, fmt.Errorf("execution constraints cannot be created for denied policy decision %s", policyDecision.ID)
	}

	constraints := ExecutionConstraints{
		CoordinationDecisionID: coordination.ID,
		PolicyDecisionID:       policyDecision.ID,
		CaseID:                 coordination.CaseID,
		WorkItemID:             coordination.WorkItemID,
		QueueID:                coordination.QueueID,
		RiskLevel:              RiskMedium,
		ExecutionMode:          ExecutionModeDeterministicAPI,
		MaxTokens:              0,
		MaxCost:                0,
		MaxSteps:               10,
		MaxDurationSec:         300,
		AllowedScopes:          []string{"default"},
		ForbiddenOps:           []string{},
		Reason:                 fmt.Sprintf("baseline execution constraints selected for strategy %s with policy outcome %s", coordination.Strategy, policyDecision.Outcome),
	}

	switch coordination.Strategy {
	case "ui_operator_mode":
		constraints.ExecutionMode = ExecutionModeGuidedUIOperator
		constraints.RiskLevel = RiskHigh
		constraints.MaxSteps = 20
		constraints.MaxDurationSec = 900
		constraints.AllowedScopes = appendUnique(constraints.AllowedScopes, UIScopeMarker)
		constraints.Reason = "ui operator strategy requires guided high-risk execution constraints with deterministic UI scope"
	case "checkpointed_mode":
		constraints.ExecutionMode = ExecutionModeCheckpointedAutonomy
		constraints.RiskLevel = RiskHigh
		constraints.MaxSteps = 5
		constraints.MaxDurationSec = 300
		constraints.Reason = "checkpointed strategy requires bounded high-risk autonomy with explicit checkpoints"
	case "requires_manager_approval":
		constraints.RiskLevel = RiskHigh
		constraints.ExecutionMode = ExecutionModeApprovalEachStep
		constraints.MaxSteps = 1
		constraints.MaxDurationSec = 60
		constraints.Reason = "restrictive execution constraints selected because manager approval is required before any execution step"
	case workplan.DefaultCoordinationStrategy:
		if policyDecision.Outcome == policy.PolicyAllow {
			constraints.Reason = "baseline execution constraints selected for default queue selection after policy allow"
		}
	}

	if policyDecision.Outcome == policy.PolicyRequireApproval {
		constraints.RiskLevel = RiskHigh
		constraints.ExecutionMode = ExecutionModeApprovalEachStep
		constraints.MaxSteps = 1
		constraints.MaxDurationSec = 60
		constraints.Reason = restrictiveReason(coordination.Strategy)
	}

	constraints.Reason = strings.TrimSpace(constraints.Reason)
	if constraints.Reason == "" {
		constraints.Reason = fmt.Sprintf("execution constraints selected for strategy %s with policy outcome %s", coordination.Strategy, policyDecision.Outcome)
	}
	return constraints, nil
}

func restrictiveReason(strategy string) string {
	if strategy == "requires_manager_approval" {
		return "restrictive execution constraints selected because manager approval is required before any execution step"
	}
	return fmt.Sprintf("restrictive execution constraints selected because policy requires approval before continuing strategy %s", strategy)
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
