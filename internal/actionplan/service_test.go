package actionplan

import (
	"context"
	"testing"
	"time"

	"kalita/internal/eventcore"
)

func TestServiceCreatePlanEmitsExecutionEvent(t *testing.T) {
	log := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 13, 0, 0, 0, time.UTC)}
	compiler := NewCompiler(testRegistry(), clock, &fakeIDGenerator{ids: []string{"plan-1", "act-1", "comp-1"}})
	validator := NewValidator(testRegistry())
	service := NewService(compiler, validator, log, clock, &fakeIDGenerator{ids: []string{"event-1"}})

	ctx := ContextWithExecution(context.Background(), ExecutionContext{ExecutionID: "exec-1", CorrelationID: "corr-1", CausationID: "cmd-1"})
	plan, err := service.CreatePlan(ctx, "wi-1", "case-1", map[string]any{
		"reason":  "safe outreach",
		"actions": []any{map[string]any{"type": "send_notification", "params": map[string]any{"message": "hello"}}},
	})
	if err != nil {
		t.Fatalf("CreatePlan error = %v", err)
	}
	if plan.WorkItemID != "wi-1" || plan.CaseID != "case-1" {
		t.Fatalf("plan = %#v", plan)
	}
	_, events, err := log.ListByCorrelation(context.Background(), "corr-1")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(events) != 1 || events[0].Step != "action_plan_created" {
		t.Fatalf("execution events = %#v", events)
	}
}
