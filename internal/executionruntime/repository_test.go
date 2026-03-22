package executionruntime

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryExecutionRepositorySaveGetListSessions(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryExecutionRepository()
	now := time.Date(2026, 3, 22, 16, 0, 0, 0, time.UTC)
	session := ExecutionSession{ID: "session-1", WorkItemID: "work-1", Status: ExecutionSessionPending, CreatedAt: now, UpdatedAt: now}
	if err := repo.SaveSession(context.Background(), session); err != nil {
		t.Fatalf("SaveSession error = %v", err)
	}
	got, ok, err := repo.GetSession(context.Background(), "session-1")
	if err != nil || !ok {
		t.Fatalf("GetSession = %#v ok=%v err=%v", got, ok, err)
	}
	list, err := repo.ListSessionsByWorkItem(context.Background(), "work-1")
	if err != nil {
		t.Fatalf("ListSessionsByWorkItem error = %v", err)
	}
	if got.ID != session.ID || len(list) != 1 || list[0].ID != session.ID {
		t.Fatalf("got=%#v list=%#v", got, list)
	}
}

func TestInMemoryExecutionRepositorySaveGetListStepsPreservesOrdering(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryExecutionRepository()
	steps := []StepExecution{{ID: "step-2", ExecutionSessionID: "session-1", StepIndex: 1}, {ID: "step-1", ExecutionSessionID: "session-1", StepIndex: 0}, {ID: "step-3", ExecutionSessionID: "session-1", StepIndex: 2}}
	for _, step := range steps {
		if err := repo.SaveStep(context.Background(), step); err != nil {
			t.Fatalf("SaveStep error = %v", err)
		}
	}
	got, ok, err := repo.GetStep(context.Background(), "step-1")
	if err != nil || !ok {
		t.Fatalf("GetStep = %#v ok=%v err=%v", got, ok, err)
	}
	list, err := repo.ListStepsBySession(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("ListStepsBySession error = %v", err)
	}
	if got.StepIndex != 0 || len(list) != 3 || list[0].ID != "step-1" || list[1].ID != "step-2" || list[2].ID != "step-3" {
		t.Fatalf("got=%#v list=%#v", got, list)
	}
}
