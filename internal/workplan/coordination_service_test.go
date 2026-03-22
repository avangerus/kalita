package workplan

import (
	"context"
	"testing"
	"time"

	"kalita/internal/eventcore"
)

func TestCoordinatorCreatesSelectedDecisionAndExecutionEventDeterministically(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryCoordinationRepository()
	log := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 16, 30, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"coord-1", "coord-event-1"}}
	coordinator := NewCoordinator(repo, log, clock, ids)
	wi := WorkItem{ID: "wi-1", CaseID: "case-1", QueueID: "queue-1"}
	ctx := ContextWithPlanningExecution(context.Background(), PlanningExecutionContext{ExecutionID: "exec-1", CorrelationID: "corr-1", CausationID: "cmd-1"})

	decision, err := coordinator.CoordinateWorkItem(ctx, wi)
	if err != nil {
		t.Fatalf("CoordinateWorkItem error = %v", err)
	}
	if decision.ID != "coord-1" || decision.Strategy != DefaultCoordinationStrategy || decision.SelectedBy != "system" || decision.Outcome != CoordinationSelected {
		t.Fatalf("decision = %#v", decision)
	}
	if decision.Reason == "" || !decision.CreatedAt.Equal(clock.now) {
		t.Fatalf("decision = %#v", decision)
	}
	stored, ok, err := repo.GetDecision(context.Background(), "coord-1")
	if err != nil || !ok {
		t.Fatalf("GetDecision = %#v ok=%v err=%v", stored, ok, err)
	}
	if stored != decision {
		t.Fatalf("stored = %#v, want %#v", stored, decision)
	}
	_, executionEvents, err := log.ListByCorrelation(context.Background(), "corr-1")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(executionEvents) != 1 {
		t.Fatalf("execution events len = %d", len(executionEvents))
	}
	got := executionEvents[0]
	if got.ID != "coord-event-1" || got.Step != "coordination_decision" || got.Status != "selected" || got.ExecutionID != "exec-1" || got.CausationID != "cmd-1" {
		t.Fatalf("execution event = %#v", got)
	}
	if got.Payload["coordination_decision_id"] != "coord-1" || got.Payload["strategy"] != DefaultCoordinationStrategy {
		t.Fatalf("payload = %#v", got.Payload)
	}
}
