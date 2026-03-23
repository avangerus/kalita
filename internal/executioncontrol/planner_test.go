package executioncontrol

import (
	"context"
	"testing"

	"kalita/internal/policy"
	"kalita/internal/workplan"
)

func TestDeterministicPlannerExecuteNowUsesBaselineConstraints(t *testing.T) {
	t.Parallel()
	planner := NewPlanner()
	constraints, err := planner.PlanForPolicyDecision(context.Background(), workplan.CoordinationDecision{ID: "coord-1", CaseID: "case-1", WorkItemID: "work-1", QueueID: "queue-1", DecisionType: workplan.CoordinationExecuteNow}, policy.PolicyDecision{ID: "pol-1", Outcome: policy.PolicyAllow})
	if err != nil {
		t.Fatalf("PlanForPolicyDecision error = %v", err)
	}
	if constraints.ExecutionMode != ExecutionModeDeterministicAPI || constraints.RiskLevel != RiskMedium || constraints.MaxSteps != 10 || constraints.MaxDurationSec != 300 {
		t.Fatalf("constraints = %#v", constraints)
	}
}

func TestDeterministicPlannerRequireApprovalIsRestrictive(t *testing.T) {
	t.Parallel()
	planner := NewPlanner()
	constraints, err := planner.PlanForPolicyDecision(context.Background(), workplan.CoordinationDecision{ID: "coord-1", DecisionType: workplan.CoordinationExecuteNow}, policy.PolicyDecision{ID: "pol-1", Outcome: policy.PolicyRequireApproval})
	if err != nil {
		t.Fatalf("PlanForPolicyDecision error = %v", err)
	}
	if constraints.ExecutionMode != ExecutionModeApprovalEachStep || constraints.RiskLevel != RiskHigh || constraints.MaxSteps != 1 || constraints.MaxDurationSec != 60 {
		t.Fatalf("constraints = %#v", constraints)
	}
}

func TestDeterministicPlannerDeferIsRestrictive(t *testing.T) {
	t.Parallel()
	planner := NewPlanner()
	constraints, err := planner.PlanForPolicyDecision(context.Background(), workplan.CoordinationDecision{ID: "coord-1", DecisionType: workplan.CoordinationDefer}, policy.PolicyDecision{ID: "pol-1", Outcome: policy.PolicyAllow})
	if err != nil {
		t.Fatalf("PlanForPolicyDecision error = %v", err)
	}
	if constraints.ExecutionMode != ExecutionModeApprovalEachStep || constraints.RiskLevel != RiskHigh {
		t.Fatalf("constraints = %#v", constraints)
	}
}

func TestDeterministicPlannerEscalateRaisesCriticalRisk(t *testing.T) {
	t.Parallel()
	planner := NewPlanner()
	constraints, err := planner.PlanForPolicyDecision(context.Background(), workplan.CoordinationDecision{ID: "coord-1", DecisionType: workplan.CoordinationEscalate}, policy.PolicyDecision{ID: "pol-1", Outcome: policy.PolicyAllow})
	if err != nil {
		t.Fatalf("PlanForPolicyDecision error = %v", err)
	}
	if constraints.ExecutionMode != ExecutionModeApprovalEachStep || constraints.RiskLevel != RiskCritical {
		t.Fatalf("constraints = %#v", constraints)
	}
}

func TestDeterministicPlannerDenyReturnsError(t *testing.T) {
	t.Parallel()
	planner := NewPlanner()
	if _, err := planner.PlanForPolicyDecision(context.Background(), workplan.CoordinationDecision{ID: "coord-1", DecisionType: workplan.CoordinationExecuteNow}, policy.PolicyDecision{ID: "pol-1", Outcome: policy.PolicyDeny}); err == nil {
		t.Fatal("expected error for deny outcome")
	}
}
