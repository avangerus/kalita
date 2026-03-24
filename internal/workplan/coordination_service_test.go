package workplan

import (
	"context"
	"testing"
	"time"

	"kalita/internal/eventcore"
)

func TestCoordinatorExecutesNowForHighTrustActor(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryCoordinationRepository()
	log := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 16, 30, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"coord-1", "coord-event-1"}}
	coordinator := NewCoordinator(repo, log, clock, ids)
	wi := WorkItem{ID: "wi-1", CaseID: "case-1", QueueID: "queue-1", Status: string(WorkItemOpen)}
	ctx := ContextWithPlanningExecution(context.Background(), PlanningExecutionContext{ExecutionID: "exec-1", CorrelationID: "corr-1", CausationID: "cmd-1"})

	decision, err := coordinator.Decide(ctx, wi, CoordinationContext{ActionTypes: []string{"legacy_workflow_action"}, Complexity: 1, Actors: []CoordinationActor{{ID: "emp-1", Enabled: true, QueueMemberships: []string{"queue-1"}, AllowedActionTypes: []string{"legacy_workflow_action"}}}, Profiles: map[string]CoordinationActorProfile{"emp-1": {ActorID: "emp-1", MaxComplexity: 3, TrustLevel: "high", TrustAvailable: true}}})
	if err != nil {
		t.Fatalf("Decide error = %v", err)
	}
	if decision.DecisionType != CoordinationExecuteNow || decision.Priority != CoordinationPriorityExecuteNow {
		t.Fatalf("decision = %#v", decision)
	}
	_, executionEvents, err := log.ListByCorrelation(context.Background(), "corr-1")
	if err != nil || len(executionEvents) != 1 {
		t.Fatalf("events err=%v len=%d", err, len(executionEvents))
	}
	if executionEvents[0].Step != "coordination_decision_made" || executionEvents[0].Status != string(CoordinationExecuteNow) {
		t.Fatalf("execution event = %#v", executionEvents[0])
	}
}

func TestCoordinatorDefersForOnlyLowTrustActors(t *testing.T) {
	t.Parallel()
	decision, err := NewCoordinator(NewInMemoryCoordinationRepository(), nil, fakeClock{now: time.Date(2026, 3, 22, 16, 30, 0, 0, time.UTC)}, &fakeIDGenerator{ids: []string{"coord-1"}}).Decide(context.Background(), WorkItem{ID: "wi-1", CaseID: "case-1", QueueID: "queue-1", Status: string(WorkItemOpen)}, CoordinationContext{ActionTypes: []string{"legacy_workflow_action"}, Complexity: 1, Actors: []CoordinationActor{{ID: "emp-1", Enabled: true, QueueMemberships: []string{"queue-1"}, AllowedActionTypes: []string{"legacy_workflow_action"}}}, Profiles: map[string]CoordinationActorProfile{"emp-1": {ActorID: "emp-1", MaxComplexity: 2, TrustLevel: "low", TrustAvailable: true}}})
	if err != nil || decision.DecisionType != CoordinationDefer {
		t.Fatalf("decision=%#v err=%v", decision, err)
	}
}

func TestCoordinatorBlocksWhenNoEligibleActorExists(t *testing.T) {
	t.Parallel()
	decision, err := NewCoordinator(NewInMemoryCoordinationRepository(), nil, fakeClock{now: time.Date(2026, 3, 22, 16, 30, 0, 0, time.UTC)}, &fakeIDGenerator{ids: []string{"coord-1"}}).Decide(context.Background(), WorkItem{ID: "wi-1", CaseID: "case-1", QueueID: "queue-1", Status: string(WorkItemOpen)}, CoordinationContext{ActionTypes: []string{"legacy_workflow_action"}, Complexity: 1, Actors: []CoordinationActor{{ID: "emp-1", Enabled: false, QueueMemberships: []string{"queue-1"}, AllowedActionTypes: []string{"legacy_workflow_action"}}}})
	if err != nil || decision.DecisionType != CoordinationBlock {
		t.Fatalf("decision=%#v err=%v", decision, err)
	}
}

