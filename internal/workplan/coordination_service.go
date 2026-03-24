package workplan

import (
	"context"
	"fmt"
	"strings"

	"kalita/internal/eventcore"
)

type CoordinationConfig struct {
	QueueDepthThreshold  int
	DepartmentLoadSource DepartmentLoadProvider
}

func defaultCoordinationConfig() CoordinationConfig {
	return CoordinationConfig{QueueDepthThreshold: 10}
}

type DefaultCoordinator struct {
	repo                CoordinationRepository
	queueRepo           QueueRepository
	snapshot            WorkQueueSnapshot
	queuePressureScorer QueuePressureScorer
	departmentLoad      DepartmentLoadProvider
	config              CoordinationConfig
	log                 eventcore.EventLog
	clock               eventcore.Clock
	ids                 eventcore.IDGenerator
}

func NewCoordinator(repo CoordinationRepository, log eventcore.EventLog, clock eventcore.Clock, ids eventcore.IDGenerator) *DefaultCoordinator {
	return NewCoordinationService(repo, nil, nil, defaultCoordinationConfig(), nil, log, clock, ids)
}

func NewCoordinationService(
	repo CoordinationRepository,
	queueRepo QueueRepository,
	snapshot WorkQueueSnapshot,
	config CoordinationConfig,
	queuePressureScorer QueuePressureScorer,
	log eventcore.EventLog,
	clock eventcore.Clock,
	ids eventcore.IDGenerator,
) *DefaultCoordinator {
	if clock == nil {
		clock = eventcore.RealClock{}
	}
	if ids == nil {
		ids = eventcore.NewULIDGenerator()
	}
	if config.QueueDepthThreshold <= 0 {
		config = defaultCoordinationConfig()
	}
	if queuePressureScorer == nil {
		queuePressureScorer = NewQueuePressureScorer(config, queueRepo, config.DepartmentLoadSource)
	}
	return &DefaultCoordinator{
		repo:                repo,
		queueRepo:           queueRepo,
		snapshot:            snapshot,
		queuePressureScorer: queuePressureScorer,
		departmentLoad:      config.DepartmentLoadSource,
		config:              config,
		log:                 log,
		clock:               clock,
		ids:                 ids,
	}
}

func (s *DefaultCoordinator) Decide(ctx context.Context, wi WorkItem, coordinationContext CoordinationContext) (CoordinationDecision, error) {
	if s.repo == nil {
		return CoordinationDecision{}, fmt.Errorf("coordination repository is nil")
	}
	if s.snapshot != nil {
		if err := s.snapshot.Refresh(ctx); err != nil {
			return CoordinationDecision{}, err
		}
	}
	now := s.clock.Now()
	decisionType, reason := s.evaluate(wi, coordinationContext)

	if decisionType == CoordinationExecuteNow {
		departmentDecision, departmentReason, err := s.evaluateDepartmentLoad(ctx, wi)
		if err != nil {
			return CoordinationDecision{}, err
		}
		if departmentDecision != CoordinationExecuteNow {
			decisionType = departmentDecision
			reason = departmentReason
		}
	}

	if decisionType == CoordinationExecuteNow {
		queueLen, err := s.queueLenByCapability(ctx, wi.Type)
		if err != nil {
			return CoordinationDecision{}, err
		}
		if queueLen > s.config.QueueDepthThreshold {
			pressure := s.queuePressureScorer.Score(ctx, wi.Type, queueLen)
			executeNowScore := 1.0 - pressure
			deferScore := 0.5 + pressure
			if deferScore > executeNowScore {
				decisionType = CoordinationDefer
				reason = fmt.Sprintf("queue pressure %.2f for capability %q (len=%d threshold=%d) favors defer", pressure, wi.Type, queueLen, s.config.QueueDepthThreshold)
			}
		}
	}

	decision := CoordinationDecision{ID: s.ids.NewID(), WorkItemID: wi.ID, CaseID: wi.CaseID, QueueID: wi.QueueID, DecisionType: decisionType, Priority: coordinationPriority(decisionType), Reason: reason, CreatedAt: now}
	if err := s.repo.SaveDecision(ctx, decision); err != nil {
		return CoordinationDecision{}, err
	}
	if s.log != nil {
		meta := planningExecutionFromContext(ctx)
		if err := s.log.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{ID: s.ids.NewID(), ExecutionID: meta.ExecutionID, CaseID: wi.CaseID, Step: "coordination_decision_made", Status: string(decision.DecisionType), OccurredAt: now, CorrelationID: meta.CorrelationID, CausationID: meta.CausationID, Payload: map[string]any{"case_id": wi.CaseID, "queue_id": wi.QueueID, "work_item_id": wi.ID, "coordination_decision_id": decision.ID, "decision_type": decision.DecisionType, "reason": decision.Reason, "priority": decision.Priority}}); err != nil {
			return CoordinationDecision{}, err
		}
	}
	return decision, nil
}

