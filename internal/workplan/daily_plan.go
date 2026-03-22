package workplan

import (
	"context"
	"sync"
	"time"
)

type DailyPlan struct {
	ID          string
	QueueID     string
	PlanDate    string
	Status      string
	WorkItemIDs []string
	Assignments map[string][]string
	CreatedAt   time.Time
	ApprovedAt  *time.Time
}

type DailyPlanStatus string

const (
	DailyPlanDraft DailyPlanStatus = "draft"
	DailyPlanReady DailyPlanStatus = "ready"
)

type PlanRepository interface {
	SavePlan(ctx context.Context, p DailyPlan) error
	GetPlan(ctx context.Context, id string) (DailyPlan, bool, error)
	FindPlanByQueueAndDate(ctx context.Context, queueID, planDate string) (DailyPlan, bool, error)
	ListPlansByQueue(ctx context.Context, queueID string) ([]DailyPlan, error)
}

type InMemoryPlanRepository struct {
	mu               sync.RWMutex
	plansByID        map[string]DailyPlan
	planOrder        []string
	planIDByQueueDay map[string]string
	planIDsByQueue   map[string][]string
}

func NewInMemoryPlanRepository() *InMemoryPlanRepository {
	return &InMemoryPlanRepository{
		plansByID:        make(map[string]DailyPlan),
		planIDByQueueDay: make(map[string]string),
		planIDsByQueue:   make(map[string][]string),
	}
}

func (r *InMemoryPlanRepository) SavePlan(_ context.Context, p DailyPlan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.plansByID[p.ID]; !exists {
		r.planOrder = append(r.planOrder, p.ID)
	}
	if existing, exists := r.plansByID[p.ID]; exists && existing.QueueID != p.QueueID {
		r.planIDsByQueue[existing.QueueID] = removeID(r.planIDsByQueue[existing.QueueID], p.ID)
	}
	if !containsID(r.planIDsByQueue[p.QueueID], p.ID) {
		r.planIDsByQueue[p.QueueID] = append(r.planIDsByQueue[p.QueueID], p.ID)
	}
	r.planIDByQueueDay[planQueueDateKey(p.QueueID, p.PlanDate)] = p.ID
	r.plansByID[p.ID] = cloneDailyPlan(p)
	return nil
}

func (r *InMemoryPlanRepository) GetPlan(_ context.Context, id string) (DailyPlan, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plansByID[id]
	if !ok {
		return DailyPlan{}, false, nil
	}
	return cloneDailyPlan(p), true, nil
}

func (r *InMemoryPlanRepository) FindPlanByQueueAndDate(_ context.Context, queueID, planDate string) (DailyPlan, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.planIDByQueueDay[planQueueDateKey(queueID, planDate)]
	if !ok {
		return DailyPlan{}, false, nil
	}
	return cloneDailyPlan(r.plansByID[id]), true, nil
}

func (r *InMemoryPlanRepository) ListPlansByQueue(_ context.Context, queueID string) ([]DailyPlan, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.planIDsByQueue[queueID]
	out := make([]DailyPlan, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneDailyPlan(r.plansByID[id]))
	}
	return out, nil
}

func cloneDailyPlan(p DailyPlan) DailyPlan {
	out := p
	out.WorkItemIDs = append([]string(nil), p.WorkItemIDs...)
	if p.Assignments != nil {
		out.Assignments = make(map[string][]string, len(p.Assignments))
		for key, ids := range p.Assignments {
			out.Assignments[key] = append([]string(nil), ids...)
		}
	}
	if p.ApprovedAt != nil {
		approvedAt := *p.ApprovedAt
		out.ApprovedAt = &approvedAt
	}
	return out
}

func planQueueDateKey(queueID, planDate string) string {
	return queueID + "::" + planDate
}
