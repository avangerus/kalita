package executionruntime

import (
	"context"
	"testing"
	"time"

	"kalita/internal/actionplan"
	"kalita/internal/eventcore"
	"kalita/internal/executioncontrol"
)

func TestRunnerAllActionsSucceedMarksSessionSucceeded(t *testing.T) {
	t.Parallel()
	runner, repo, wal := newTestRunner(&fakeIDGenerator{ids: []string{"session-1", "event-1", "step-1", "step-2", "wal-1", "event-2", "wal-2", "event-3", "wal-3", "event-4", "wal-4", "event-5", "event-6"}})
	plan := testPlan([]actionplan.Action{{ID: "action-1", Type: "legacy_workflow_action", Params: map[string]any{}, Reversibility: actionplan.ReversibilityIrreversible}, {ID: "action-2", Type: "legacy_workflow_action", Params: map[string]any{}, Reversibility: actionplan.ReversibilityIrreversible}})
	session, err := runner.RunPlan(ContextWithExecution(context.Background(), ExecutionContext{ExecutionID: "exec-1", CorrelationID: "corr-1", CausationID: "cause-1"}), plan, executioncontrol.ExecutionConstraints{ID: "constraints-1"}, RunMetadata{CaseID: "case-1", WorkItemID: "work-1"})
	if err != nil {
		t.Fatalf("RunPlan error = %v", err)
	}
	if session.Status != ExecutionSessionSucceeded {
		t.Fatalf("session.Status = %s", session.Status)
	}
	steps, _ := repo.ListStepsBySession(context.Background(), session.ID)
	records, _ := wal.ListBySession(context.Background(), session.ID)
	if len(steps) != 2 || steps[0].Status != StepSucceeded || steps[1].Status != StepSucceeded {
		t.Fatalf("steps = %#v", steps)
	}
	if len(records) != 4 || records[0].Type != WALStepIntent || records[1].Type != WALStepResult || records[2].Type != WALStepIntent || records[3].Type != WALStepResult {
		t.Fatalf("records = %#v", records)
	}
}

func TestRunnerFailsStepAndCompensatesPriorCompensatableActionsInReverseOrder(t *testing.T) {
	t.Parallel()
	executor := &recordingExecutor{}
	runner, repo, wal := newTestRunnerWithExecutor(executor, &fakeIDGenerator{ids: []string{"session-1", "event-1", "step-1", "step-2", "step-3", "wal-1", "event-2", "wal-2", "event-3", "wal-3", "event-4", "wal-4", "event-5", "event-6", "wal-5", "wal-6", "event-7", "event-8"}})
	plan := testPlan([]actionplan.Action{{ID: "action-1", Type: "legacy_workflow_action", Params: map[string]any{}, Reversibility: actionplan.ReversibilityCompensatable}, {ID: "action-2", Type: "legacy_workflow_action", Params: map[string]any{}, Reversibility: actionplan.ReversibilityCompensatable}, {ID: "action-3", Type: "legacy_workflow_action", Params: map[string]any{"fail": true}, Reversibility: actionplan.ReversibilityIrreversible}})
	session, err := runner.RunPlan(context.Background(), plan, executioncontrol.ExecutionConstraints{ID: "constraints-1"}, RunMetadata{CaseID: "case-1", WorkItemID: "work-1"})
	if err != nil {
		t.Fatalf("RunPlan error = %v", err)
	}
	if session.Status != ExecutionSessionCompensated {
		t.Fatalf("session.Status = %s", session.Status)
	}
	if want := []string{"execute:action-1", "execute:action-2", "execute:action-3", "compensate:action-2", "compensate:action-1"}; !equalStrings(executor.calls, want) {
		t.Fatalf("calls = %#v", executor.calls)
	}
	steps, _ := repo.ListStepsBySession(context.Background(), session.ID)
	records, _ := wal.ListBySession(context.Background(), session.ID)
	if steps[0].Status != StepCompensated || steps[1].Status != StepCompensated || steps[2].Status != StepFailed {
		t.Fatalf("steps = %#v", steps)
	}
	if len(records) != 10 || records[6].Type != WALCompensationIntent || records[7].Type != WALCompensationResult || records[8].Type != WALCompensationIntent || records[9].Type != WALCompensationResult {
		t.Fatalf("records = %#v", records)
	}
}

func TestRunnerWritesWALIntentBeforeResultOnFailure(t *testing.T) {
	t.Parallel()
	runner, _, wal := newTestRunner(&fakeIDGenerator{ids: []string{"session-1", "event-1", "step-1", "wal-1", "event-2", "wal-2", "event-3", "event-4"}})
	plan := testPlan([]actionplan.Action{{ID: "action-1", Type: "legacy_workflow_action", Params: map[string]any{"fail": true}, Reversibility: actionplan.ReversibilityIrreversible}})
	session, err := runner.RunPlan(context.Background(), plan, executioncontrol.ExecutionConstraints{ID: "constraints-1"}, RunMetadata{CaseID: "case-1", WorkItemID: "work-1"})
	if err != nil {
		t.Fatalf("RunPlan error = %v", err)
	}
	records, _ := wal.ListBySession(context.Background(), session.ID)
	if len(records) != 2 || records[0].Type != WALStepIntent || records[1].Type != WALStepResult || records[1].Payload["status"] != string(StepFailed) {
		t.Fatalf("records = %#v", records)
	}
}

func newTestRunner(ids eventcore.IDGenerator) (*DefaultRunner, *InMemoryExecutionRepository, *InMemoryWAL) {
	return newTestRunnerWithExecutor(NewStubExecutor(), ids)
}
func newTestRunnerWithExecutor(executor ActionExecutor, ids eventcore.IDGenerator) (*DefaultRunner, *InMemoryExecutionRepository, *InMemoryWAL) {
	repo := NewInMemoryExecutionRepository()
	wal := NewInMemoryWAL()
	return NewRunner(repo, wal, executor, eventcore.NewInMemoryEventLog(), fakeClock{now: time.Date(2026, 3, 22, 16, 0, 0, 0, time.UTC)}, ids), repo, wal
}
func testPlan(actions []actionplan.Action) actionplan.ActionPlan {
	return actionplan.ActionPlan{ID: "plan-1", Actions: actions, CreatedAt: time.Date(2026, 3, 22, 16, 0, 0, 0, time.UTC), Reason: "test"}
}

type recordingExecutor struct{ calls []string }

func (e *recordingExecutor) ExecuteAction(_ context.Context, action actionplan.Action, _ executioncontrol.ExecutionConstraints) error {
	e.calls = append(e.calls, "execute:"+action.ID)
	if shouldFail(action.Params, "fail") {
		return context.DeadlineExceeded
	}
	return nil
}
func (e *recordingExecutor) CompensateAction(_ context.Context, action actionplan.Action, _ executioncontrol.ExecutionConstraints) error {
	e.calls = append(e.calls, "compensate:"+action.ID)
	return nil
}
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