func TestCoordinatorEscalatesWhenComplexityExceedsAvailableProfiles(t *testing.T) {
	t.Parallel()
	decision, err := NewCoordinator(NewInMemoryCoordinationRepository(), nil, fakeClock{now: time.Date(2026, 3, 22, 16, 30, 0, 0, time.UTC)}, &fakeIDGenerator{ids: []string{"coord-1"}}).Decide(context.Background(), WorkItem{ID: "wi-1", CaseID: "case-1", QueueID: "queue-1", Status: string(WorkItemOpen)}, CoordinationContext{ActionTypes: []string{"legacy_workflow_action", "legacy_workflow_action"}, Complexity: 2, Actors: []CoordinationActor{{ID: "emp-1", Enabled: true, QueueMemberships: []string{"queue-1"}, AllowedActionTypes: []string{"legacy_workflow_action"}}}, Profiles: map[string]CoordinationActorProfile{"emp-1": {ActorID: "emp-1", MaxComplexity: 1, TrustLevel: "high", TrustAvailable: true}}})
	if err != nil || decision.DecisionType != CoordinationEscalate {
		t.Fatalf("decision=%#v err=%v", decision, err)
	}
}

func TestCoordinatorIsDeterministic(t *testing.T) {
	t.Parallel()
	coordinationContext := CoordinationContext{ActionTypes: []string{"legacy_workflow_action"}, Complexity: 1, Actors: []CoordinationActor{{ID: "emp-1", Enabled: true, QueueMemberships: []string{"queue-1"}, AllowedActionTypes: []string{"legacy_workflow_action"}}}, Profiles: map[string]CoordinationActorProfile{"emp-1": {ActorID: "emp-1", MaxComplexity: 2, TrustLevel: "high", TrustAvailable: true}}}
	coordinator := NewCoordinator(NewInMemoryCoordinationRepository(), nil, fakeClock{now: time.Date(2026, 3, 22, 16, 30, 0, 0, time.UTC)}, &fakeIDGenerator{ids: []string{"coord-1", "coord-2"}})
	first, err := coordinator.Decide(context.Background(), WorkItem{ID: "wi-1", CaseID: "case-1", QueueID: "queue-1", Status: string(WorkItemOpen)}, coordinationContext)
	if err != nil {
		t.Fatalf("first Decide error = %v", err)
	}
	second, err := coordinator.Decide(context.Background(), WorkItem{ID: "wi-2", CaseID: "case-2", QueueID: "queue-1", Status: string(WorkItemOpen)}, coordinationContext)
	if err != nil {
		t.Fatalf("second Decide error = %v", err)
	}
	if first.DecisionType != second.DecisionType || first.Priority != second.Priority || first.Reason != second.Reason {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
}

func TestCoordinatorQueuePressurePrefersDeferWhenBacklogExceedsThreshold(t *testing.T) {
	t.Parallel()
	queueRepo := NewInMemoryQueueRepository()
	base := time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		if err := queueRepo.SaveWorkItem(context.Background(), WorkItem{ID: "wi-backlog-" + string(rune('a'+i)), CaseID: "case-backlog", QueueID: "queue-1", Type: "legacy_workflow_action", Status: string(WorkItemOpen), CreatedAt: base, UpdatedAt: base}); err != nil {
			t.Fatalf("SaveWorkItem error = %v", err)
		}
	}
	coordinator := NewCoordinationService(
		NewInMemoryCoordinationRepository(),
		queueRepo,
		nil,
		CoordinationConfig{QueueDepthThreshold: 2},
		NewQueuePressureScorer(CoordinationConfig{QueueDepthThreshold: 2}, queueRepo, nil),
		nil,
		fakeClock{now: base.Add(time.Hour)},
		&fakeIDGenerator{ids: []string{"coord-1"}},
	)
	decision, err := coordinator.Decide(context.Background(), WorkItem{ID: "wi-target", CaseID: "case-1", QueueID: "queue-1", Type: "legacy_workflow_action", Status: string(WorkItemOpen)}, CoordinationContext{ActionTypes: []string{"legacy_workflow_action"}, Complexity: 1, Actors: []CoordinationActor{{ID: "emp-1", Enabled: true, QueueMemberships: []string{"queue-1"}, AllowedActionTypes: []string{"legacy_workflow_action"}}}, Profiles: map[string]CoordinationActorProfile{"emp-1": {ActorID: "emp-1", MaxComplexity: 3, TrustLevel: "high", TrustAvailable: true}}})
	if err != nil {
		t.Fatalf("Decide error = %v", err)
	}
	if decision.DecisionType != CoordinationDefer {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestCoordinatorQueuePressureKeepsExecuteNowWhenBacklogBelowThreshold(t *testing.T) {
	t.Parallel()
	queueRepo := NewInMemoryQueueRepository()
	base := time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)
	if err := queueRepo.SaveWorkItem(context.Background(), WorkItem{ID: "wi-backlog-1", CaseID: "case-backlog", QueueID: "queue-1", Type: "legacy_workflow_action", Status: string(WorkItemOpen), CreatedAt: base, UpdatedAt: base}); err != nil {
		t.Fatalf("SaveWorkItem error = %v", err)
	}
	coordinator := NewCoordinationService(
		NewInMemoryCoordinationRepository(),
		queueRepo,
		nil,
		CoordinationConfig{QueueDepthThreshold: 3},
		NewQueuePressureScorer(CoordinationConfig{QueueDepthThreshold: 3}, queueRepo, nil),
		nil,
		fakeClock{now: base.Add(time.Hour)},
		&fakeIDGenerator{ids: []string{"coord-1"}},
	)
	decision, err := coordinator.Decide(context.Background(), WorkItem{ID: "wi-target", CaseID: "case-1", QueueID: "queue-1", Type: "legacy_workflow_action", Status: string(WorkItemOpen)}, CoordinationContext{ActionTypes: []string{"legacy_workflow_action"}, Complexity: 1, Actors: []CoordinationActor{{ID: "emp-1", Enabled: true, QueueMemberships: []string{"queue-1"}, AllowedActionTypes: []string{"legacy_workflow_action"}}}, Profiles: map[string]CoordinationActorProfile{"emp-1": {ActorID: "emp-1", MaxComplexity: 3, TrustLevel: "high", TrustAvailable: true}}})
	if err != nil {
		t.Fatalf("Decide error = %v", err)
	}
	if decision.DecisionType != CoordinationExecuteNow {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestCoordinatorDepartmentLoadSkipsWhenDepartmentIsEmpty(t *testing.T) {
	t.Parallel()
	queueRepo := NewInMemoryQueueRepository()
	base := time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)
	if err := queueRepo.SaveQueue(context.Background(), WorkQueue{ID: "queue-1", Department: ""}); err != nil {
		t.Fatalf("SaveQueue error = %v", err)
	}
	provider := NewInMemoryDepartmentLoadProvider()
	provider.SaveLoad(context.Background(), DepartmentLoad{DepartmentID: "operations", TotalActors: 1, BusyActors: 1, DepartmentExists: true})
	coordinator := NewCoordinationService(
		NewInMemoryCoordinationRepository(),
		queueRepo,
		nil,
		CoordinationConfig{QueueDepthThreshold: 3, DepartmentLoadSource: provider},
		NewQueuePressureScorer(CoordinationConfig{QueueDepthThreshold: 3, DepartmentLoadSource: provider}, queueRepo, provider),
		nil,
		fakeClock{now: base.Add(time.Hour)},
		&fakeIDGenerator{ids: []string{"coord-1"}},
	)
	decision, err := coordinator.Decide(context.Background(), WorkItem{ID: "wi-target", CaseID: "case-1", QueueID: "queue-1", Type: "legacy_workflow_action", Status: string(WorkItemOpen)}, CoordinationContext{ActionTypes: []string{"legacy_workflow_action"}, Complexity: 1, Actors: []CoordinationActor{{ID: "emp-1", Enabled: true, QueueMemberships: []string{"queue-1"}, AllowedActionTypes: []string{"legacy_workflow_action"}}}, Profiles: map[string]CoordinationActorProfile{"emp-1": {ActorID: "emp-1", MaxComplexity: 3, TrustLevel: "high", TrustAvailable: true}}})
	if err != nil {
		t.Fatalf("Decide error = %v", err)
	}
	if decision.DecisionType != CoordinationExecuteNow {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestCoordinatorDepartmentLoadDefersWhenAllActorsBusy(t *testing.T) {
	t.Parallel()
	queueRepo := NewInMemoryQueueRepository()
	base := time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)
	if err := queueRepo.SaveQueue(context.Background(), WorkQueue{ID: "queue-1", Department: "operations"}); err != nil {
		t.Fatalf("SaveQueue error = %v", err)
	}
	provider := NewInMemoryDepartmentLoadProvider()
	provider.SaveLoad(context.Background(), DepartmentLoad{DepartmentID: "operations", TotalActors: 2, BusyActors: 2, DepartmentExists: true})
	coordinator := NewCoordinationService(
		NewInMemoryCoordinationRepository(),
		queueRepo,
		nil,
		CoordinationConfig{QueueDepthThreshold: 3, DepartmentLoadSource: provider},
		NewQueuePressureScorer(CoordinationConfig{QueueDepthThreshold: 3, DepartmentLoadSource: provider}, queueRepo, provider),
		nil,
		fakeClock{now: base.Add(time.Hour)},
		&fakeIDGenerator{ids: []string{"coord-1"}},
	)
	decision, err := coordinator.Decide(context.Background(), WorkItem{ID: "wi-target", CaseID: "case-1", QueueID: "queue-1", Type: "legacy_workflow_action", Status: string(WorkItemOpen)}, CoordinationContext{ActionTypes: []string{"legacy_workflow_action"}, Complexity: 1, Actors: []CoordinationActor{{ID: "emp-1", Enabled: true, QueueMemberships: []string{"queue-1"}, AllowedActionTypes: []string{"legacy_workflow_action"}}}, Profiles: map[string]CoordinationActorProfile{"emp-1": {ActorID: "emp-1", MaxComplexity: 3, TrustLevel: "high", TrustAvailable: true}}})
	if err != nil {
		t.Fatalf("Decide error = %v", err)
	}
	if decision.DecisionType != CoordinationDefer {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestCoordinatorDepartmentLoadBlocksWhenDepartmentDoesNotExist(t *testing.T) {
	t.Parallel()
	queueRepo := NewInMemoryQueueRepository()
	base := time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)
	if err := queueRepo.SaveQueue(context.Background(), WorkQueue{ID: "queue-1", Department: "operations"}); err != nil {
		t.Fatalf("SaveQueue error = %v", err)
	}
	coordinator := NewCoordinationService(
		NewInMemoryCoordinationRepository(),
		queueRepo,
		nil,
		CoordinationConfig{QueueDepthThreshold: 3, DepartmentLoadSource: NewInMemoryDepartmentLoadProvider()},
		NewQueuePressureScorer(CoordinationConfig{QueueDepthThreshold: 3}, queueRepo, NewInMemoryDepartmentLoadProvider()),
		nil,
		fakeClock{now: base.Add(time.Hour)},
		&fakeIDGenerator{ids: []string{"coord-1"}},
	)
	decision, err := coordinator.Decide(context.Background(), WorkItem{ID: "wi-target", CaseID: "case-1", QueueID: "queue-1", Type: "legacy_workflow_action", Status: string(WorkItemOpen)}, CoordinationContext{ActionTypes: []string{"legacy_workflow_action"}, Complexity: 1, Actors: []CoordinationActor{{ID: "emp-1", Enabled: true, QueueMemberships: []string{"queue-1"}, AllowedActionTypes: []string{"legacy_workflow_action"}}}, Profiles: map[string]CoordinationActorProfile{"emp-1": {ActorID: "emp-1", MaxComplexity: 3, TrustLevel: "high", TrustAvailable: true}}})
	if err != nil {
		t.Fatalf("Decide error = %v", err)
	}
	if decision.DecisionType != CoordinationBlock {
		t.Fatalf("decision = %#v", decision)
	}
}
