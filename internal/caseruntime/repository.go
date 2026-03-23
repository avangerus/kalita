package caseruntime

import (
	"context"
	"sync"
)

type InMemoryCaseRepository struct {
	mu              sync.RWMutex
	byID            map[string]Case
	idByCorrelation map[string]string
	idBySubjectRef  map[string]string
}

func NewInMemoryCaseRepository() *InMemoryCaseRepository {
	return &InMemoryCaseRepository{
		byID:            make(map[string]Case),
		idByCorrelation: make(map[string]string),
		idBySubjectRef:  make(map[string]string),
	}
}

func (r *InMemoryCaseRepository) Save(_ context.Context, c Case) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.byID[c.ID]; ok {
		if existing.CorrelationID != "" && existing.CorrelationID != c.CorrelationID {
			delete(r.idByCorrelation, existing.CorrelationID)
		}
		if existing.SubjectRef != "" && existing.SubjectRef != c.SubjectRef {
			delete(r.idBySubjectRef, existing.SubjectRef)
		}
	}

	r.byID[c.ID] = cloneCase(c)
	if c.CorrelationID != "" {
		r.idByCorrelation[c.CorrelationID] = c.ID
	}
	if c.SubjectRef != "" {
		r.idBySubjectRef[c.SubjectRef] = c.ID
	}
	return nil
}

func (r *InMemoryCaseRepository) GetByID(_ context.Context, id string) (Case, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	c, ok := r.byID[id]
	if !ok {
		return Case{}, false, nil
	}
	return cloneCase(c), true, nil
}

func (r *InMemoryCaseRepository) List(_ context.Context) ([]Case, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Case, 0, len(r.byID))
	for _, c := range r.byID {
		out = append(out, cloneCase(c))
	}
	return out, nil
}

func (r *InMemoryCaseRepository) FindByCorrelation(ctx context.Context, correlationID string) (Case, bool, error) {
	if correlationID == "" {
		return Case{}, false, nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	id, ok := r.idByCorrelation[correlationID]
	if !ok {
		return Case{}, false, nil
	}
	c, ok := r.byID[id]
	if !ok {
		return Case{}, false, nil
	}
	return cloneCase(c), true, nil
}

func (r *InMemoryCaseRepository) FindBySubjectRef(ctx context.Context, subjectRef string) (Case, bool, error) {
	if subjectRef == "" {
		return Case{}, false, nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	id, ok := r.idBySubjectRef[subjectRef]
	if !ok {
		return Case{}, false, nil
	}
	c, ok := r.byID[id]
	if !ok {
		return Case{}, false, nil
	}
	return cloneCase(c), true, nil
}

func cloneCase(c Case) Case {
	out := c
	if c.Attributes != nil {
		out.Attributes = make(map[string]any, len(c.Attributes))
		for k, v := range c.Attributes {
			out.Attributes[k] = v
		}
	}
	return out
}
