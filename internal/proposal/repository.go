package proposal

import (
	"context"
	"sync"
)

type InMemoryRepository struct {
	mu               sync.RWMutex
	proposalsByID    map[string]Proposal
	proposalOrder    []string
	proposalsByWork  map[string][]string
	proposalsByActor map[string][]string
}

func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{
		proposalsByID:    map[string]Proposal{},
		proposalsByWork:  map[string][]string{},
		proposalsByActor: map[string][]string{},
	}
}

func (r *InMemoryRepository) Save(_ context.Context, p Proposal) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.proposalsByID[p.ID]; ok {
		if existing.WorkItemID != p.WorkItemID {
			r.proposalsByWork[existing.WorkItemID] = removeString(r.proposalsByWork[existing.WorkItemID], p.ID)
		}
		if existing.ActorID != p.ActorID {
			r.proposalsByActor[existing.ActorID] = removeString(r.proposalsByActor[existing.ActorID], p.ID)
		}
	} else {
		r.proposalOrder = append(r.proposalOrder, p.ID)
	}
	r.proposalsByID[p.ID] = cloneProposal(p)
	if !containsString(r.proposalsByWork[p.WorkItemID], p.ID) {
		r.proposalsByWork[p.WorkItemID] = append(r.proposalsByWork[p.WorkItemID], p.ID)
	}
	if !containsString(r.proposalsByActor[p.ActorID], p.ID) {
		r.proposalsByActor[p.ActorID] = append(r.proposalsByActor[p.ActorID], p.ID)
	}
	return nil
}

func (r *InMemoryRepository) Get(_ context.Context, id string) (Proposal, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.proposalsByID[id]
	if !ok {
		return Proposal{}, false, nil
	}
	return cloneProposal(p), true, nil
}

func (r *InMemoryRepository) ListByWorkItem(_ context.Context, workItemID string) ([]Proposal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.proposalsByWork[workItemID]
	out := make([]Proposal, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneProposal(r.proposalsByID[id]))
	}
	return out, nil
}

func (r *InMemoryRepository) ListByActor(_ context.Context, actorID string) ([]Proposal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.proposalsByActor[actorID]
	out := make([]Proposal, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneProposal(r.proposalsByID[id]))
	}
	return out, nil
}

func cloneProposal(p Proposal) Proposal {
	out := p
	out.Payload = cloneMap(p.Payload)
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneValue(v)
	}
	return out
}
func cloneSlice(in []any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = cloneValue(v)
	}
	return out
}
func cloneValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		return cloneSlice(typed)
	default:
		return typed
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
func removeString(items []string, target string) []string {
	out := items[:0]
	for _, item := range items {
		if item != target {
			out = append(out, item)
		}
	}
	return out
}

func (r *InMemoryRepository) ListAll(_ context.Context) ([]Proposal, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Proposal, 0, len(r.proposalOrder))
	for _, id := range r.proposalOrder {
		out = append(out, cloneProposal(r.proposalsByID[id]))
	}
	return out, nil
}
