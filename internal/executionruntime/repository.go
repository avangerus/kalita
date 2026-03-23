package executionruntime

import (
	"context"
	"sort"
	"sync"
)

type InMemoryExecutionRepository struct {
	mu                 sync.RWMutex
	sessions           map[string]ExecutionSession
	sessionsByWorkItem map[string][]string
	steps              map[string]StepExecution
	stepsBySession     map[string][]string
}

func NewInMemoryExecutionRepository() *InMemoryExecutionRepository {
	return &InMemoryExecutionRepository{sessions: map[string]ExecutionSession{}, sessionsByWorkItem: map[string][]string{}, steps: map[string]StepExecution{}, stepsBySession: map[string][]string{}}
}

func (r *InMemoryExecutionRepository) SaveSession(_ context.Context, s ExecutionSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.sessions[s.ID]; ok {
		r.sessionsByWorkItem[existing.WorkItemID] = removeID(r.sessionsByWorkItem[existing.WorkItemID], s.ID)
	}
	r.sessions[s.ID] = s
	r.sessionsByWorkItem[s.WorkItemID] = appendIfMissing(r.sessionsByWorkItem[s.WorkItemID], s.ID)
	return nil
}
func (r *InMemoryExecutionRepository) GetSession(_ context.Context, id string) (ExecutionSession, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	return s, ok, nil
}
func (r *InMemoryExecutionRepository) ListSessionsByWorkItem(_ context.Context, workItemID string) ([]ExecutionSession, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ExecutionSession, 0, len(r.sessionsByWorkItem[workItemID]))
	for _, id := range r.sessionsByWorkItem[workItemID] {
		out = append(out, r.sessions[id])
	}
	return out, nil
}
func (r *InMemoryExecutionRepository) SaveStep(_ context.Context, s StepExecution) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.steps[s.ID]; ok {
		r.stepsBySession[existing.ExecutionSessionID] = removeID(r.stepsBySession[existing.ExecutionSessionID], s.ID)
	}
	r.steps[s.ID] = cloneStep(s)
	r.stepsBySession[s.ExecutionSessionID] = appendIfMissing(r.stepsBySession[s.ExecutionSessionID], s.ID)
	sort.SliceStable(r.stepsBySession[s.ExecutionSessionID], func(i, j int) bool {
		a, b := r.steps[r.stepsBySession[s.ExecutionSessionID][i]], r.steps[r.stepsBySession[s.ExecutionSessionID][j]]
		if a.StepIndex == b.StepIndex {
			return a.ID < b.ID
		}
		return a.StepIndex < b.StepIndex
	})
	return nil
}
func (r *InMemoryExecutionRepository) GetStep(_ context.Context, id string) (StepExecution, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.steps[id]
	return cloneStep(s), ok, nil
}
func (r *InMemoryExecutionRepository) ListStepsBySession(_ context.Context, sessionID string) ([]StepExecution, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]StepExecution, 0, len(r.stepsBySession[sessionID]))
	for _, id := range r.stepsBySession[sessionID] {
		out = append(out, cloneStep(r.steps[id]))
	}
	return out, nil
}

func appendIfMissing(ids []string, id string) []string {
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
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
func cloneStep(s StepExecution) StepExecution {
	out := s
	if s.StartedAt != nil {
		v := *s.StartedAt
		out.StartedAt = &v
	}
	if s.FinishedAt != nil {
		v := *s.FinishedAt
		out.FinishedAt = &v
	}
	return out
}

func (r *InMemoryExecutionRepository) ListSessions(_ context.Context) ([]ExecutionSession, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ExecutionSession, 0, len(r.sessions))
	for _, sessionIDs := range r.sessionsByWorkItem {
		for _, id := range sessionIDs {
			out = append(out, r.sessions[id])
		}
	}
	return out, nil
}

func (r *InMemoryExecutionRepository) ListSteps(_ context.Context) ([]StepExecution, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]StepExecution, 0, len(r.steps))
	for _, stepIDs := range r.stepsBySession {
		for _, id := range stepIDs {
			out = append(out, cloneStep(r.steps[id]))
		}
	}
	return out, nil
}
