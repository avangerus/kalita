package proposal

import (
	"context"
	"fmt"

	"kalita/internal/actionplan"
	"kalita/internal/employee"
	"kalita/internal/eventcore"
	"kalita/internal/workplan"
)

type proposalService struct {
	repo      Repository
	validator Validator
	compiler  CompilerAdapter
	log       eventcore.EventLog
	clock     eventcore.Clock
	ids       eventcore.IDGenerator
}

func NewService(repo Repository, validator Validator, compiler CompilerAdapter, log eventcore.EventLog, clock eventcore.Clock, ids eventcore.IDGenerator) Service {
	if clock == nil {
		clock = eventcore.RealClock{}
	}
	if ids == nil {
		ids = eventcore.NewULIDGenerator()
	}
	return &proposalService{repo: repo, validator: validator, compiler: compiler, log: log, clock: clock, ids: ids}
}

func (s *proposalService) CreateProposal(ctx context.Context, actor employee.DigitalEmployee, wi workplan.WorkItem, assignment employee.Assignment, payload map[string]any, justification string) (Proposal, error) {
	if s.repo == nil {
		return Proposal{}, fmt.Errorf("proposal repository is nil")
	}
	now := s.clock.Now()
	p := Proposal{ID: s.ids.NewID(), Type: ProposalTypeActionIntent, Status: ProposalDraft, ActorID: actor.ID, CaseID: wi.CaseID, WorkItemID: wi.ID, AssignmentID: assignment.ID, Payload: cloneMap(payload), Justification: justification, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.Save(ctx, p); err != nil {
		return Proposal{}, err
	}
	if err := s.appendEvent(ctx, p, "proposal_created", string(ProposalDraft), map[string]any{"proposal_id": p.ID, "work_item_id": p.WorkItemID, "actor_id": p.ActorID, "assignment_id": p.AssignmentID}); err != nil {
		return Proposal{}, err
	}
	return p, nil
}

func (s *proposalService) ValidateProposal(ctx context.Context, proposalID string, actor employee.DigitalEmployee) (Proposal, error) {
	if s.repo == nil {
		return Proposal{}, fmt.Errorf("proposal repository is nil")
	}
	if s.validator == nil {
		return Proposal{}, fmt.Errorf("proposal validator is nil")
	}
	p, ok, err := s.repo.Get(ctx, proposalID)
	if err != nil {
		return Proposal{}, err
	}
	if !ok {
		return Proposal{}, fmt.Errorf("proposal %s not found", proposalID)
	}
	status, reason, err := s.validator.Validate(ctx, p, actor)
	if err != nil {
		return Proposal{}, err
	}
	p.Status = status
	p.RejectionReason = reason
	p.UpdatedAt = s.clock.Now()
	if err := s.repo.Save(ctx, p); err != nil {
		return Proposal{}, err
	}
	step := "proposal_validated"
	if status == ProposalRejected {
		step = "proposal_rejected"
	}
	if err := s.appendEvent(ctx, p, step, string(status), map[string]any{"proposal_id": p.ID, "work_item_id": p.WorkItemID, "actor_id": p.ActorID, "rejection_reason": p.RejectionReason}); err != nil {
		return Proposal{}, err
	}
	return p, nil
}

func (s *proposalService) CompileProposal(ctx context.Context, proposalID string) (Proposal, actionplan.ActionPlan, error) {
	if s.repo == nil {
		return Proposal{}, actionplan.ActionPlan{}, fmt.Errorf("proposal repository is nil")
	}
	if s.compiler == nil {
		return Proposal{}, actionplan.ActionPlan{}, fmt.Errorf("proposal compiler adapter is nil")
	}
	p, ok, err := s.repo.Get(ctx, proposalID)
	if err != nil {
		return Proposal{}, actionplan.ActionPlan{}, err
	}
	if !ok {
		return Proposal{}, actionplan.ActionPlan{}, fmt.Errorf("proposal %s not found", proposalID)
	}
	if p.Status != ProposalValidated {
		return Proposal{}, actionplan.ActionPlan{}, fmt.Errorf("proposal %s must be validated before compilation", proposalID)
	}
	plan, err := s.compiler.CompileToActionPlan(ctx, p)
	if err != nil {
		return Proposal{}, actionplan.ActionPlan{}, err
	}
	p.ActionPlanID = plan.ID
	p.Status = ProposalCompiled
	p.RejectionReason = ""
	p.UpdatedAt = s.clock.Now()
	if err := s.repo.Save(ctx, p); err != nil {
		return Proposal{}, actionplan.ActionPlan{}, err
	}
	if err := s.appendEvent(ctx, p, "proposal_compiled", string(ProposalCompiled), map[string]any{"proposal_id": p.ID, "work_item_id": p.WorkItemID, "actor_id": p.ActorID, "action_plan_id": p.ActionPlanID}); err != nil {
		return Proposal{}, actionplan.ActionPlan{}, err
	}
	return p, plan, nil
}

func (s *proposalService) appendEvent(ctx context.Context, p Proposal, step, status string, payload map[string]any) error {
	if s.log == nil {
		return nil
	}
	meta := actionplanExecutionFromContext(ctx)
	return s.log.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{ID: s.ids.NewID(), ExecutionID: meta.ExecutionID, CaseID: p.CaseID, Step: step, Status: status, OccurredAt: s.clock.Now(), CorrelationID: meta.CorrelationID, CausationID: meta.CausationID, Payload: payload})
}

type ExecutionContext struct{ ExecutionID, CorrelationID, CausationID string }
type proposalExecutionContextKey struct{}

func ContextWithExecution(ctx context.Context, meta ExecutionContext) context.Context {
	return context.WithValue(ctx, proposalExecutionContextKey{}, meta)
}
func actionplanExecutionFromContext(ctx context.Context) ExecutionContext {
	meta, _ := ctx.Value(proposalExecutionContextKey{}).(ExecutionContext)
	return meta
}
