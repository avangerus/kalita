package eventcore

import (
	"context"
	"sync"
)

type EventLog interface {
	AppendEvent(ctx context.Context, e Event) error
	AppendExecutionEvent(ctx context.Context, e ExecutionEvent) error
	ListByCorrelation(ctx context.Context, correlationID string) ([]Event, []ExecutionEvent, error)
}

type InMemoryEventLog struct {
	mu              sync.RWMutex
	events          []Event
	executionEvents []ExecutionEvent
}

func NewInMemoryEventLog() *InMemoryEventLog {
	return &InMemoryEventLog{}
}

func (l *InMemoryEventLog) AppendEvent(_ context.Context, e Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
	return nil
}

func (l *InMemoryEventLog) AppendExecutionEvent(_ context.Context, e ExecutionEvent) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.executionEvents = append(l.executionEvents, e)
	return nil
}

func (l *InMemoryEventLog) ListByCorrelation(_ context.Context, correlationID string) ([]Event, []ExecutionEvent, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	events := make([]Event, 0)
	for _, e := range l.events {
		if e.CorrelationID == correlationID {
			events = append(events, e)
		}
	}

	executionEvents := make([]ExecutionEvent, 0)
	for _, e := range l.executionEvents {
		if e.CorrelationID == correlationID {
			executionEvents = append(executionEvents, e)
		}
	}

	return events, executionEvents, nil
}

func (l *InMemoryEventLog) ListAll(_ context.Context) ([]Event, []ExecutionEvent, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	events := make([]Event, len(l.events))
	copy(events, l.events)
	executionEvents := make([]ExecutionEvent, len(l.executionEvents))
	copy(executionEvents, l.executionEvents)
	return events, executionEvents, nil
}
