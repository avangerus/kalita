package executionruntime

import (
	"context"
	"testing"
	"time"

	"kalita/internal/actionplan"
	"kalita/internal/eventcore"
	"kalita/internal/executioncontrol"
)

func TestServiceEmitsExecutionSessionAndStepEvents(t *testing.T) {
	t.Parallel()
	repo, wal, log := NewInMemoryExecutionRepository(), NewInMemoryWAL(), eventcore.NewInMemoryEventLog()
	ids := &fakeIDGenerator{ids: []string{"session-1", "event-1", "step-1", "wal-1", "event-2", "wal-2", "event-3", "event-4"}}
	service := NewService(NewRunner(repo, wal, NewStubExecutor(), log, fakeClock{now: time.Date(2026, 3, 22, 17, 0, 0, 0, time.UTC)}, ids))
	_, err := service.StartExecution(ContextWithExecution(context.Background(), ExecutionContext{ExecutionID: "exec-1", CorrelationID: "corr-1", CausationID: "cause-1"}), testPlan([]actionplan.Action{{ID: "action-1", Type: "legacy_workflow_action", Params: map[string]any{}, Reversibility: actionplan.ReversibilityIrreversible}}), executioncontrol.ExecutionConstraints{ID: "constraints-1"}, RunMetadata{CaseID: "case-1", WorkItemID: "work-1"})
	if err != nil {
		t.Fatalf("StartExecution error = %v", err)
	}
	_, events, err := log.ListByCorrelation(context.Background(), "corr-1")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(events) < 4 || events[0].Step != "execution_session_created" || events[1].Step != "execution_step_started" || events[2].Step != "execution_step_succeeded" || events[3].Step != "execution_session_succeeded" {
		t.Fatalf("events = %#v", events)
	}
}

func TestStubExecutorFailureRecordsReasons(t *testing.T) {
	t.Parallel()
	repo, wal := NewInMemoryExecutionRepository(), NewInMemoryWAL()
	service := NewService(NewRunner(repo, wal, NewStubExecutor(), eventcore.NewInMemoryEventLog(), fakeClock{now: time.Date(2026, 3, 22, 17, 30, 0, 0, time.UTC)}, &fakeIDGenerator{ids: []string{"session-1", "event-1", "step-1", "wal-1", "event-2", "wal-2", "event-3", "event-4"}}))
	session, err := service.StartExecution(context.Background(), testPlan([]actionplan.Action{{ID: "action-1", Type: "legacy_workflow_action", Params: map[string]any{"fail": true}, Reversibility: actionplan.ReversibilityIrreversible}}), executioncontrol.ExecutionConstraints{ID: "constraints-1"}, RunMetadata{CaseID: "case-1", WorkItemID: "work-1"})
	if err != nil {
		t.Fatalf("StartExecution error = %v", err)
	}
	steps, _ := repo.ListStepsBySession(context.Background(), session.ID)
	if session.Status != ExecutionSessionFailed || session.FailureReason == "" || len(steps) != 1 || steps[0].Status != StepFailed || steps[0].FailureReason == "" {
		t.Fatalf("session=%#v steps=%#v", session, steps)
	}
}

type fakeClock struct{ now time.Time }

func (f fakeClock) Now() time.Time { return f.now }

type fakeIDGenerator struct {
	ids []string
	i   int
}

func (f *fakeIDGenerator) NewID() string {
	if f.i >= len(f.ids) {
		return "generated-id"
	}
	id := f.ids[f.i]
	f.i++
	return id
}
