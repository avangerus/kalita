package proposal

import (
	"context"
	"testing"
	"time"

	"kalita/internal/actionplan"
	"kalita/internal/employee"
	"kalita/internal/eventcore"
	"kalita/internal/workplan"
)

type fakeClock struct{ now time.Time }

func (f fakeClock) Now() time.Time { return f.now }

type fakeIDs struct {
	ids []string
	i   int
}

func (f *fakeIDs) NewID() string {
	if f.i >= len(f.ids) {
		return ""
	}
	id := f.ids[f.i]
	f.i++
	return id
}

type staticCompiler struct {
	plan  actionplan.ActionPlan
	err   error
	calls int
}

func (s *staticCompiler) CompileToActionPlan(context.Context, Proposal) (actionplan.ActionPlan, error) {
	s.calls++
	return s.plan, s.err
}

func TestServiceCreateProposalEmitsEvent(t *testing.T) {
	log := eventcore.NewInMemoryEventLog()
	svc := NewService(NewInMemoryRepository(), NewValidator(), &staticCompiler{}, log, fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, &fakeIDs{ids: []string{"proposal-1", "event-1"}})
	ctx := ContextWithExecution(context.Background(), ExecutionContext{ExecutionID: "exec-1", CorrelationID: "corr-1", CausationID: "cmd-1"})
	proposal, err := svc.CreateProposal(ctx, employee.DigitalEmployee{ID: "emp-1"}, workplan.WorkItem{ID: "work-1", CaseID: "case-1"}, employee.Assignment{ID: "assignment-1"}, map[string]any{"actions": []any{map[string]any{"type": "legacy_workflow_action"}}}, "because")
	if err != nil || proposal.Status != ProposalDraft {
		t.Fatalf("proposal=%#v err=%v", proposal, err)
	}
	_, events, _ := log.ListByCorrelation(context.Background(), "corr-1")
	if len(events) != 1 || events[0].Step != "proposal_created" {
		t.Fatalf("events=%#v", events)
	}
}

func TestServiceValidateProposalUpdatesStatus(t *testing.T) {
	repo := NewInMemoryRepository()
	_ = repo.Save(context.Background(), Proposal{ID: "proposal-1", Type: ProposalTypeActionIntent, Status: ProposalDraft, ActorID: "emp-1", CaseID: "case-1", WorkItemID: "work-1", Payload: map[string]any{"actions": []any{map[string]any{"type": "legacy_workflow_action"}}}, Justification: "because"})
	svc := NewService(repo, NewValidator(), &staticCompiler{}, eventcore.NewInMemoryEventLog(), fakeClock{now: time.Now().UTC()}, &fakeIDs{ids: []string{"event-1"}})
	proposal, err := svc.ValidateProposal(context.Background(), "proposal-1", employee.DigitalEmployee{ID: "emp-1", AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}})
	if err != nil || proposal.Status != ProposalValidated {
		t.Fatalf("proposal=%#v err=%v", proposal, err)
	}
}

func TestRejectedProposalCannotCompile(t *testing.T) {
	repo := NewInMemoryRepository()
	_ = repo.Save(context.Background(), Proposal{ID: "proposal-1", Type: ProposalTypeActionIntent, Status: ProposalRejected})
	svc := NewService(repo, NewValidator(), &staticCompiler{}, nil, fakeClock{now: time.Now().UTC()}, &fakeIDs{})
	_, _, err := svc.CompileProposal(context.Background(), "proposal-1")
	if err == nil {
		t.Fatal("expected compile error")
	}
}

func TestValidatedProposalCompilesToActionPlanAndUpdatesProposal(t *testing.T) {
	repo := NewInMemoryRepository()
	_ = repo.Save(context.Background(), Proposal{ID: "proposal-1", Type: ProposalTypeActionIntent, Status: ProposalValidated, ActorID: "emp-1", CaseID: "case-1", WorkItemID: "work-1", Payload: map[string]any{"actions": []any{map[string]any{"type": "legacy_workflow_action"}}}, Justification: "because"})
	compiler := &staticCompiler{plan: actionplan.ActionPlan{ID: "plan-1"}}
	svc := NewService(repo, NewValidator(), compiler, eventcore.NewInMemoryEventLog(), fakeClock{now: time.Now().UTC()}, &fakeIDs{ids: []string{"event-1"}})
	proposal, plan, err := svc.CompileProposal(context.Background(), "proposal-1")
	if err != nil || plan.ID != "plan-1" || proposal.ActionPlanID != "plan-1" || proposal.Status != ProposalCompiled || compiler.calls != 1 {
		t.Fatalf("proposal=%#v plan=%#v calls=%d err=%v", proposal, plan, compiler.calls, err)
	}
}
