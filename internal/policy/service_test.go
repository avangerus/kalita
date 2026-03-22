package policy

import (
	"context"
	"testing"
	"time"

	"kalita/internal/eventcore"
	"kalita/internal/workplan"
)

type fakeClock struct{ now time.Time }

func (f fakeClock) Now() time.Time { return f.now }

type fakeIDGenerator struct {
	ids []string
	i   int
}

func (f *fakeIDGenerator) NewID() string { id := f.ids[f.i]; f.i++; return id }

func TestPolicyServiceAllowCreatesDecisionAndEvent(t *testing.T) {
	repo := NewInMemoryRepository()
	log := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 16, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"policy-1", "event-1"}}
	service := NewService(repo, NewEvaluator(), log, clock, ids)
	ctx := ContextWithExecution(context.Background(), ExecutionContext{ExecutionID: "exec-1", CorrelationID: "corr-1", CausationID: "cmd-1"})
	decision, approval, err := service.EvaluateAndRecord(ctx, workplan.CoordinationDecision{ID: "coord-1", CaseID: "case-1", WorkItemID: "wi-1", QueueID: "queue-1", Outcome: workplan.CoordinationSelected, Strategy: workplan.DefaultCoordinationStrategy})
	if err != nil {
		t.Fatalf("EvaluateAndRecord error = %v", err)
	}
	if decision.Outcome != PolicyAllow || approval != nil {
		t.Fatalf("decision=%#v approval=%#v", decision, approval)
	}
	stored, err := repo.ListByCoordinationDecision(context.Background(), "coord-1")
	if err != nil || len(stored) != 1 {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	_, events, err := log.ListByCorrelation(context.Background(), "corr-1")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(events) != 1 || events[0].Step != "policy_evaluation" || events[0].Status != string(PolicyAllow) {
		t.Fatalf("events = %#v", events)
	}
}

func TestPolicyServiceRequireApprovalCreatesDecisionApprovalAndEvents(t *testing.T) {
	repo := NewInMemoryRepository()
	log := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 16, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"policy-1", "event-1", "approval-1", "event-2"}}
	service := NewService(repo, NewEvaluator(), log, clock, ids)
	ctx := ContextWithExecution(context.Background(), ExecutionContext{ExecutionID: "exec-1", CorrelationID: "corr-1", CausationID: "cmd-1"})
	decision, approval, err := service.EvaluateAndRecord(ctx, workplan.CoordinationDecision{ID: "coord-1", CaseID: "case-1", WorkItemID: "wi-1", QueueID: "queue-1", Outcome: workplan.CoordinationSelected, Strategy: "requires_manager_approval"})
	if err != nil {
		t.Fatalf("EvaluateAndRecord error = %v", err)
	}
	if decision.Outcome != PolicyRequireApproval || approval == nil || approval.Status != ApprovalPending {
		t.Fatalf("decision=%#v approval=%#v", decision, approval)
	}
	approvals, err := repo.ListApprovalRequestsByCoordinationDecision(context.Background(), "coord-1")
	if err != nil || len(approvals) != 1 {
		t.Fatalf("approvals=%#v err=%v", approvals, err)
	}
	_, events, err := log.ListByCorrelation(context.Background(), "corr-1")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(events) != 2 || events[0].Step != "policy_evaluation" || events[1].Step != "approval_request_created" || events[1].Status != string(ApprovalPending) {
		t.Fatalf("events = %#v", events)
	}
}

func TestPolicyServiceDenyCreatesDecisionAndEventWithoutApproval(t *testing.T) {
	repo := NewInMemoryRepository()
	log := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 16, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"policy-1", "event-1"}}
	service := NewService(repo, NewEvaluator(), log, clock, ids)
	ctx := ContextWithExecution(context.Background(), ExecutionContext{ExecutionID: "exec-1", CorrelationID: "corr-1", CausationID: "cmd-1"})
	decision, approval, err := service.EvaluateAndRecord(ctx, workplan.CoordinationDecision{ID: "coord-1", CaseID: "case-1", WorkItemID: "wi-1", QueueID: "queue-1", Outcome: workplan.CoordinationSelected, Strategy: "blocked_strategy"})
	if err != nil {
		t.Fatalf("EvaluateAndRecord error = %v", err)
	}
	if decision.Outcome != PolicyDeny || approval != nil {
		t.Fatalf("decision=%#v approval=%#v", decision, approval)
	}
	approvals, err := repo.ListApprovalRequestsByCoordinationDecision(context.Background(), "coord-1")
	if err != nil {
		t.Fatalf("ListApprovalRequestsByCoordinationDecision error = %v", err)
	}
	if len(approvals) != 0 {
		t.Fatalf("approvals = %#v", approvals)
	}
	_, events, err := log.ListByCorrelation(context.Background(), "corr-1")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(events) != 1 || events[0].Step != "policy_evaluation" || events[0].Status != string(PolicyDeny) {
		t.Fatalf("events = %#v", events)
	}
}
