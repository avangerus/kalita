package executionruntime

import (
	"context"
	"sync"
)

type InMemoryWAL struct {
	mu        sync.RWMutex
	records   []WALRecord
	bySession map[string][]int
}

func NewInMemoryWAL() *InMemoryWAL { return &InMemoryWAL{bySession: map[string][]int{}} }
func (w *InMemoryWAL) Append(_ context.Context, r WALRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	idx := len(w.records)
	w.records = append(w.records, cloneWALRecord(r))
	w.bySession[r.ExecutionSessionID] = append(w.bySession[r.ExecutionSessionID], idx)
	return nil
}
func (w *InMemoryWAL) ListBySession(_ context.Context, sessionID string) ([]WALRecord, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]WALRecord, 0, len(w.bySession[sessionID]))
	for _, idx := range w.bySession[sessionID] {
		out = append(out, cloneWALRecord(w.records[idx]))
	}
	return out, nil
}
func cloneWALRecord(r WALRecord) WALRecord {
	out := r
	if r.Payload != nil {
		out.Payload = map[string]any{}
		for k, v := range r.Payload {
			out.Payload[k] = v
		}
	}
	return out
}
