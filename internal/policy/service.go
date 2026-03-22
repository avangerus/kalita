package policy

import (
	"context"
	"fmt"
	"time"

	"kalita/internal/eventcore"
	"kalita/internal/workplan"
)

type policyExecutionContextKey struct{}

type ExecutionContext struct {
	ExecutionID   string
	CorrelationID string
	CausationID   string
}

func ContextWithExecution(ctx context.Context, meta ExecutionContext) context.Context {
	return context.WithValue(ctx, policyExecutionContextKey{}, meta)
}

func executionFromContext(ctx context.Context) ExecutionContext {
	meta, _ := ctx.Value(policyExecutionContextKey{}).(ExecutionContext)
	return meta
}

type PolicyService struct {
	repo      PolicyRepository
	evaluator Evaluator
	log       eventcore.EventLog
	clock     eventcore.Clock
	ids       eventcore.IDGenerator
}

func NewService(repo PolicyRepository, evaluator Evaluator, log eventcore.EventLog, clock eventcore.Clock, ids eventcore.IDGenerator) *PolicyService {
	if clock == nil {
		clock = eventcore.RealClock{}
	}
	if ids == nil {
		ids = eventcore.NewULIDGenerator()
	}
	return &PolicyService{repo: repo, evaluator: evaluator, log: log, clock: clock, ids: ids}
}

func (s *PolicyService) EvaluateAndRecord(ctx context.Context, d workplan.CoordinationDecision) (PolicyDecision, *ApprovalRequest, error) {
	if s.repo == nil {
		return PolicyDecision{}, nil, fmt.Errorf("policy repository is nil")
	}
	if s.evaluator == nil {
		return PolicyDecision{}, nil, fmt.Errorf("policy evaluator is nil")
	}
	outcome, reason, err := s.evaluator.EvaluateCoordinationDecision(ctx, d)
	if err != nil {
		return PolicyDecision{}, nil, err
	}
	if reason == "" {
		reason = fmt.Sprintf("policy outcome %s", outcome)
	}
	now := s.clock.Now()
	decision := PolicyDecision{
		ID:                     s.ids.NewID(),
		CoordinationDecisionID: d.ID,
		CaseID:                 d.CaseID,
		WorkItemID:             d.WorkItemID,
		QueueID:                d.QueueID,
		Outcome:                outcome,
		Reason:                 reason,
		CreatedAt:              now,
	}
	if err := s.repo.SaveDecision(ctx, decision); err != nil {
		return PolicyDecision{}, nil, err
	}
	if err := s.appendEvent(ctx, d, decision, "policy_evaluation", string(outcome), map[string]any{
		"coordination_decision_id": d.ID,
		"case_id":                  d.CaseID,
		"work_item_id":             d.WorkItemID,
		"queue_id":                 d.QueueID,
		"policy_decision_id":       decision.ID,
	}, now); err != nil {
		return PolicyDecision{}, nil, err
	}
	if outcome != PolicyRequireApproval {
		return decision, nil, nil
	}
	approval := &ApprovalRequest{
		ID:                     s.ids.NewID(),
		CoordinationDecisionID: d.ID,
		PolicyDecisionID:       decision.ID,
		CaseID:                 d.CaseID,
		WorkItemID:             d.WorkItemID,
		QueueID:                d.QueueID,
		Status:                 ApprovalPending,
		RequestedFromRole:      "manager",
		CreatedAt:              now,
	}
	if err := s.repo.SaveApprovalRequest(ctx, *approval); err != nil {
		return PolicyDecision{}, nil, err
	}
	if err := s.appendEvent(ctx, d, decision, "approval_request_created", string(ApprovalPending), map[string]any{
		"coordination_decision_id": d.ID,
		"case_id":                  d.CaseID,
		"work_item_id":             d.WorkItemID,
		"queue_id":                 d.QueueID,
		"policy_decision_id":       decision.ID,
		"approval_request_id":      approval.ID,
	}, now); err != nil {
		return PolicyDecision{}, nil, err
	}
	return decision, approval, nil
}

func (s *PolicyService) appendEvent(ctx context.Context, d workplan.CoordinationDecision, decision PolicyDecision, step, status string, payload map[string]any, now time.Time) error {
	if s.log == nil {
		return nil
	}
	meta := executionFromContext(ctx)
	return s.log.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{
		ID:            s.ids.NewID(),
		ExecutionID:   meta.ExecutionID,
		CaseID:        d.CaseID,
		Step:          step,
		Status:        status,
		OccurredAt:    now,
		CorrelationID: meta.CorrelationID,
		CausationID:   meta.CausationID,
		Payload:       payload,
	})
}
