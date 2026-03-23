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

func NewPlanner() *DeterministicPlanner { return &DeterministicPlanner{} }

func (p *DeterministicPlanner) PlanForPolicyDecision(_ context.Context, coordination workplan.CoordinationDecision, policyDecision policy.PolicyDecision) (ExecutionConstraints, error) {
	if policyDecision.Outcome == policy.PolicyDeny {
		return ExecutionConstraints{}, fmt.Errorf("execution constraints cannot be created for denied policy decision %s", policyDecision.ID)
	}
	constraints := ExecutionConstraints{CoordinationDecisionID: coordination.ID, PolicyDecisionID: policyDecision.ID, CaseID: coordination.CaseID, WorkItemID: coordination.WorkItemID, QueueID: coordination.QueueID, RiskLevel: RiskMedium, ExecutionMode: ExecutionModeDeterministicAPI, MaxSteps: 10, MaxDurationSec: 300, AllowedScopes: []string{"default"}, ForbiddenOps: []string{}, Reason: fmt.Sprintf("baseline execution constraints selected for coordination decision %s with policy outcome %s", coordination.DecisionType, policyDecision.Outcome)}

	switch coordination.DecisionType {
	case workplan.CoordinationDefer:
		constraints.RiskLevel = RiskHigh
		constraints.ExecutionMode = ExecutionModeApprovalEachStep
		constraints.MaxSteps = 1
		constraints.MaxDurationSec = 60
		constraints.Reason = "restrictive execution constraints selected because coordination deferred automatic execution"
	case workplan.CoordinationEscalate:
		constraints.RiskLevel = RiskCritical
		constraints.ExecutionMode = ExecutionModeApprovalEachStep
		constraints.MaxSteps = 1
		constraints.MaxDurationSec = 60
		constraints.Reason = "restrictive execution constraints selected because coordination escalated the work item"
	}
	if policyDecision.Outcome == policy.PolicyRequireApproval {
		constraints.RiskLevel = RiskHigh
		constraints.ExecutionMode = ExecutionModeApprovalEachStep
		constraints.MaxSteps = 1
		constraints.MaxDurationSec = 60
		constraints.Reason = restrictiveReason(coordination.DecisionType)
	}
	constraints.Reason = strings.TrimSpace(constraints.Reason)
	return constraints, nil
}

func restrictiveReason(decisionType workplan.CoordinationDecisionType) string {
	return fmt.Sprintf("restrictive execution constraints selected because policy requires approval before continuing coordination decision %s", decisionType)
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
