package actionplan

import (
	"context"
	"fmt"

	"kalita/internal/eventcore"
)

type actionPlanService struct {
	compiler  Compiler
	validator Validator
	log       eventcore.EventLog
	clock     eventcore.Clock
	ids       eventcore.IDGenerator
}

func NewService(compiler Compiler, validator Validator, log eventcore.EventLog, clock eventcore.Clock, ids eventcore.IDGenerator) Service {
	if clock == nil {
		clock = eventcore.RealClock{}
	}
	if ids == nil {
		ids = eventcore.NewULIDGenerator()
	}
	return &actionPlanService{compiler: compiler, validator: validator, log: log, clock: clock, ids: ids}
}

func (s *actionPlanService) CreatePlan(ctx context.Context, workItemID string, caseID string, input map[string]any) (ActionPlan, error) {
	if s.compiler == nil {
		return ActionPlan{}, fmt.Errorf("action plan compiler is nil")
	}
	if s.validator == nil {
		return ActionPlan{}, fmt.Errorf("action plan validator is nil")
	}
	plan, err := s.compiler.Compile(ctx, input)
	if err != nil {
		return ActionPlan{}, err
	}
	plan.WorkItemID = workItemID
	plan.CaseID = caseID
	if err := s.validator.Validate(plan); err != nil {
		return ActionPlan{}, err
	}
	if s.log != nil {
		meta := executionFromContext(ctx)
		now := s.clock.Now()
		if err := s.log.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{
			ID:            s.ids.NewID(),
			ExecutionID:   meta.ExecutionID,
			CaseID:        caseID,
			Step:          "action_plan_created",
			Status:        "ready",
			OccurredAt:    now,
			CorrelationID: meta.CorrelationID,
			CausationID:   meta.CausationID,
			Payload: map[string]any{
				"action_plan_id": plan.ID,
				"case_id":        caseID,
				"work_item_id":   workItemID,
				"reason":         plan.Reason,
				"action_count":   len(plan.Actions),
			},
		}); err != nil {
			return ActionPlan{}, err
		}
	}
	return plan, nil
}
