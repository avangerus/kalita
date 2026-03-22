package command

import (
	"context"
	"errors"
	"testing"
	"time"

	"kalita/internal/eventcore"
)

type fakeClock struct{ now time.Time }

func (f fakeClock) Now() time.Time { return f.now }

type fakeIDGenerator struct {
	ids []string
	i   int
}

func (f *fakeIDGenerator) NewID() string {
	if f.i >= len(f.ids) {
		return ""
	}
	id := f.ids[f.i]
	f.i++
	return id
}

type denyPolicy struct{ err error }

func (p denyPolicy) Admit(context.Context, eventcore.Command) error { return p.err }

func TestServiceSubmitFillsMissingFieldsDeterministically(t *testing.T) {
	t.Parallel()

	log := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"cmd-1", "corr-1", "exec-1", "evt-1"}}
	service := NewService(log, PassThroughAdmissionPolicy{}, clock, ids)

	cmd, err := service.Submit(context.Background(), eventcore.Command{Type: "workflow.action", TargetRef: "test.WorkflowTask/rec-1"})
	if err != nil {
		t.Fatalf("Submit error = %v", err)
	}

	if cmd.ID != "cmd-1" || cmd.CorrelationID != "corr-1" || cmd.ExecutionID != "exec-1" {
		t.Fatalf("normalized IDs = %#v", cmd)
	}
	if !cmd.RequestedAt.Equal(clock.now) {
		t.Fatalf("RequestedAt = %v, want %v", cmd.RequestedAt, clock.now)
	}

	_, executionEvents, err := log.ListByCorrelation(context.Background(), "corr-1")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(executionEvents) != 1 {
		t.Fatalf("executionEvents len = %d, want 1", len(executionEvents))
	}
	got := executionEvents[0]
	if got.ID != "evt-1" {
		t.Fatalf("execution event ID = %s, want evt-1", got.ID)
	}
	if got.Step != "command_admission" || got.Status != "admitted" {
		t.Fatalf("execution event = %#v", got)
	}
	if got.CausationID != "cmd-1" || got.ExecutionID != "exec-1" || got.CorrelationID != "corr-1" {
		t.Fatalf("execution event linkage = %#v", got)
	}
	if got.Payload["command_type"] != "workflow.action" || got.Payload["target_ref"] != "test.WorkflowTask/rec-1" {
		t.Fatalf("execution payload = %#v", got.Payload)
	}
}

func TestServiceSubmitDeniedCommandAppendsNothing(t *testing.T) {
	t.Parallel()

	log := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"cmd-1", "corr-1", "exec-1", "evt-1"}}
	denyErr := errors.New("denied")
	service := NewService(log, denyPolicy{err: denyErr}, clock, ids)

	_, err := service.Submit(context.Background(), eventcore.Command{Type: "workflow.action"})
	if !errors.Is(err, denyErr) {
		t.Fatalf("Submit error = %v, want %v", err, denyErr)
	}

	events, executionEvents, listErr := log.ListByCorrelation(context.Background(), "corr-1")
	if listErr != nil {
		t.Fatalf("ListByCorrelation error = %v", listErr)
	}
	if len(events) != 0 || len(executionEvents) != 0 {
		t.Fatalf("log mutated after denied command: %d events, %d execution events", len(events), len(executionEvents))
	}
}

func TestServiceSubmitPreservesProvidedIDs(t *testing.T) {
	t.Parallel()

	log := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"evt-1"}}
	service := NewService(log, PassThroughAdmissionPolicy{}, clock, ids)

	requestedAt := time.Date(2026, 3, 20, 8, 30, 0, 0, time.UTC)
	input := eventcore.Command{
		ID:            "cmd-existing",
		RequestedAt:   requestedAt,
		CorrelationID: "corr-existing",
		ExecutionID:   "exec-existing",
		Type:          "workflow.action",
	}
	cmd, err := service.Submit(context.Background(), input)
	if err != nil {
		t.Fatalf("Submit error = %v", err)
	}
	if cmd.ID != input.ID || cmd.CorrelationID != input.CorrelationID || cmd.ExecutionID != input.ExecutionID {
		t.Fatalf("normalized cmd = %#v, want preserved IDs from %#v", cmd, input)
	}
	if !cmd.RequestedAt.Equal(requestedAt) {
		t.Fatalf("RequestedAt = %v, want %v", cmd.RequestedAt, requestedAt)
	}

	_, executionEvents, err := log.ListByCorrelation(context.Background(), "corr-existing")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(executionEvents) != 1 {
		t.Fatalf("executionEvents len = %d, want 1", len(executionEvents))
	}
	if executionEvents[0].ExecutionID != "exec-existing" || executionEvents[0].CausationID != "cmd-existing" {
		t.Fatalf("execution event = %#v", executionEvents[0])
	}
}
