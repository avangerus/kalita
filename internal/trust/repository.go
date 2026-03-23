package trust

import (
	"context"
	"sync"
)

type InMemoryRepository struct {
	mu          sync.RWMutex
	byActorID   map[string]TrustProfile
	actorIDList []string
}

func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{byActorID: make(map[string]TrustProfile)}
}

func (r *InMemoryRepository) Save(_ context.Context, p TrustProfile) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byActorID[p.ActorID]; !ok {
		r.actorIDList = append(r.actorIDList, p.ActorID)
	}
	r.byActorID[p.ActorID] = p
	return nil
}

func (r *InMemoryRepository) GetByActor(_ context.Context, actorID string) (TrustProfile, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byActorID[actorID]
	if !ok {
		return TrustProfile{}, false, nil
	}
	return p, true, nil
}

func (r *InMemoryRepository) List(_ context.Context) ([]TrustProfile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]TrustProfile, 0, len(r.actorIDList))
	for _, actorID := range r.actorIDList {
		out = append(out, r.byActorID[actorID])
	}
	return out, nil
}
