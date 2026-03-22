package executioncontrol

import (
	"context"
	"testing"

	"kalita/internal/policy"
	"kalita/internal/workplan"
)

func TestDeterministicPlannerAllowDefaultQueueSelection(t *testing.T) {
	t.Parallel()
	planner := NewPlanner()
	constraints, err := planner.PlanForPolicyDecision(context.Background(), workplan.CoordinationDecision{ID: "coord-1", CaseID: "case-1", WorkItemID: "work-1", QueueID: "queue-1", Strategy: workplan.DefaultCoordinationStrategy}, policy.PolicyDecision{ID: "pol-1", Outcome: policy.PolicyAllow})
	if err != nil {
		t.Fatalf("PlanForPolicyDecision error = %v", err)
	}
	if constraints.ExecutionMode != ExecutionModeDeterministicAPI || constraints.RiskLevel != RiskMedium || constraints.MaxSteps != 10 || constraints.MaxDurationSec != 300 {
		t.Fatalf("constraints = %#v", constraints)
	}
	if len(constraints.AllowedScopes) != 1 || constraints.AllowedScopes[0] != "default" {
		t.Fatalf("AllowedScopes = %#v", constraints.AllowedScopes)
	}
	if constraints.Reason == "" {
		t.Fatal("Reason is empty")
	}
}

func TestDeterministicPlannerRequireApprovalRestrictive(t *testing.T) {
	t.Parallel()
	planner := NewPlanner()
	constraints, err := planner.PlanForPolicyDecision(context.Background(), workplan.CoordinationDecision{ID: "coord-1", CaseID: "case-1", WorkItemID: "work-1", QueueID: "queue-1", Strategy: workplan.DefaultCoordinationStrategy}, policy.PolicyDecision{ID: "pol-1", Outcome: policy.PolicyRequireApproval})
	if err != nil {
		t.Fatalf("PlanForPolicyDecision error = %v", err)
	}
	if constraints.ExecutionMode != ExecutionModeApprovalEachStep || constraints.RiskLevel != RiskHigh || constraints.MaxSteps != 1 || constraints.MaxDurationSec != 60 {
		t.Fatalf("constraints = %#v", constraints)
	}
	if constraints.Reason == "" {
		t.Fatal("Reason is empty")
	}
}

func TestDeterministicPlannerUIOperatorMode(t *testing.T) {
	t.Parallel()
	planner := NewPlanner()
	constraints, err := planner.PlanForPolicyDecision(context.Background(), workplan.CoordinationDecision{ID: "coord-1", CaseID: "case-1", WorkItemID: "work-1", QueueID: "queue-1", Strategy: "ui_operator_mode"}, policy.PolicyDecision{ID: "pol-1", Outcome: policy.PolicyAllow})
	if err != nil {
		t.Fatalf("PlanForPolicyDecision error = %v", err)
	}
	if constraints.ExecutionMode != ExecutionModeGuidedUIOperator || constraints.RiskLevel != RiskHigh || constraints.MaxSteps != 20 || constraints.MaxDurationSec != 900 {
		t.Fatalf("constraints = %#v", constraints)
	}
	if len(constraints.AllowedScopes) != 2 || constraints.AllowedScopes[1] != UIScopeMarker {
		t.Fatalf("AllowedScopes = %#v", constraints.AllowedScopes)
	}
	if constraints.Reason == "" {
		t.Fatal("Reason is empty")
	}
}

func TestDeterministicPlannerCheckpointedMode(t *testing.T) {
	t.Parallel()
	planner := NewPlanner()
	constraints, err := planner.PlanForPolicyDecision(context.Background(), workplan.CoordinationDecision{ID: "coord-1", CaseID: "case-1", WorkItemID: "work-1", QueueID: "queue-1", Strategy: "checkpointed_mode"}, policy.PolicyDecision{ID: "pol-1", Outcome: policy.PolicyAllow})
	if err != nil {
		t.Fatalf("PlanForPolicyDecision error = %v", err)
	}
	if constraints.ExecutionMode != ExecutionModeCheckpointedAutonomy || constraints.RiskLevel != RiskHigh || constraints.MaxSteps != 5 || constraints.MaxDurationSec != 300 {
		t.Fatalf("constraints = %#v", constraints)
	}
	if constraints.Reason == "" {
		t.Fatal("Reason is empty")
	}
}

func TestDeterministicPlannerDenyReturnsError(t *testing.T) {
	t.Parallel()
	planner := NewPlanner()
	if _, err := planner.PlanForPolicyDecision(context.Background(), workplan.CoordinationDecision{ID: "coord-1", Strategy: workplan.DefaultCoordinationStrategy}, policy.PolicyDecision{ID: "pol-1", Outcome: policy.PolicyDeny}); err == nil {
		t.Fatal("expected error for deny outcome")
	}
}

func TestDeterministicPlannerAllBranchesReturnReason(t *testing.T) {
	t.Parallel()
	planner := NewPlanner()
	cases := []struct {
		coordination workplan.CoordinationDecision
		decision     policy.PolicyDecision
	}{
		{coordination: workplan.CoordinationDecision{ID: "coord-1", Strategy: workplan.DefaultCoordinationStrategy}, decision: policy.PolicyDecision{ID: "pol-1", Outcome: policy.PolicyAllow}},
		{coordination: workplan.CoordinationDecision{ID: "coord-2", Strategy: "requires_manager_approval"}, decision: policy.PolicyDecision{ID: "pol-2", Outcome: policy.PolicyAllow}},
		{coordination: workplan.CoordinationDecision{ID: "coord-3", Strategy: "ui_operator_mode"}, decision: policy.PolicyDecision{ID: "pol-3", Outcome: policy.PolicyAllow}},
		{coordination: workplan.CoordinationDecision{ID: "coord-4", Strategy: "checkpointed_mode"}, decision: policy.PolicyDecision{ID: "pol-4", Outcome: policy.PolicyAllow}},
		{coordination: workplan.CoordinationDecision{ID: "coord-5", Strategy: workplan.DefaultCoordinationStrategy}, decision: policy.PolicyDecision{ID: "pol-5", Outcome: policy.PolicyRequireApproval}},
	}
	for _, tc := range cases {
		constraints, err := planner.PlanForPolicyDecision(context.Background(), tc.coordination, tc.decision)
		if err != nil {
			t.Fatalf("PlanForPolicyDecision(%s,%s) error = %v", tc.coordination.Strategy, tc.decision.Outcome, err)
		}
		if constraints.Reason == "" {
			t.Fatalf("Reason empty for strategy=%s outcome=%s", tc.coordination.Strategy, tc.decision.Outcome)
		}
	}
}
