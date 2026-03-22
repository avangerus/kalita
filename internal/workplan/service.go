package workplan

import (
	"context"
	"fmt"
	"strings"
	"time"

	"kalita/internal/caseruntime"
	"kalita/internal/eventcore"
)

type Service struct {
	repo        QueueRepository
	router      AssignmentRouter
	planner     Planner
	coordinator Coordinator
	log         eventcore.EventLog
	clock       eventcore.Clock
	ids         eventcore.IDGenerator
	planDate    func(time.Time) string
}

type IntakeResult struct {
	Command              eventcore.Command
	Case                 caseruntime.Case
	Queue                WorkQueue
	WorkItem             WorkItem
	CoordinationDecision CoordinationDecision
	ExecEvent            eventcore.ExecutionEvent
}

func NewService(repo QueueRepository, router AssignmentRouter, planner Planner, coordinator Coordinator, log eventcore.EventLog, clock eventcore.Clock, ids eventcore.IDGenerator) *Service {
	if clock == nil {
		clock = eventcore.RealClock{}
	}
	if ids == nil {
		ids = eventcore.NewULIDGenerator()
	}
	return &Service{repo: repo, router: router, planner: planner, coordinator: coordinator, log: log, clock: clock, ids: ids, planDate: PlanDateFromTime}
}

func (s *Service) IntakeCommand(ctx context.Context, resolved caseruntime.ResolutionResult) (IntakeResult, error) {
	if s.repo == nil {
		return IntakeResult{}, fmt.Errorf("queue repository is nil")
	}
	if s.router == nil {
		return IntakeResult{}, fmt.Errorf("assignment router is nil")
	}
	if s.planner == nil {
		return IntakeResult{}, fmt.Errorf("planner is nil")
	}
	if s.coordinator == nil {
		return IntakeResult{}, fmt.Errorf("coordinator is nil")
	}
	queue, err := s.router.RouteCase(ctx, resolved.Case)
	if err != nil {
		return IntakeResult{}, err
	}
	now := s.clock.Now()
	workItem := WorkItem{
		ID:        s.ids.NewID(),
		CaseID:    resolved.Case.ID,
		QueueID:   queue.ID,
		Type:      workItemTypeForCommand(resolved.Command),
		Status:    string(WorkItemOpen),
		Reason:    workItemReason(resolved.Case, resolved.Command),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.SaveWorkItem(ctx, workItem); err != nil {
		return IntakeResult{}, err
	}
	execEvent := eventcore.ExecutionEvent{
		ID:            s.ids.NewID(),
		ExecutionID:   resolved.Command.ExecutionID,
		CaseID:        resolved.Case.ID,
		Step:          "work_item_intake",
		Status:        "created",
		OccurredAt:    now,
		CorrelationID: resolved.Command.CorrelationID,
		CausationID:   resolved.Command.ID,
		Payload: map[string]any{
			"case_id":      resolved.Case.ID,
			"queue_id":     queue.ID,
			"work_item_id": workItem.ID,
		},
	}
	if s.log != nil {
		if err := s.log.AppendExecutionEvent(ctx, execEvent); err != nil {
			return IntakeResult{}, err
		}
	}
	planDate := s.planDate(now)
	planningCtx := ContextWithPlanningExecution(ctx, PlanningExecutionContext{
		ExecutionID:   resolved.Command.ExecutionID,
		CorrelationID: resolved.Command.CorrelationID,
		CausationID:   resolved.Command.ID,
	})
	plan, _, err := s.planner.EnsurePlanForWorkItem(planningCtx, queue, workItem, planDate)
	if err != nil {
		return IntakeResult{}, err
	}
	workItem.PlanID = plan.ID
	workItem.UpdatedAt = now
	if err := s.repo.SaveWorkItem(ctx, workItem); err != nil {
		return IntakeResult{}, err
	}
	coordinationCtx := ContextWithPlanningExecution(ctx, PlanningExecutionContext{
		ExecutionID:   resolved.Command.ExecutionID,
		CorrelationID: resolved.Command.CorrelationID,
		CausationID:   resolved.Command.ID,
	})
	decision, err := s.coordinator.CoordinateWorkItem(coordinationCtx, workItem)
	if err != nil {
		return IntakeResult{}, err
	}
	return IntakeResult{Command: resolved.Command, Case: resolved.Case, Queue: queue, WorkItem: workItem, CoordinationDecision: decision, ExecEvent: execEvent}, nil
}

func workItemTypeForCommand(cmd eventcore.Command) string {
	if strings.TrimSpace(cmd.Type) != "" {
		return cmd.Type
	}
	return "generic"
}

func workItemReason(c caseruntime.Case, cmd eventcore.Command) string {
	if cmd.TargetRef != "" {
		return fmt.Sprintf("intake %s for %s", c.Kind, cmd.TargetRef)
	}
	return fmt.Sprintf("intake %s", c.Kind)
}
