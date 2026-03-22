package policy

import (
	"context"
	"sync"
)

type InMemoryRepository struct {
	mu sync.RWMutex

	decisionsByID               map[string]PolicyDecision
	decisionOrder               []string
	decisionIDsByCoordinationID map[string][]string
	approvalsByID               map[string]ApprovalRequest
	approvalOrder               []string
	approvalIDsByCoordinationID map[string][]string
}

func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{
		decisionsByID:               make(map[string]PolicyDecision),
		decisionIDsByCoordinationID: make(map[string][]string),
		approvalsByID:               make(map[string]ApprovalRequest),
		approvalIDsByCoordinationID: make(map[string][]string),
	}
}

func (r *InMemoryRepository) SaveDecision(_ context.Context, d PolicyDecision) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.decisionsByID[d.ID]; ok {
		if existing.CoordinationDecisionID != d.CoordinationDecisionID {
			r.decisionIDsByCoordinationID[existing.CoordinationDecisionID] = removeID(r.decisionIDsByCoordinationID[existing.CoordinationDecisionID], d.ID)
		}
	} else {
		r.decisionOrder = append(r.decisionOrder, d.ID)
	}
	if !containsID(r.decisionIDsByCoordinationID[d.CoordinationDecisionID], d.ID) {
		r.decisionIDsByCoordinationID[d.CoordinationDecisionID] = append(r.decisionIDsByCoordinationID[d.CoordinationDecisionID], d.ID)
	}
	r.decisionsByID[d.ID] = d
	return nil
}

func (r *InMemoryRepository) GetDecision(_ context.Context, id string) (PolicyDecision, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.decisionsByID[id]
	if !ok {
		return PolicyDecision{}, false, nil
	}
	return d, true, nil
}

func (r *InMemoryRepository) ListByCoordinationDecision(_ context.Context, coordinationDecisionID string) ([]PolicyDecision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.decisionIDsByCoordinationID[coordinationDecisionID]
	out := make([]PolicyDecision, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.decisionsByID[id])
	}
	return out, nil
}

func (r *InMemoryRepository) SaveApprovalRequest(_ context.Context, a ApprovalRequest) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.approvalsByID[a.ID]; ok {
		if existing.CoordinationDecisionID != a.CoordinationDecisionID {
			r.approvalIDsByCoordinationID[existing.CoordinationDecisionID] = removeID(r.approvalIDsByCoordinationID[existing.CoordinationDecisionID], a.ID)
		}
	} else {
		r.approvalOrder = append(r.approvalOrder, a.ID)
	}
	if !containsID(r.approvalIDsByCoordinationID[a.CoordinationDecisionID], a.ID) {
		r.approvalIDsByCoordinationID[a.CoordinationDecisionID] = append(r.approvalIDsByCoordinationID[a.CoordinationDecisionID], a.ID)
	}
	r.approvalsByID[a.ID] = a
	return nil
}

func (r *InMemoryRepository) GetApprovalRequest(_ context.Context, id string) (ApprovalRequest, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.approvalsByID[id]
	if !ok {
		return ApprovalRequest{}, false, nil
	}
	return a, true, nil
}

func (r *InMemoryRepository) ListApprovalRequestsByCoordinationDecision(_ context.Context, coordinationDecisionID string) ([]ApprovalRequest, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.approvalIDsByCoordinationID[coordinationDecisionID]
	out := make([]ApprovalRequest, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.approvalsByID[id])
	}
	return out, nil
}

func containsID(ids []string, id string) bool {
	for _, existing := range ids {
		if existing == id {
			return true
		}
	}
	return false
}

func removeID(ids []string, id string) []string {
	if len(ids) == 0 {
		return ids
	}
	out := ids[:0]
	for _, existing := range ids {
		if existing != id {
			out = append(out, existing)
		}
	}
	return out
}
