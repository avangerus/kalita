package employee

import (
	"context"
	"testing"
	"time"

	"kalita/internal/actionplan"
	"kalita/internal/eventcore"
	"kalita/internal/executioncontrol"
	"kalita/internal/executionruntime"
	"kalita/internal/trust"
	"kalita/internal/workplan"
)

type staticExecutionRuntime struct {
	session executionruntime.ExecutionSession
	err     error
	calls   int
	last    executioncontrol.ExecutionConstraints
}

func (s *staticExecutionRuntime) StartExecution(_ context.Context, _ actionplan.ActionPlan, constraints executioncontrol.ExecutionConstraints, _ executionruntime.RunMetadata) (executionruntime.ExecutionSession, error) {
	s.calls++
	s.last = constraints
	return s.session, s.err
}

func TestEmployeeServiceAssignsEmitsEventAndStartsExecution(t *testing.T) {
	t.Parallel()
	directory := NewInMemoryDirectory()
	_ = directory.SaveEmployee(context.Background(), DigitalEmployee{ID: "emp-1", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}})
	assignments := NewInMemoryAssignmentRepository()
	eventLog := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 19, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"assignment-1", "event-1", "event-2"}}
	runtimeSvc := &staticExecutionRuntime{session: executionruntime.ExecutionSession{ID: "session-1", Status: executionruntime.ExecutionSessionSucceeded}}
	service := NewService(assignments, NewSelector(directory), runtimeSvc, eventLog, clock, ids)
	ctx := executionruntime.ContextWithExecution(context.Background(), executionruntime.ExecutionContext{ExecutionID: "exec-1", CorrelationID: "corr-1", CausationID: "cmd-1"})
	assignment, session, err := service.AssignAndStartExecution(ctx, workplan.WorkItem{ID: "work-1", QueueID: "q-1"}, actionplan.ActionPlan{ID: "plan-1", Actions: []actionplan.Action{{ID: "a-1", Type: "legacy_workflow_action"}}}, executioncontrol.ExecutionConstraints{ID: "constraints-1"}, RunMetadata{CaseID: "case-1", QueueID: "q-1", CoordinationDecisionID: "coord-1", PolicyDecisionID: "policy-1"})
	if err != nil {
		t.Fatalf("AssignAndStartExecution error = %v", err)
	}
	if assignment.ID != "assignment-1" || session.ID != "session-1" || runtimeSvc.calls != 1 {
		t.Fatalf("assignment=%#v session=%#v calls=%d", assignment, session, runtimeSvc.calls)
	}
	stored, ok, err := assignments.GetAssignment(context.Background(), "assignment-1")
	if err != nil || !ok || stored.EmployeeID != "emp-1" {
		t.Fatalf("GetAssignment = %#v ok=%v err=%v", stored, ok, err)
	}
	_, execEvents, err := eventLog.ListByCorrelation(context.Background(), "corr-1")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(execEvents) != 2 || execEvents[0].Step != "employee_assigned" || execEvents[0].Status != "assigned" || execEvents[1].Step != "execution_mode_adjusted_by_trust" {
		t.Fatalf("execEvents = %#v", execEvents)
	}
	if execEvents[0].Payload["assignment_id"] != "assignment-1" {
		t.Fatalf("payload = %#v", execEvents[0].Payload)
	}
	if runtimeSvc.last.ExecutionMode != executioncontrol.ExecutionModeApprovalEachStep || runtimeSvc.last.MaxSteps != 1 {
		t.Fatalf("runtime constraints = %#v", runtimeSvc.last)
	}
}

func TestEmployeeServiceFailsWhenNoEligibleEmployeeExists(t *testing.T) {
	t.Parallel()
	service := NewService(NewInMemoryAssignmentRepository(), NewSelector(NewInMemoryDirectory()), &staticExecutionRuntime{}, nil, fakeClock{now: time.Now().UTC()}, &fakeIDGenerator{})
	_, _, err := service.AssignAndStartExecution(context.Background(), workplan.WorkItem{ID: "work-1", QueueID: "q-1"}, actionplan.ActionPlan{Actions: []actionplan.Action{{ID: "a-1", Type: "legacy_workflow_action"}}}, executioncontrol.ExecutionConstraints{ID: "constraints-1"}, RunMetadata{CaseID: "case-1", QueueID: "q-1"})
	if err == nil {
		t.Fatal("expected error when no eligible employee exists")
	}
}

func TestEmployeeServiceKeepsLowTrustActorSelectableButConstrained(t *testing.T) {
	t.Parallel()
	directory := NewInMemoryDirectory()
	_ = directory.SaveEmployee(context.Background(), DigitalEmployee{ID: "emp-1", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}})
	assignments := NewInMemoryAssignmentRepository()
	runtimeSvc := &staticExecutionRuntime{session: executionruntime.ExecutionSession{ID: "session-1"}}
	trustRepo := trust.NewInMemoryRepository()
	_ = trustRepo.Save(context.Background(), trust.TrustProfile{ActorID: "emp-1", TrustLevel: trust.TrustLow})
	service := NewService(assignments, NewSelector(directory), runtimeSvc, eventcore.NewInMemoryEventLog(), fakeClock{now: time.Date(2026, 3, 22, 19, 0, 0, 0, time.UTC)}, &fakeIDGenerator{ids: []string{"assignment-1", "event-1", "event-2"}}, trust.NewService(trustRepo, trust.NewScorerWithClock(nil)))

	_, _, err := service.AssignAndStartExecution(context.Background(), workplan.WorkItem{ID: "work-1", QueueID: "q-1"}, actionplan.ActionPlan{ID: "plan-1", Actions: []actionplan.Action{{ID: "a-1", Type: "legacy_workflow_action"}}}, executioncontrol.ExecutionConstraints{ID: "constraints-1", ExecutionMode: executioncontrol.ExecutionModeDeterministicAPI, MaxSteps: 10, MaxDurationSec: 300}, RunMetadata{CaseID: "case-1", QueueID: "q-1"})
	if err != nil {
		t.Fatalf("AssignAndStartExecution error = %v", err)
	}
	if runtimeSvc.last.ExecutionMode != executioncontrol.ExecutionModeApprovalEachStep || runtimeSvc.last.MaxSteps != 1 || runtimeSvc.last.MaxDurationSec != 60 {
		t.Fatalf("runtime constraints = %#v", runtimeSvc.last)
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
		return ""
	}
	id := f.ids[f.i]
	f.i++
	return id
}
