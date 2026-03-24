package workplan

import (
	"context"
	"sort"
	"sync"

	"kalita/internal/actionplan"
)

type InMemoryQueueRepository struct {
	mu             sync.RWMutex
	queuesByID     map[string]WorkQueue
	queueOrder     []string
	workItemsByID  map[string]WorkItem
	workItemOrder  []string
	workIDsByCase  map[string][]string
	workIDsByQueue map[string][]string
}

var _ WorkItemRepository = (*InMemoryQueueRepository)(nil)

func NewInMemoryQueueRepository() *InMemoryQueueRepository {
	return &InMemoryQueueRepository{
		queuesByID:     make(map[string]WorkQueue),
		workItemsByID:  make(map[string]WorkItem),
		workIDsByCase:  make(map[string][]string),
		workIDsByQueue: make(map[string][]string),
	}
}

func (r *InMemoryQueueRepository) SaveQueue(_ context.Context, q WorkQueue) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.queuesByID[q.ID]; !exists {
		r.queueOrder = append(r.queueOrder, q.ID)
	}
	r.queuesByID[q.ID] = cloneQueue(q)
	return nil
}

func (r *InMemoryQueueRepository) GetQueue(_ context.Context, id string) (WorkQueue, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	q, ok := r.queuesByID[id]
	if !ok {
		return WorkQueue{}, false, nil
	}
	return cloneQueue(q), true, nil
}

func (r *InMemoryQueueRepository) ListQueues(_ context.Context) ([]WorkQueue, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]WorkQueue, 0, len(r.queueOrder))
	for _, id := range r.queueOrder {
		out = append(out, cloneQueue(r.queuesByID[id]))
	}
	return out, nil
}

func (r *InMemoryQueueRepository) SaveWorkItem(_ context.Context, wi WorkItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, exists := r.workItemsByID[wi.ID]; exists {
		if existing.CaseID != wi.CaseID {
			r.workIDsByCase[existing.CaseID] = removeID(r.workIDsByCase[existing.CaseID], wi.ID)
		}
		if existing.QueueID != wi.QueueID {
			r.workIDsByQueue[existing.QueueID] = removeID(r.workIDsByQueue[existing.QueueID], wi.ID)
		}
	} else {
		r.workItemOrder = append(r.workItemOrder, wi.ID)
	}
	if !containsID(r.workIDsByCase[wi.CaseID], wi.ID) {
		r.workIDsByCase[wi.CaseID] = append(r.workIDsByCase[wi.CaseID], wi.ID)
	}
	if !containsID(r.workIDsByQueue[wi.QueueID], wi.ID) {
		r.workIDsByQueue[wi.QueueID] = append(r.workIDsByQueue[wi.QueueID], wi.ID)
	}
	r.workItemsByID[wi.ID] = cloneWorkItem(wi)
	return nil
}

func (r *InMemoryQueueRepository) GetWorkItem(_ context.Context, id string) (WorkItem, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	wi, ok := r.workItemsByID[id]
	if !ok {
		return WorkItem{}, false, nil
	}
	return cloneWorkItem(wi), true, nil
}

func (r *InMemoryQueueRepository) Save(ctx context.Context, wi WorkItem) error {
	return r.SaveWorkItem(ctx, wi)
}

func (r *InMemoryQueueRepository) FindByID(ctx context.Context, id string) (WorkItem, bool, error) {
	return r.GetWorkItem(ctx, id)
}

func (r *InMemoryQueueRepository) ListWorkItemsByCase(_ context.Context, caseID string) ([]WorkItem, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.workIDsByCase[caseID]
	out := make([]WorkItem, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneWorkItem(r.workItemsByID[id]))
	}
	return out, nil
}

func (r *InMemoryQueueRepository) FindByCaseID(ctx context.Context, caseID string) ([]WorkItem, error) {
	return r.ListWorkItemsByCase(ctx, caseID)
}

func (r *InMemoryQueueRepository) ListWorkItemsByQueue(_ context.Context, queueID string) ([]WorkItem, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.workIDsByQueue[queueID]
	out := make([]WorkItem, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneWorkItem(r.workItemsByID[id]))
	}
	return out, nil
}

func (r *InMemoryQueueRepository) ListWorkItems(_ context.Context) ([]WorkItem, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]WorkItem, 0, len(r.workItemOrder))
	for _, id := range r.workItemOrder {
		out = append(out, cloneWorkItem(r.workItemsByID[id]))
	}
	return out, nil
}

func (r *InMemoryQueueRepository) FindByStatus(_ context.Context, status string) ([]WorkItem, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]WorkItem, 0)
	for _, id := range r.workItemOrder {
		item := r.workItemsByID[id]
		if item.Status == status {
			out = append(out, cloneWorkItem(item))
		}
	}
	return out, nil
}

func (r *InMemoryQueueRepository) FindByActorID(_ context.Context, actorID string) ([]WorkItem, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]WorkItem, 0)
	for _, id := range r.workItemOrder {
		item := r.workItemsByID[id]
		if item.AssignedEmployeeID == actorID {
			out = append(out, cloneWorkItem(item))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func cloneQueue(q WorkQueue) WorkQueue {
	out := q
	out.AllowedCaseKinds = append([]string(nil), q.AllowedCaseKinds...)
	out.DefaultEmployeeIDs = append([]string(nil), q.DefaultEmployeeIDs...)
	return out
}

func cloneWorkItem(wi WorkItem) WorkItem {
	out := wi
	if wi.DueAt != nil {
		due := *wi.DueAt
		out.DueAt = &due
	}
	if wi.ActionPlan != nil {
		plan := cloneActionPlan(*wi.ActionPlan)
		out.ActionPlan = &plan
	}
	return out
}

func cloneActionPlan(plan actionplan.ActionPlan) actionplan.ActionPlan {
	out := plan
	out.Actions = make([]actionplan.Action, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		out.Actions = append(out.Actions, cloneAction(action))
	}
	return out
}

func cloneAction(action actionplan.Action) actionplan.Action {
	out := action
	out.Params = cloneAnyMap(action.Params)
	if action.Compensation != nil {
		compensation := cloneAction(*action.Compensation)
		out.Compensation = &compensation
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneAnyValue(v)
	}
	return out
}

func cloneAnySlice(in []any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = cloneAnyValue(v)
	}
	return out
}

func cloneAnyValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		return cloneAnySlice(typed)
	default:
		return typed
	}
}

func containsID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func removeID(ids []string, target string) []string {
	out := ids[:0]
	for _, id := range ids {
		if id != target {
			out = append(out, id)
		}
	}
	return out
}