func (s *DefaultCoordinator) evaluateDepartmentLoad(ctx context.Context, wi WorkItem) (CoordinationDecisionType, string, error) {
	if s.queueRepo == nil || s.departmentLoad == nil {
		return CoordinationExecuteNow, "", nil
	}
	queue, ok, err := s.queueRepo.GetQueue(ctx, wi.QueueID)
	if err != nil {
		return CoordinationBlock, "", err
	}
	if !ok {
		return CoordinationBlock, fmt.Sprintf("queue %s not found for department-level coordination", wi.QueueID), nil
	}
	departmentID := strings.TrimSpace(queue.Department)
	if departmentID == "" {
		return CoordinationExecuteNow, "", nil
	}
	load, err := s.departmentLoad.GetLoad(ctx, departmentID)
	if err != nil {
		return CoordinationBlock, "", err
	}
	if !load.DepartmentExists {
		return CoordinationBlock, fmt.Sprintf("department %s does not exist", departmentID), nil
	}
	if load.TotalActors > 0 && load.BusyActors >= load.TotalActors {
		return CoordinationDefer, fmt.Sprintf("all actors busy in department %s (%d/%d)", departmentID, load.BusyActors, load.TotalActors), nil
	}
	return CoordinationExecuteNow, "", nil
}

func (s *DefaultCoordinator) queueLenByCapability(ctx context.Context, capability string) (int, error) {
	if s.queueRepo == nil || strings.TrimSpace(capability) == "" {
		return 0, nil
	}
	items, err := s.queueRepo.ListWorkItems(ctx)
	if err != nil {
		return 0, err
	}
	queueLen := 0
	for _, item := range items {
		if item.Type != capability {
			continue
		}
		if item.Status == string(WorkItemDone) {
			continue
		}
		queueLen++
	}
	return queueLen, nil
}

func (s *DefaultCoordinator) evaluate(wi WorkItem, coordinationContext CoordinationContext) (CoordinationDecisionType, string) {
	if wi.Status == string(WorkItemDone) {
		return CoordinationBlock, fmt.Sprintf("work item %s already executed", wi.ID)
	}
	if coordinationContext.Complexity == 0 {
		coordinationContext.Complexity = len(coordinationContext.ActionTypes)
	}
	if len(coordinationContext.Actors) == 0 {
		return CoordinationExecuteNow, "coordination context not yet enriched with actors; continue to downstream eligibility checks"
	}
	eligibleActors := make([]string, 0)
	executableActors := make([]string, 0)
	lowTrustActors := make([]string, 0)
	complexityLimited := false
	for _, actor := range coordinationContext.Actors {
		if !actor.Enabled || !containsString(actor.QueueMemberships, wi.QueueID) {
			continue
		}
		if len(coordinationContext.ActionTypes) > 0 && !allowsAllActionTypes(actor, coordinationContext.ActionTypes) {
			continue
		}
		if profile, ok := coordinationContext.Profiles[actor.ID]; ok && profile.MaxComplexity > 0 && coordinationContext.Complexity > profile.MaxComplexity {
			complexityLimited = true
			continue
		}
		eligibleActors = append(eligibleActors, actor.ID)
		trustLevel := "low"
		if profile, ok := coordinationContext.Profiles[actor.ID]; ok && profile.TrustAvailable {
			trustLevel = profile.TrustLevel
		}
		if trustLevel == "high" || trustLevel == "medium" {
			executableActors = append(executableActors, actor.ID)
		} else {
			lowTrustActors = append(lowTrustActors, actor.ID)
		}
	}
	if len(eligibleActors) == 0 {
		if complexityLimited {
			return CoordinationEscalate, fmt.Sprintf("work item complexity %d exceeds available actor profiles", coordinationContext.Complexity)
		}
		return CoordinationBlock, fmt.Sprintf("no eligible actor available for queue %s", wi.QueueID)
	}
	if len(executableActors) > 0 {
		return CoordinationExecuteNow, fmt.Sprintf("trusted actor available for execution: %s", strings.Join(executableActors, ","))
	}
	return CoordinationDefer, fmt.Sprintf("only low-trust actors available: %s; defer until trust improves or supervised release is granted", strings.Join(lowTrustActors, ","))
}

func coordinationPriority(decisionType CoordinationDecisionType) int {
	switch decisionType {
	case CoordinationEscalate:
		return CoordinationPriorityEscalate
	case CoordinationExecuteNow:
		return CoordinationPriorityExecuteNow
	case CoordinationDefer:
		return CoordinationPriorityDefer
	default:
		return CoordinationPriorityBlock
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func allowsAllActionTypes(actor CoordinationActor, actions []string) bool {
	allowed := make(map[string]struct{}, len(actor.AllowedActionTypes))
	for _, actionType := range actor.AllowedActionTypes {
		allowed[actionType] = struct{}{}
	}
	for _, action := range actions {
		if _, ok := allowed[action]; !ok {
			return false
		}
	}
	return true
}
