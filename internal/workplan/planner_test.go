package workplan

import (
	"context"
	"errors"
	"testing"
	"time"

	"kalita/internal/eventcore"
)

func TestInMemoryPlanRepositorySaveGetPlan(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryPlanRepository()
	approvedAt := time.Date(2026, 3, 23, 8, 0, 0, 0, time.UTC)
	plan := DailyPlan{
		ID:          "plan-1",
		QueueID:     "queue-1",
		PlanDate:    "2026-03-22",
		Status:      string(DailyPlanReady),
		WorkItemIDs: []string{"wi-1"},
		Assignments: map[string][]string{"emp-1": {"wi-1"}},
		CreatedAt:   time.Date(2026, 3, 22, 7, 0, 0, 0, time.UTC),
		ApprovedAt:  &approvedAt,
	}
	if err := repo.SavePlan(context.Background(), plan); err != nil {
		t.Fatalf("SavePlan error = %v", err)
	}

	got, ok, err := repo.GetPlan(context.Background(), "plan-1")
	if err != nil || !ok {
		t.Fatalf("GetPlan = %#v ok=%v err=%v", got, ok, err)
	}
	got.WorkItemIDs[0] = "mutated"
	got.Assignments["emp-1"][0] = "mutated"

	reloaded, ok, err := repo.GetPlan(context.Background(), "plan-1")
	if err != nil || !ok {
		t.Fatalf("GetPlan(reload) = %#v ok=%v err=%v", reloaded, ok, err)
	}
	if reloaded.WorkItemIDs[0] != "wi-1" || reloaded.Assignments["emp-1"][0] != "wi-1" {
		t.Fatalf("plan clone failed: %#v", reloaded)
	}
}

func TestInMemoryPlanRepositoryFindByQueueAndDate(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryPlanRepository()
	plan := DailyPlan{ID: "plan-1", QueueID: "queue-1", PlanDate: "2026-03-22", Status: string(DailyPlanDraft)}
	if err := repo.SavePlan(context.Background(), plan); err != nil {
		t.Fatalf("SavePlan error = %v", err)
	}

	got, ok, err := repo.FindPlanByQueueAndDate(context.Background(), "queue-1", "2026-03-22")
	if err != nil || !ok {
		t.Fatalf("FindPlanByQueueAndDate = %#v ok=%v err=%v", got, ok, err)
	}
	if got.ID != "plan-1" {
		t.Fatalf("plan = %#v", got)
	}
}

func TestInMemoryPlanRepositoryListPlansByQueue(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryPlanRepository()
	plans := []DailyPlan{
		{ID: "plan-1", QueueID: "queue-1", PlanDate: "2026-03-22", Status: string(DailyPlanDraft)},
		{ID: "plan-2", QueueID: "queue-1", PlanDate: "2026-03-23", Status: string(DailyPlanDraft)},
		{ID: "plan-3", QueueID: "queue-2", PlanDate: "2026-03-22", Status: string(DailyPlanDraft)},
	}
	for _, plan := range plans {
		if err := repo.SavePlan(context.Background(), plan); err != nil {
			t.Fatalf("SavePlan(%s) error = %v", plan.ID, err)
		}
	}

	got, err := repo.ListPlansByQueue(context.Background(), "queue-1")
	if err != nil {
		t.Fatalf("ListPlansByQueue error = %v", err)
	}
	if len(got) != 2 || got[0].ID != "plan-1" || got[1].ID != "plan-2" {
		t.Fatalf("plans = %#v", got)
	}
}

