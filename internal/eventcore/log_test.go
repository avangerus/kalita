package eventcore

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestInMemoryEventLogPreservesEventOrder(t *testing.T) {
	t.Parallel()

	log := NewInMemoryEventLog()
	ctx := context.Background()
	corr := "corr-1"

	first := Event{ID: "evt-1", CorrelationID: corr, OccurredAt: time.Unix(1, 0).UTC()}
	second := Event{ID: "evt-2", CorrelationID: corr, OccurredAt: time.Unix(2, 0).UTC()}

	if err := log.AppendEvent(ctx, first); err != nil {
		t.Fatalf("AppendEvent(first) error = %v", err)
	}
	if err := log.AppendEvent(ctx, second); err != nil {
		t.Fatalf("AppendEvent(second) error = %v", err)
	}

	events, executionEvents, err := log.ListByCorrelation(ctx, corr)
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(executionEvents) != 0 {
		t.Fatalf("executionEvents len = %d, want 0", len(executionEvents))
	}
	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	if events[0].ID != first.ID || events[1].ID != second.ID {
		t.Fatalf("event order = [%s %s], want [%s %s]", events[0].ID, events[1].ID, first.ID, second.ID)
	}
}

func TestInMemoryEventLogSeparatesExecutionEvents(t *testing.T) {
	t.Parallel()

	log := NewInMemoryEventLog()
	ctx := context.Background()
	corr := "corr-2"

	if err := log.AppendEvent(ctx, Event{ID: "evt-1", CorrelationID: corr}); err != nil {
		t.Fatalf("AppendEvent error = %v", err)
	}
	execEvent := ExecutionEvent{ID: "exec-1", CorrelationID: corr, ExecutionID: "run-1"}
	if err := log.AppendExecutionEvent(ctx, execEvent); err != nil {
		t.Fatalf("AppendExecutionEvent error = %v", err)
	}

	events, executionEvents, err := log.ListByCorrelation(ctx, corr)
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if len(executionEvents) != 1 {
		t.Fatalf("executionEvents len = %d, want 1", len(executionEvents))
	}
	if executionEvents[0].ID != execEvent.ID {
		t.Fatalf("execution event ID = %s, want %s", executionEvents[0].ID, execEvent.ID)
	}
}

func TestInMemoryEventLogListByCorrelationHandlesEmptyCorrelationID(t *testing.T) {
	t.Parallel()

	log := NewInMemoryEventLog()
	ctx := context.Background()

	if err := log.AppendEvent(ctx, Event{ID: "evt-empty", CorrelationID: ""}); err != nil {
		t.Fatalf("AppendEvent error = %v", err)
	}
	if err := log.AppendExecutionEvent(ctx, ExecutionEvent{ID: "exec-empty", CorrelationID: ""}); err != nil {
		t.Fatalf("AppendExecutionEvent error = %v", err)
	}

	events, executionEvents, err := log.ListByCorrelation(ctx, "")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(events) != 1 || len(executionEvents) != 1 {
		t.Fatalf("got %d events and %d execution events, want 1 and 1", len(events), len(executionEvents))
	}
}

func TestInMemoryEventLogConcurrentAppendSmoke(t *testing.T) {
	t.Parallel()

	log := NewInMemoryEventLog()
	ctx := context.Background()
	const n = 64

	var wg sync.WaitGroup
	wg.Add(n * 2)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			_ = log.AppendEvent(ctx, Event{ID: fmt.Sprintf("evt-%d", i), CorrelationID: "corr-concurrent"})
		}(i)
		go func(i int) {
			defer wg.Done()
			_ = log.AppendExecutionEvent(ctx, ExecutionEvent{ID: fmt.Sprintf("exec-%d", i), CorrelationID: "corr-concurrent"})
		}(i)
	}
	wg.Wait()

	events, executionEvents, err := log.ListByCorrelation(ctx, "corr-concurrent")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(events) != n {
		t.Fatalf("events len = %d, want %d", len(events), n)
	}
	if len(executionEvents) != n {
		t.Fatalf("executionEvents len = %d, want %d", len(executionEvents), n)
	}
}
