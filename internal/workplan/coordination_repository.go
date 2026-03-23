package workplan

import (
	"context"
	"sync"
)

type InMemoryCoordinationRepository struct {
	mu            sync.RWMutex
	decisionsByID map[string]CoordinationDecision
	decisionOrder []string
	idsByWorkItem map[string][]string
	idsByCase     map[string][]string
	idsByQueue    map[string][]string
}

func NewInMemoryCoordinationRepository() *InMemoryCoordinationRepository {
	return &InMemoryCoordinationRepository{
		decisionsByID: make(map[string]CoordinationDecision),
		idsByWorkItem: make(map[string][]string),
		idsByCase:     make(map[string][]string),
		idsByQueue:    make(map[string][]string),
	}
}

func (r *InMemoryCoordinationRepository) SaveDecision(_ context.Context, d CoordinationDecision) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.decisionsByID[d.ID]; ok {
		if existing.WorkItemID != d.WorkItemID {
			r.idsByWorkItem[existing.WorkItemID] = removeID(r.idsByWorkItem[existing.WorkItemID], d.ID)
		}
		if existing.CaseID != d.CaseID {
			r.idsByCase[existing.CaseID] = removeID(r.idsByCase[existing.CaseID], d.ID)
		}
		if existing.QueueID != d.QueueID {
			r.idsByQueue[existing.QueueID] = removeID(r.idsByQueue[existing.QueueID], d.ID)
		}
	} else {
		r.decisionOrder = append(r.decisionOrder, d.ID)
	}
	if !containsID(r.idsByWorkItem[d.WorkItemID], d.ID) {
		r.idsByWorkItem[d.WorkItemID] = append(r.idsByWorkItem[d.WorkItemID], d.ID)
	}
	if !containsID(r.idsByCase[d.CaseID], d.ID) {
		r.idsByCase[d.CaseID] = append(r.idsByCase[d.CaseID], d.ID)
	}
	if !containsID(r.idsByQueue[d.QueueID], d.ID) {
		r.idsByQueue[d.QueueID] = append(r.idsByQueue[d.QueueID], d.ID)
	}
	r.decisionsByID[d.ID] = d
	return nil
}

func (r *InMemoryCoordinationRepository) GetDecision(_ context.Context, id string) (CoordinationDecision, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.decisionsByID[id]
	if !ok {
		return CoordinationDecision{}, false, nil
	}
	return d, true, nil
}

func (r *InMemoryCoordinationRepository) ListByWorkItem(_ context.Context, workItemID string) ([]CoordinationDecision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.listByIDs(r.idsByWorkItem[workItemID]), nil
}

func (r *InMemoryCoordinationRepository) ListByCase(_ context.Context, caseID string) ([]CoordinationDecision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.listByIDs(r.idsByCase[caseID]), nil
}

func (r *InMemoryCoordinationRepository) ListByQueue(_ context.Context, queueID string) ([]CoordinationDecision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.listByIDs(r.idsByQueue[queueID]), nil
}

func (r *InMemoryCoordinationRepository) listByIDs(ids []string) []CoordinationDecision {
	out := make([]CoordinationDecision, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.decisionsByID[id])
	}
	return out
}

func (r *InMemoryCoordinationRepository) ListAll(_ context.Context) ([]CoordinationDecision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]CoordinationDecision, 0, len(r.decisionOrder))
	for _, id := range r.decisionOrder {
		out = append(out, r.decisionsByID[id])
	}
	return out, nil
}
