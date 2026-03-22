package policy

import (
	"context"
	"testing"

	"kalita/internal/workplan"
)

func TestDeterministicEvaluatorSelectedAllows(t *testing.T) {
	e := NewEvaluator()
	outcome, reason, err := e.EvaluateCoordinationDecision(context.Background(), workplan.CoordinationDecision{ID: "coord-1", WorkItemID: "wi-1", Outcome: workplan.CoordinationSelected, Strategy: workplan.DefaultCoordinationStrategy})
	if err != nil {
		t.Fatalf("EvaluateCoordinationDecision error = %v", err)
	}
	if outcome != PolicyAllow {
		t.Fatalf("outcome = %q", outcome)
	}
	if reason == "" {
		t.Fatal("reason is empty")
	}
}

func TestDeterministicEvaluatorRequiresApprovalStrategy(t *testing.T) {
	e := NewEvaluator()
	outcome, reason, err := e.EvaluateCoordinationDecision(context.Background(), workplan.CoordinationDecision{ID: "coord-1", WorkItemID: "wi-1", Outcome: workplan.CoordinationSelected, Strategy: "requires_manager_approval"})
	if err != nil {
		t.Fatalf("EvaluateCoordinationDecision error = %v", err)
	}
	if outcome != PolicyRequireApproval {
		t.Fatalf("outcome = %q", outcome)
	}
	if reason == "" {
		t.Fatal("reason is empty")
	}
}

func TestDeterministicEvaluatorBlockedStrategyDenies(t *testing.T) {
	e := NewEvaluator()
	outcome, reason, err := e.EvaluateCoordinationDecision(context.Background(), workplan.CoordinationDecision{ID: "coord-1", WorkItemID: "wi-1", Outcome: workplan.CoordinationSelected, Strategy: "blocked_strategy"})
	if err != nil {
		t.Fatalf("EvaluateCoordinationDecision error = %v", err)
	}
	if outcome != PolicyDeny {
		t.Fatalf("outcome = %q", outcome)
	}
	if reason == "" {
		t.Fatal("reason is empty")
	}
}
