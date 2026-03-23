package capability

import (
	"context"
	"sync"
)

type InMemoryCapabilityRepository struct {
	mu         sync.RWMutex
	byID       map[string]Capability
	order      []string
	actorByID  map[string][]ActorCapability
	actorOrder map[string][]string
}

func NewInMemoryRepository() *InMemoryCapabilityRepository {
	return &InMemoryCapabilityRepository{
		byID:       map[string]Capability{},
		actorByID:  map[string][]ActorCapability{},
		actorOrder: map[string][]string{},
	}
}

func (r *InMemoryCapabilityRepository) SaveCapability(_ context.Context, c Capability) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[c.ID]; !ok {
		r.order = append(r.order, c.ID)
	}
	r.byID[c.ID] = cloneCapability(c)
	return nil
}

func (r *InMemoryCapabilityRepository) GetCapability(_ context.Context, id string) (Capability, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.byID[id]
	if !ok {
		return Capability{}, false, nil
	}
	return cloneCapability(c), true, nil
}

func (r *InMemoryCapabilityRepository) ListCapabilities(_ context.Context) ([]Capability, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Capability, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, cloneCapability(r.byID[id]))
	}
	return out, nil
}

func (r *InMemoryCapabilityRepository) AssignCapability(_ context.Context, ac ActorCapability) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	assignments := r.actorByID[ac.ActorID]
	for i := range assignments {
		if assignments[i].CapabilityID == ac.CapabilityID {
			assignments[i] = ac
			r.actorByID[ac.ActorID] = assignments
			return nil
		}
	}
	r.actorByID[ac.ActorID] = append(assignments, ac)
	r.actorOrder[ac.ActorID] = append(r.actorOrder[ac.ActorID], ac.CapabilityID)
	return nil
}

func (r *InMemoryCapabilityRepository) ListByActor(_ context.Context, actorID string) ([]ActorCapability, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	assignments := r.actorByID[actorID]
	out := make([]ActorCapability, len(assignments))
	copy(out, assignments)
	return out, nil
}

func cloneCapability(c Capability) Capability {
	out := c
	if c.Metadata != nil {
		out.Metadata = make(map[string]any, len(c.Metadata))
		for k, v := range c.Metadata {
			out.Metadata[k] = v
		}
	}
	return out
}
