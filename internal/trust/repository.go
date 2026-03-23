package trust

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type InMemoryRepository struct {
	mu      sync.RWMutex
	byActor map[string]TrustProfile
	inOrder []string
}

func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{
		byActor: map[string]TrustProfile{},
	}
}

func normalizeActorID(actorID string) string {
	return strings.TrimSpace(actorID)
}

func (r *InMemoryRepository) Save(_ context.Context, p TrustProfile) error {
	actorID := normalizeActorID(p.ActorID)
	if actorID == "" {
		return fmt.Errorf("actor id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.byActor[actorID]; !ok {
		r.inOrder = append(r.inOrder, actorID)
	}

	p.ActorID = actorID
	r.byActor[actorID] = p
	return nil
}

func (r *InMemoryRepository) GetByActor(_ context.Context, actorID string) (TrustProfile, bool, error) {
	actorID = normalizeActorID(actorID)
	if actorID == "" {
		return TrustProfile{}, false, nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.byActor[actorID]
	if !ok {
		return TrustProfile{}, false, nil
	}
	return p, true, nil
}

func (r *InMemoryRepository) List(_ context.Context) ([]TrustProfile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]TrustProfile, 0, len(r.inOrder))
	for _, actorID := range r.inOrder {
		out = append(out, r.byActor[actorID])
	}
	return out, nil
}