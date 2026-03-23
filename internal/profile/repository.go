package profile

import (
	"context"
	"sync"

	"kalita/internal/actionplan"
)

type InMemoryRepository struct {
	mu                   sync.RWMutex
	profilesByID         map[string]CompetencyProfile
	profileOrder         []string
	profileByActor       map[string]string
	requirementsByAction map[actionplan.ActionType]CapabilityRequirement
	requirementOrder     []actionplan.ActionType
}

func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{
		profilesByID:         make(map[string]CompetencyProfile),
		profileByActor:       make(map[string]string),
		requirementsByAction: make(map[actionplan.ActionType]CapabilityRequirement),
	}
}

func (r *InMemoryRepository) SaveProfile(_ context.Context, p CompetencyProfile) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.profilesByID[p.ID]; ok {
		if existing.ActorID != "" && existing.ActorID != p.ActorID {
			delete(r.profileByActor, existing.ActorID)
		}
	} else {
		r.profileOrder = append(r.profileOrder, p.ID)
	}
	r.profilesByID[p.ID] = cloneProfile(p)
	if p.ActorID != "" {
		r.profileByActor[p.ActorID] = p.ID
	}
	return nil
}

func (r *InMemoryRepository) GetProfile(_ context.Context, id string) (CompetencyProfile, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.profilesByID[id]
	if !ok {
		return CompetencyProfile{}, false, nil
	}
	return cloneProfile(p), true, nil
}

func (r *InMemoryRepository) GetProfileByActor(_ context.Context, actorID string) (CompetencyProfile, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.profileByActor[actorID]
	if !ok {
		return CompetencyProfile{}, false, nil
	}
	p, ok := r.profilesByID[id]
	if !ok {
		return CompetencyProfile{}, false, nil
	}
	return cloneProfile(p), true, nil
}

func (r *InMemoryRepository) ListProfiles(_ context.Context) ([]CompetencyProfile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]CompetencyProfile, 0, len(r.profileOrder))
	for _, id := range r.profileOrder {
		out = append(out, cloneProfile(r.profilesByID[id]))
	}
	return out, nil
}

func (r *InMemoryRepository) SaveRequirement(_ context.Context, req CapabilityRequirement) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.requirementsByAction[req.ActionType]; !ok {
		r.requirementOrder = append(r.requirementOrder, req.ActionType)
	}
	r.requirementsByAction[req.ActionType] = cloneRequirement(req)
	return nil
}

func (r *InMemoryRepository) ListRequirements(_ context.Context) ([]CapabilityRequirement, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]CapabilityRequirement, 0, len(r.requirementOrder))
	for _, actionType := range r.requirementOrder {
		out = append(out, cloneRequirement(r.requirementsByAction[actionType]))
	}
	return out, nil
}

func cloneProfile(p CompetencyProfile) CompetencyProfile {
	out := p
	out.PreferredWorkKinds = append([]string(nil), p.PreferredWorkKinds...)
	if p.Metadata != nil {
		out.Metadata = make(map[string]any, len(p.Metadata))
		for k, v := range p.Metadata {
			out.Metadata[k] = v
		}
	}
	return out
}

func cloneRequirement(r CapabilityRequirement) CapabilityRequirement {
	out := r
	out.CapabilityCodes = append([]string(nil), r.CapabilityCodes...)
	return out
}
