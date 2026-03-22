package runtime

import (
	"testing"
	"time"

	"kalita/internal/schema"
)

func TestExecuteWorkflowActionReturnsProposalForValidTransition(t *testing.T) {
	t.Parallel()

	storage, rec := workflowTestStorage()
	beforeUpdated := rec.UpdatedAt
	result, errs := ExecuteWorkflowAction(storage, "test.WorkflowTask", rec.ID, "submit", 3)
	if len(errs) > 0 {
		t.Fatalf("ExecuteWorkflowAction() errs = %#v", errs)
	}
	if result.To != "InApproval" || result.From != "Draft" {
		t.Fatalf("unexpected transition result = %#v", result)
	}
	if got := rec.Data["status"]; got != "Draft" {
		t.Fatalf("status mutated to %v", got)
	}
	if got := rec.Data["title"]; got != "Keep me" {
		t.Fatalf("title mutated to %v", got)
	}
	if rec.Version != 3 {
		t.Fatalf("version = %d, want 3", rec.Version)
	}
	if !rec.UpdatedAt.Equal(beforeUpdated) {
		t.Fatalf("updated_at changed")
	}
	if result.Committed {
		t.Fatalf("committed = true, want false")
	}
	if got := result.Record["status"]; got != "InApproval" {
		t.Fatalf("proposal status = %v", got)
	}
}

func TestExecuteWorkflowActionRejectsDisallowedState(t *testing.T) {
	t.Parallel()

	storage, rec := workflowTestStorage()
	rec.Data["status"] = "Approved"

	_, errs := ExecuteWorkflowAction(storage, "test.WorkflowTask", rec.ID, "submit", 3)
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %#v", errs)
	}
	if errs[0].Code != "enum_invalid" {
		t.Fatalf("error code = %q", errs[0].Code)
	}
}

func TestExecuteWorkflowActionRejectsUnknownAction(t *testing.T) {
	t.Parallel()

	storage, rec := workflowTestStorage()
	_, errs := ExecuteWorkflowAction(storage, "test.WorkflowTask", rec.ID, "reject", 3)
	if len(errs) != 1 || errs[0].Field != "action" {
		t.Fatalf("unexpected errs = %#v", errs)
	}
}

func TestExecuteWorkflowActionRejectsVersionMismatch(t *testing.T) {
	t.Parallel()

	storage, rec := workflowTestStorage()
	_, errs := ExecuteWorkflowAction(storage, "test.WorkflowTask", rec.ID, "submit", 2)
	if len(errs) != 1 || errs[0].Code != "version_conflict" {
		t.Fatalf("unexpected errs = %#v", errs)
	}
}

func workflowTestStorage() (*Storage, *Record) {
	entity := &schema.Entity{
		Name:   "WorkflowTask",
		Module: "test",
		Fields: []schema.Field{
			{Name: "title", Type: "string"},
			{Name: "status", Type: "enum", Enum: []string{"Draft", "InApproval", "Approved"}},
		},
		Workflow: &schema.Workflow{
			StatusField: "status",
			Actions: map[string]schema.WorkflowAction{
				"submit": {From: []string{"Draft"}, To: "InApproval"},
			},
		},
	}
	st := NewStorage([]*schema.Entity{entity}, nil)
	now := time.Now().UTC().Add(-time.Minute)
	rec := &Record{
		ID:        "rec-1",
		Version:   3,
		CreatedAt: now,
		UpdatedAt: now,
		Data: map[string]interface{}{
			"title":  "Keep me",
			"status": "Draft",
		},
	}
	st.Data["test.WorkflowTask"] = map[string]*Record{rec.ID: rec}
	return st, rec
}
