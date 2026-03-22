package workplan

import (
	"context"
	"testing"
	"time"

	"kalita/internal/caseruntime"
	"kalita/internal/eventcore"
)

type fakeClock struct{ now time.Time }

func (f fakeClock) Now() time.Time { return f.now }

type fakeIDGenerator struct {
	ids []string
	i   int
}

func (f *fakeIDGenerator) NewID() string { id := f.ids[f.i]; f.i++; return id }

func TestServiceCreatesWorkItemAndExecutionEventDeterministically(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryQueueRepository()
	if err := repo.SaveQueue(context.Background(), WorkQueue{ID: "queue-1", AllowedCaseKinds: []string{"workflow.action"}}); err != nil {
		t.Fatalf("SaveQueue error = %v", err)
	}
	log := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 15, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"work-1", "event-1"}}
	service := NewService(repo, NewRouter(repo, ""), log, clock, ids)
	resolved := caseruntime.ResolutionResult{Command: eventcore.Command{ID: "cmd-1", Type: "workflow.action", CorrelationID: "corr-1", ExecutionID: "exec-1", TargetRef: "test.WorkflowTask/rec-1"}, Case: caseruntime.Case{ID: "case-1", Kind: "workflow.action"}}

	result, err := service.IntakeCommand(context.Background(), resolved)
	if err != nil {
		t.Fatalf("IntakeCommand error = %v", err)
	}
	if result.WorkItem.ID != "work-1" || result.WorkItem.CaseID != "case-1" || result.WorkItem.QueueID != "queue-1" {
		t.Fatalf("work item = %#v", result.WorkItem)
	}
	if result.WorkItem.Type != "workflow.action" || result.WorkItem.Status != string(WorkItemOpen) {
		t.Fatalf("work item = %#v", result.WorkItem)
	}
	if result.WorkItem.Reason != "intake workflow.action for test.WorkflowTask/rec-1" {
		t.Fatalf("reason = %q", result.WorkItem.Reason)
	}
	if !result.WorkItem.CreatedAt.Equal(clock.now) || !result.WorkItem.UpdatedAt.Equal(clock.now) {
		t.Fatalf("timestamps = %#v", result.WorkItem)
	}
	_, executionEvents, err := log.ListByCorrelation(context.Background(), "corr-1")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(executionEvents) != 1 {
		t.Fatalf("executionEvents len = %d", len(executionEvents))
	}
	if executionEvents[0].Step != "work_item_intake" || executionEvents[0].Status != "created" {
		t.Fatalf("execution event = %#v", executionEvents[0])
	}
	if executionEvents[0].Payload["case_id"] != "case-1" || executionEvents[0].Payload["queue_id"] != "queue-1" || executionEvents[0].Payload["work_item_id"] != "work-1" {
		t.Fatalf("execution event payload = %#v", executionEvents[0].Payload)
	}
}