func TestPlannerCreatesNewPlanWhenMissing(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryPlanRepository()
	log := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"plan-1", "plan-event-1"}}
	planner := NewPlanner(repo, log, clock, ids)
	ctx := ContextWithPlanningExecution(context.Background(), PlanningExecutionContext{ExecutionID: "exec-1", CorrelationID: "corr-1", CausationID: "cmd-1"})

	plan, reused, err := planner.EnsurePlanForWorkItem(ctx, WorkQueue{ID: "queue-1"}, WorkItem{ID: "wi-1", CaseID: "case-1", QueueID: "queue-1"}, "2026-03-22")
	if err != nil {
		t.Fatalf("EnsurePlanForWorkItem error = %v", err)
	}
	if reused {
		t.Fatal("reused = true, want false")
	}
	if plan.ID != "plan-1" || plan.Status != string(DailyPlanDraft) || plan.QueueID != "queue-1" || plan.PlanDate != "2026-03-22" {
		t.Fatalf("plan = %#v", plan)
	}
	if !plan.CreatedAt.Equal(clock.now) || len(plan.WorkItemIDs) != 1 || plan.WorkItemIDs[0] != "wi-1" {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.ApprovedAt != nil || len(plan.Assignments) != 0 {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlannerReusesExistingPlanWhenQueueAndDateMatch(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryPlanRepository()
	createdAt := time.Date(2026, 3, 22, 9, 0, 0, 0, time.UTC)
	if err := repo.SavePlan(context.Background(), DailyPlan{ID: "plan-1", QueueID: "queue-1", PlanDate: "2026-03-22", Status: string(DailyPlanDraft), WorkItemIDs: []string{"wi-1"}, Assignments: map[string][]string{}, CreatedAt: createdAt}); err != nil {
		t.Fatalf("SavePlan error = %v", err)
	}
	planner := NewPlanner(repo, nil, fakeClock{now: time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)}, &fakeIDGenerator{ids: []string{"unused"}})

	plan, reused, err := planner.EnsurePlanForWorkItem(context.Background(), WorkQueue{ID: "queue-1"}, WorkItem{ID: "wi-2", CaseID: "case-1", QueueID: "queue-1"}, "2026-03-22")
	if err != nil {
		t.Fatalf("EnsurePlanForWorkItem error = %v", err)
	}
	if !reused {
		t.Fatal("reused = false, want true")
	}
	if plan.ID != "plan-1" || len(plan.WorkItemIDs) != 2 || plan.WorkItemIDs[1] != "wi-2" || !plan.CreatedAt.Equal(createdAt) {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestPlannerDoesNotDuplicateWorkItemID(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryPlanRepository()
	planner := NewPlanner(repo, nil, fakeClock{now: time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)}, &fakeIDGenerator{ids: []string{"plan-1"}})

	if _, _, err := planner.EnsurePlanForWorkItem(context.Background(), WorkQueue{ID: "queue-1"}, WorkItem{ID: "wi-1", CaseID: "case-1", QueueID: "queue-1"}, "2026-03-22"); err != nil {
		t.Fatalf("first EnsurePlanForWorkItem error = %v", err)
	}
	plan, reused, err := planner.EnsurePlanForWorkItem(context.Background(), WorkQueue{ID: "queue-1"}, WorkItem{ID: "wi-1", CaseID: "case-1", QueueID: "queue-1"}, "2026-03-22")
	if err != nil {
		t.Fatalf("second EnsurePlanForWorkItem error = %v", err)
	}
	if !reused || len(plan.WorkItemIDs) != 1 {
		t.Fatalf("plan = %#v reused=%v", plan, reused)
	}
}

func TestPlannerAppendsExecutionEventDeterministically(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryPlanRepository()
	log := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 10, 30, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"plan-1", "plan-event-1"}}
	planner := NewPlanner(repo, log, clock, ids)
	ctx := ContextWithPlanningExecution(context.Background(), PlanningExecutionContext{ExecutionID: "exec-1", CorrelationID: "corr-1", CausationID: "cmd-1"})

	plan, _, err := planner.EnsurePlanForWorkItem(ctx, WorkQueue{ID: "queue-1"}, WorkItem{ID: "wi-1", CaseID: "case-1", QueueID: "queue-1"}, "2026-03-22")
	if err != nil {
		t.Fatalf("EnsurePlanForWorkItem error = %v", err)
	}
	_, executionEvents, err := log.ListByCorrelation(context.Background(), "corr-1")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(executionEvents) != 1 {
		t.Fatalf("executionEvents len = %d", len(executionEvents))
	}
	got := executionEvents[0]
	if got.ID != "plan-event-1" || got.ExecutionID != "exec-1" || got.CaseID != "case-1" || got.Step != "daily_plan_intake" || got.Status != "attached" || !got.OccurredAt.Equal(clock.now) {
		t.Fatalf("execution event = %#v", got)
	}
	if got.Payload["daily_plan_id"] != plan.ID || got.Payload["plan_date"] != "2026-03-22" || got.Payload["work_item_id"] != "wi-1" || got.Payload["queue_id"] != "queue-1" || got.Payload["case_id"] != "case-1" {
		t.Fatalf("execution event payload = %#v", got.Payload)
	}
}

type failingPlanRepository struct{ err error }

func (f failingPlanRepository) SavePlan(context.Context, DailyPlan) error {
	return f.err
}

func (f failingPlanRepository) GetPlan(context.Context, string) (DailyPlan, bool, error) {
	return DailyPlan{}, false, f.err
}

func (f failingPlanRepository) FindPlanByQueueAndDate(context.Context, string, string) (DailyPlan, bool, error) {
	return DailyPlan{}, false, f.err
}

func (f failingPlanRepository) ListPlansByQueue(context.Context, string) ([]DailyPlan, error) {
	return nil, f.err
}

func TestPlannerReturnsRepositoryError(t *testing.T) {
	t.Parallel()

	planner := NewPlanner(failingPlanRepository{err: errors.New("plan repo failed")}, nil, fakeClock{now: time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)}, &fakeIDGenerator{ids: []string{"unused"}})
	if _, _, err := planner.EnsurePlanForWorkItem(context.Background(), WorkQueue{ID: "queue-1"}, WorkItem{ID: "wi-1", CaseID: "case-1"}, "2026-03-22"); err == nil {
		t.Fatal("EnsurePlanForWorkItem error = nil, want non-nil")
	}
}
