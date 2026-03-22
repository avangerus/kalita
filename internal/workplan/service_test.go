package workplan

import (
	"context"
	"fmt"
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
	ids := &fakeIDGenerator{ids: []string{"work-1", "work-event-1", "plan-1", "plan-event-1"}}
	planRepo := NewInMemoryPlanRepository()
	planner := NewPlanner(planRepo, log, clock, ids)
	service := NewService(repo, NewRouter(repo, ""), planner, log, clock, ids)
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
	if result.WorkItem.PlanID != "plan-1" {
		t.Fatalf("plan id = %q", result.WorkItem.PlanID)
	}
	if !result.WorkItem.CreatedAt.Equal(clock.now) || !result.WorkItem.UpdatedAt.Equal(clock.now) {
		t.Fatalf("timestamps = %#v", result.WorkItem)
	}
	_, executionEvents, err := log.ListByCorrelation(context.Background(), "corr-1")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(executionEvents) != 2 {
		t.Fatalf("executionEvents len = %d", len(executionEvents))
	}
	if executionEvents[0].Step != "work_item_intake" || executionEvents[0].Status != "created" {
		t.Fatalf("first execution event = %#v", executionEvents[0])
	}
	if executionEvents[1].Step != "daily_plan_intake" || executionEvents[1].Status != "attached" {
		t.Fatalf("second execution event = %#v", executionEvents[1])
	}
	if executionEvents[1].Payload["case_id"] != "case-1" || executionEvents[1].Payload["queue_id"] != "queue-1" || executionEvents[1].Payload["work_item_id"] != "work-1" || executionEvents[1].Payload["daily_plan_id"] != "plan-1" || executionEvents[1].Payload["plan_date"] != "2026-03-22" {
		t.Fatalf("execution event payload = %#v", executionEvents[1].Payload)
	}
}

type errPlanner struct{ err error }

func (p errPlanner) EnsurePlanForWorkItem(context.Context, WorkQueue, WorkItem, string) (DailyPlan, bool, error) {
	return DailyPlan{}, false, p.err
}

func TestServiceReturnsErrorWhenPlanAttachmentFails(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryQueueRepository()
	if err := repo.SaveQueue(context.Background(), WorkQueue{ID: "queue-1", AllowedCaseKinds: []string{"workflow.action"}}); err != nil {
		t.Fatalf("SaveQueue error = %v", err)
	}
	service := NewService(repo, NewRouter(repo, ""), errPlanner{err: fmt.Errorf("plan attach failed")}, eventcore.NewInMemoryEventLog(), fakeClock{now: time.Date(2026, 3, 22, 15, 0, 0, 0, time.UTC)}, &fakeIDGenerator{ids: []string{"work-1", "work-event-1"}})
	resolved := caseruntime.ResolutionResult{Command: eventcore.Command{ID: "cmd-1", Type: "workflow.action", CorrelationID: "corr-1", ExecutionID: "exec-1", TargetRef: "test.WorkflowTask/rec-1"}, Case: caseruntime.Case{ID: "case-1", Kind: "workflow.action"}}

	if _, err := service.IntakeCommand(context.Background(), resolved); err == nil {
		t.Fatal("IntakeCommand error = nil, want non-nil")
	}
}
