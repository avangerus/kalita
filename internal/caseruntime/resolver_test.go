package caseruntime

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

type stubResolver struct {
	caseValue Case
	existed   bool
	err       error
}

func (s stubResolver) ResolveForCommand(context.Context, eventcore.Command) (Case, bool, error) {
	return s.caseValue, s.existed, s.err
}

func TestResolverFindsExistingCaseByCorrelation(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryCaseRepository()
	existing := Case{ID: "case-1", CorrelationID: "corr-1", SubjectRef: "subject-1"}
	if err := repo.Save(context.Background(), existing); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	resolver := NewResolver(repo, fakeClock{}, &fakeIDGenerator{})
	got, existed, err := resolver.ResolveForCommand(context.Background(), eventcore.Command{CorrelationID: "corr-1", TargetRef: "subject-2"})
	if err != nil {
		t.Fatalf("ResolveForCommand error = %v", err)
	}
	if !existed || got.ID != existing.ID {
		t.Fatalf("ResolveForCommand = (%#v, %v), want existing case", got, existed)
	}
}

func TestResolverFindsExistingCaseBySubjectRef(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryCaseRepository()
	existing := Case{ID: "case-1", SubjectRef: "test.WorkflowTask/rec-1"}
	if err := repo.Save(context.Background(), existing); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	resolver := NewResolver(repo, fakeClock{}, &fakeIDGenerator{})
	got, existed, err := resolver.ResolveForCommand(context.Background(), eventcore.Command{TargetRef: "test.WorkflowTask/rec-1"})
	if err != nil {
		t.Fatalf("ResolveForCommand error = %v", err)
	}
	if !existed || got.ID != existing.ID {
		t.Fatalf("ResolveForCommand = (%#v, %v), want existing case", got, existed)
	}
}

func TestResolverCreatesNewCaseWhenNoneFound(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryCaseRepository()
	clock := fakeClock{now: time.Date(2026, 3, 22, 11, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"case-1"}}
	resolver := NewResolver(repo, clock, ids)

	got, existed, err := resolver.ResolveForCommand(context.Background(), eventcore.Command{Type: "workflow.action", CorrelationID: "corr-1", TargetRef: "test.WorkflowTask/rec-1"})
	if err != nil {
		t.Fatalf("ResolveForCommand error = %v", err)
	}
	if existed {
		t.Fatal("existed = true, want false")
	}
	if got.ID != "case-1" || got.Status != string(CaseOpen) {
		t.Fatalf("new case = %#v", got)
	}

	saved, ok, err := repo.GetByID(context.Background(), "case-1")
	if err != nil {
		t.Fatalf("GetByID error = %v", err)
	}
	if !ok || saved.ID != "case-1" {
		t.Fatalf("saved = %#v, ok=%v", saved, ok)
	}
}

func TestResolverCreatesDeterministicFields(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryCaseRepository()
	clock := fakeClock{now: time.Date(2026, 3, 22, 12, 34, 56, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"case-123"}}
	resolver := NewResolver(repo, clock, ids)
	cmd := eventcore.Command{Type: "workflow.action", CorrelationID: "corr-9", TargetRef: "test.WorkflowTask/rec-9"}

	got, existed, err := resolver.ResolveForCommand(context.Background(), cmd)
	if err != nil {
		t.Fatalf("ResolveForCommand error = %v", err)
	}
	if existed {
		t.Fatal("existed = true, want false")
	}
	if got.ID != "case-123" || got.Kind != "workflow.action" || got.Status != string(CaseOpen) {
		t.Fatalf("case identity fields = %#v", got)
	}
	if got.CorrelationID != "corr-9" || got.SubjectRef != "test.WorkflowTask/rec-9" {
		t.Fatalf("case links = %#v", got)
	}
	if got.Title != "workflow.action for test.WorkflowTask/rec-9" {
		t.Fatalf("Title = %q", got.Title)
	}
	if !got.OpenedAt.Equal(clock.now) || !got.UpdatedAt.Equal(clock.now) {
		t.Fatalf("times = opened %v updated %v want %v", got.OpenedAt, got.UpdatedAt, clock.now)
	}
}

func TestCaseServiceResolveCommandNormalizesCommandAndAppendsExecutionEvent(t *testing.T) {
	t.Parallel()

	log := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 13, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"exec-event-1"}}
	service := NewService(stubResolver{caseValue: Case{ID: "case-1", Kind: "workflow.action", SubjectRef: "target-1"}}, log, clock, ids)

	result, err := service.ResolveCommand(context.Background(), eventcore.Command{ID: "cmd-1", Type: "workflow.action", CorrelationID: "corr-1", ExecutionID: "exec-1", TargetRef: "target-1"})
	if err != nil {
		t.Fatalf("ResolveCommand error = %v", err)
	}
	if result.Command.CaseID != "case-1" || result.Case.ID != "case-1" {
		t.Fatalf("result = %#v", result)
	}
	_, executionEvents, err := log.ListByCorrelation(context.Background(), "corr-1")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(executionEvents) != 1 {
		t.Fatalf("executionEvents len = %d, want 1", len(executionEvents))
	}
	if executionEvents[0].Step != "case_resolution" || executionEvents[0].Status != "opened_new" {
		t.Fatalf("execution event = %#v", executionEvents[0])
	}
}

func TestCaseServiceReturnsResolverFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("resolve failed")
	service := NewService(stubResolver{err: wantErr}, nil, fakeClock{}, &fakeIDGenerator{})
	_, err := service.ResolveCommand(context.Background(), eventcore.Command{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("ResolveCommand error = %v, want %v", err, wantErr)
	}
}
