package caseruntime

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryCaseRepositorySaveAndGetByID(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryCaseRepository()
	openedAt := time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)
	input := Case{
		ID:            "case-1",
		Kind:          "workflow.action",
		Status:        string(CaseOpen),
		Title:         "workflow.action for test.WorkflowTask/rec-1",
		SubjectRef:    "test.WorkflowTask/rec-1",
		CorrelationID: "corr-1",
		OpenedAt:      openedAt,
		UpdatedAt:     openedAt,
		Attributes:    map[string]any{"priority": "normal"},
	}

	if err := repo.Save(context.Background(), input); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	got, ok, err := repo.GetByID(context.Background(), "case-1")
	if err != nil {
		t.Fatalf("GetByID error = %v", err)
	}
	if !ok {
		t.Fatal("GetByID ok = false, want true")
	}
	if got.ID != input.ID || got.CorrelationID != input.CorrelationID || got.SubjectRef != input.SubjectRef {
		t.Fatalf("GetByID case = %#v, want %#v", got, input)
	}
	if got.Attributes["priority"] != "normal" {
		t.Fatalf("attributes = %#v", got.Attributes)
	}
}

func TestInMemoryCaseRepositoryFindByCorrelation(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryCaseRepository()
	_ = repo.Save(context.Background(), Case{ID: "case-1", CorrelationID: "corr-1"})

	got, ok, err := repo.FindByCorrelation(context.Background(), "corr-1")
	if err != nil {
		t.Fatalf("FindByCorrelation error = %v", err)
	}
	if !ok || got.ID != "case-1" {
		t.Fatalf("FindByCorrelation = (%#v, %v), want case-1 true", got, ok)
	}
}

func TestInMemoryCaseRepositoryFindBySubjectRef(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryCaseRepository()
	_ = repo.Save(context.Background(), Case{ID: "case-1", SubjectRef: "test.WorkflowTask/rec-1"})

	got, ok, err := repo.FindBySubjectRef(context.Background(), "test.WorkflowTask/rec-1")
	if err != nil {
		t.Fatalf("FindBySubjectRef error = %v", err)
	}
	if !ok || got.ID != "case-1" {
		t.Fatalf("FindBySubjectRef = (%#v, %v), want case-1 true", got, ok)
	}
}

func TestInMemoryCaseRepositoryOverwriteBySameID(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryCaseRepository()
	ctx := context.Background()
	if err := repo.Save(ctx, Case{ID: "case-1", CorrelationID: "corr-1", SubjectRef: "subject-1", Title: "old"}); err != nil {
		t.Fatalf("Save(old) error = %v", err)
	}
	if err := repo.Save(ctx, Case{ID: "case-1", CorrelationID: "corr-2", SubjectRef: "subject-2", Title: "new"}); err != nil {
		t.Fatalf("Save(new) error = %v", err)
	}

	got, ok, err := repo.GetByID(ctx, "case-1")
	if err != nil {
		t.Fatalf("GetByID error = %v", err)
	}
	if !ok || got.Title != "new" || got.CorrelationID != "corr-2" || got.SubjectRef != "subject-2" {
		t.Fatalf("GetByID = %#v, ok=%v", got, ok)
	}
	if _, ok, _ := repo.FindByCorrelation(ctx, "corr-1"); ok {
		t.Fatal("old correlation index still resolves")
	}
	if _, ok, _ := repo.FindBySubjectRef(ctx, "subject-1"); ok {
		t.Fatal("old subject index still resolves")
	}
}
