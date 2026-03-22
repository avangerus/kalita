package executioncontrol

import (
	"context"
	"sync"
)

type InMemoryConstraintsRepository struct {
	mu                  sync.RWMutex
	constraintsByID     map[string]ExecutionConstraints
	order               []string
	idsByPolicyDecision map[string][]string
	idsByCoordination   map[string][]string
	idsByCase           map[string][]string
}

func NewInMemoryConstraintsRepository() *InMemoryConstraintsRepository {
	return &InMemoryConstraintsRepository{
		constraintsByID:     make(map[string]ExecutionConstraints),
		idsByPolicyDecision: make(map[string][]string),
		idsByCoordination:   make(map[string][]string),
		idsByCase:           make(map[string][]string),
	}
}

func (r *InMemoryConstraintsRepository) Save(_ context.Context, c ExecutionConstraints) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.constraintsByID[c.ID]; ok {
		if existing.PolicyDecisionID != c.PolicyDecisionID {
			r.idsByPolicyDecision[existing.PolicyDecisionID] = removeID(r.idsByPolicyDecision[existing.PolicyDecisionID], c.ID)
		}
		if existing.CoordinationDecisionID != c.CoordinationDecisionID {
			r.idsByCoordination[existing.CoordinationDecisionID] = removeID(r.idsByCoordination[existing.CoordinationDecisionID], c.ID)
		}
		if existing.CaseID != c.CaseID {
			r.idsByCase[existing.CaseID] = removeID(r.idsByCase[existing.CaseID], c.ID)
		}
	} else {
		r.order = append(r.order, c.ID)
	}
	if !containsID(r.idsByPolicyDecision[c.PolicyDecisionID], c.ID) {
		r.idsByPolicyDecision[c.PolicyDecisionID] = append(r.idsByPolicyDecision[c.PolicyDecisionID], c.ID)
	}
	if !containsID(r.idsByCoordination[c.CoordinationDecisionID], c.ID) {
		r.idsByCoordination[c.CoordinationDecisionID] = append(r.idsByCoordination[c.CoordinationDecisionID], c.ID)
	}
	if !containsID(r.idsByCase[c.CaseID], c.ID) {
		r.idsByCase[c.CaseID] = append(r.idsByCase[c.CaseID], c.ID)
	}
	r.constraintsByID[c.ID] = c
	return nil
}

func (r *InMemoryConstraintsRepository) Get(_ context.Context, id string) (ExecutionConstraints, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.constraintsByID[id]
	if !ok {
		return ExecutionConstraints{}, false, nil
	}
	return c, true, nil
}

func (r *InMemoryConstraintsRepository) ListByPolicyDecision(_ context.Context, policyDecisionID string) ([]ExecutionConstraints, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.listByIDs(r.idsByPolicyDecision[policyDecisionID]), nil
}

func (r *InMemoryConstraintsRepository) ListByCoordinationDecision(_ context.Context, coordinationDecisionID string) ([]ExecutionConstraints, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.listByIDs(r.idsByCoordination[coordinationDecisionID]), nil
}

func (r *InMemoryConstraintsRepository) ListByCase(_ context.Context, caseID string) ([]ExecutionConstraints, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.listByIDs(r.idsByCase[caseID]), nil
}

func (r *InMemoryConstraintsRepository) listByIDs(ids []string) []ExecutionConstraints {
	out := make([]ExecutionConstraints, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.constraintsByID[id])
	}
	return out
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
