package executioncontrol

import (
	"context"
	"fmt"

	"kalita/internal/eventcore"
	"kalita/internal/policy"
	"kalita/internal/workplan"
)

type executionContextKey struct{}

type ExecutionContext struct {
	ExecutionID   string
	CorrelationID string
	CausationID   string
}

func ContextWithExecution(ctx context.Context, meta ExecutionContext) context.Context {
	return context.WithValue(ctx, executionContextKey{}, meta)
}

func executionFromContext(ctx context.Context) ExecutionContext {
	meta, _ := ctx.Value(executionContextKey{}).(ExecutionContext)
	return meta
}

type Service struct {
	repo    ConstraintsRepository
	planner ConstraintsPlanner
	log     eventcore.EventLog
	clock   eventcore.Clock
	ids     eventcore.IDGenerator
}

func NewService(repo ConstraintsRepository, planner ConstraintsPlanner, log eventcore.EventLog, clock eventcore.Clock, ids eventcore.IDGenerator) *Service {
	if clock == nil {
		clock = eventcore.RealClock{}
	}
	if ids == nil {
		ids = eventcore.NewULIDGenerator()
	}
	return &Service{repo: repo, planner: planner, log: log, clock: clock, ids: ids}
}

func (s *Service) CreateAndRecord(ctx context.Context, coordination workplan.CoordinationDecision, policyDecision policy.PolicyDecision) (ExecutionConstraints, error) {
	if s.repo == nil {
		return ExecutionConstraints{}, fmt.Errorf("constraints repository is nil")
	}
	if s.planner == nil {
		return ExecutionConstraints{}, fmt.Errorf("constraints planner is nil")
	}
	planned, err := s.planner.PlanForPolicyDecision(ctx, coordination, policyDecision)
	if err != nil {
		return ExecutionConstraints{}, err
	}
	now := s.clock.Now()
	planned.ID = s.ids.NewID()
	planned.CreatedAt = now
	if err := s.repo.Save(ctx, planned); err != nil {
		return ExecutionConstraints{}, err
	}
	if s.log != nil {
		meta := executionFromContext(ctx)
		if err := s.log.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{
			ID:            s.ids.NewID(),
			ExecutionID:   meta.ExecutionID,
			CaseID:        coordination.CaseID,
			Step:          "execution_constraints_created",
			Status:        "ready",
			OccurredAt:    now,
			CorrelationID: meta.CorrelationID,
			CausationID:   meta.CausationID,
			Payload: map[string]any{
				"coordination_decision_id": coordination.ID,
				"policy_decision_id":       policyDecision.ID,
				"case_id":                  coordination.CaseID,
				"work_item_id":             coordination.WorkItemID,
				"queue_id":                 coordination.QueueID,
				"execution_constraints_id": planned.ID,
				"risk_level":               planned.RiskLevel,
				"execution_mode":           planned.ExecutionMode,
			},
		}); err != nil {
			return ExecutionConstraints{}, err
		}
	}
	return planned, nil
}
