package workplan

import (
	"context"
	"fmt"

	"kalita/internal/eventcore"
)

const DefaultCoordinationStrategy = "default_queue_selection"

type CoordinationService struct {
	repo  CoordinationRepository
	log   eventcore.EventLog
	clock eventcore.Clock
	ids   eventcore.IDGenerator
}

func NewCoordinator(repo CoordinationRepository, log eventcore.EventLog, clock eventcore.Clock, ids eventcore.IDGenerator) *CoordinationService {
	if clock == nil {
		clock = eventcore.RealClock{}
	}
	if ids == nil {
		ids = eventcore.NewULIDGenerator()
	}
	return &CoordinationService{repo: repo, log: log, clock: clock, ids: ids}
}

func (s *CoordinationService) CoordinateWorkItem(ctx context.Context, wi WorkItem) (CoordinationDecision, error) {
	if s.repo == nil {
		return CoordinationDecision{}, fmt.Errorf("coordination repository is nil")
	}
	now := s.clock.Now()
	decision := CoordinationDecision{
		ID:         s.ids.NewID(),
		CaseID:     wi.CaseID,
		WorkItemID: wi.ID,
		QueueID:    wi.QueueID,
		Strategy:   DefaultCoordinationStrategy,
		SelectedBy: "system",
		Outcome:    CoordinationSelected,
		Reason:     fmt.Sprintf("strategy %s selected work item %s from queue %s", DefaultCoordinationStrategy, wi.ID, wi.QueueID),
		CreatedAt:  now,
	}
	if err := s.repo.SaveDecision(ctx, decision); err != nil {
		return CoordinationDecision{}, err
	}
	if s.log != nil {
		meta := planningExecutionFromContext(ctx)
		if err := s.log.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{
			ID:            s.ids.NewID(),
			ExecutionID:   meta.ExecutionID,
			CaseID:        wi.CaseID,
			Step:          "coordination_decision",
			Status:        string(decision.Outcome),
			OccurredAt:    now,
			CorrelationID: meta.CorrelationID,
			CausationID:   meta.CausationID,
			Payload: map[string]any{
				"case_id":                  wi.CaseID,
				"queue_id":                 wi.QueueID,
				"work_item_id":             wi.ID,
				"coordination_decision_id": decision.ID,
				"strategy":                 decision.Strategy,
			},
		}); err != nil {
			return CoordinationDecision{}, err
		}
	}
	return decision, nil
}
