package workplan

import (
	"context"
	"fmt"
	"time"

	"kalita/internal/eventcore"
)

type Planner interface {
	EnsurePlanForWorkItem(ctx context.Context, queue WorkQueue, wi WorkItem, planDate string) (DailyPlan, bool, error)
}

type planningExecutionContextKey struct{}

type PlanningExecutionContext struct {
	ExecutionID   string
	CorrelationID string
	CausationID   string
}

func ContextWithPlanningExecution(ctx context.Context, meta PlanningExecutionContext) context.Context {
	return context.WithValue(ctx, planningExecutionContextKey{}, meta)
}

func planningExecutionFromContext(ctx context.Context) PlanningExecutionContext {
	meta, _ := ctx.Value(planningExecutionContextKey{}).(PlanningExecutionContext)
	return meta
}

type DefaultPlanner struct {
	repo  PlanRepository
	log   eventcore.EventLog
	clock eventcore.Clock
	ids   eventcore.IDGenerator
}

func NewPlanner(repo PlanRepository, log eventcore.EventLog, clock eventcore.Clock, ids eventcore.IDGenerator) *DefaultPlanner {
	if clock == nil {
		clock = eventcore.RealClock{}
	}
	if ids == nil {
		ids = eventcore.NewULIDGenerator()
	}
	return &DefaultPlanner{repo: repo, log: log, clock: clock, ids: ids}
}

func (p *DefaultPlanner) EnsurePlanForWorkItem(ctx context.Context, queue WorkQueue, wi WorkItem, planDate string) (DailyPlan, bool, error) {
	if p.repo == nil {
		return DailyPlan{}, false, fmt.Errorf("plan repository is nil")
	}
	plan, reused, err := p.repo.FindPlanByQueueAndDate(ctx, queue.ID, planDate)
	if err != nil {
		return DailyPlan{}, false, err
	}
	now := p.clock.Now()
	if !reused {
		plan = DailyPlan{
			ID:          p.ids.NewID(),
			QueueID:     queue.ID,
			PlanDate:    planDate,
			Status:      string(DailyPlanDraft),
			WorkItemIDs: []string{},
			Assignments: map[string][]string{},
			CreatedAt:   now,
		}
	}
	if !containsID(plan.WorkItemIDs, wi.ID) {
		plan.WorkItemIDs = append(plan.WorkItemIDs, wi.ID)
	}
	if err := p.repo.SavePlan(ctx, plan); err != nil {
		return DailyPlan{}, false, err
	}
	if p.log != nil {
		meta := planningExecutionFromContext(ctx)
		if err := p.log.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{
			ID:            p.ids.NewID(),
			ExecutionID:   meta.ExecutionID,
			CaseID:        wi.CaseID,
			Step:          "daily_plan_intake",
			Status:        "attached",
			OccurredAt:    now,
			CorrelationID: meta.CorrelationID,
			CausationID:   meta.CausationID,
			Payload: map[string]any{
				"case_id":       wi.CaseID,
				"queue_id":      queue.ID,
				"work_item_id":  wi.ID,
				"daily_plan_id": plan.ID,
				"plan_date":     planDate,
			},
		}); err != nil {
			return DailyPlan{}, false, err
		}
	}
	return plan, reused, nil
}

func PlanDateFromTime(now time.Time) string {
	return now.UTC().Format("2006-01-02")
}
