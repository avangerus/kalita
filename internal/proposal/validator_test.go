package proposal

import (
	"context"
	"testing"

	"kalita/internal/actionplan"
	"kalita/internal/employee"
)

func TestValidatorRejectsEmptyPayload(t *testing.T) {
	assertValidation(t, Proposal{Type: ProposalTypeActionIntent, Justification: "because"}, ProposalRejected)
}
func TestValidatorRejectsEmptyJustification(t *testing.T) {
	assertValidation(t, Proposal{Type: ProposalTypeActionIntent, Payload: map[string]any{"actions": []any{map[string]any{"type": "legacy_workflow_action"}}}}, ProposalRejected)
}
func TestValidatorRejectsMissingActions(t *testing.T) {
	assertValidation(t, Proposal{Type: ProposalTypeActionIntent, Payload: map[string]any{"reason": "x"}, Justification: "because"}, ProposalRejected)
}
func TestValidatorRejectsUnsupportedActionType(t *testing.T) {
	v := NewValidator()
	status, reason, err := v.Validate(context.Background(), Proposal{Type: ProposalTypeActionIntent, Payload: map[string]any{"actions": []any{map[string]any{"type": "forbidden_action"}}}, Justification: "because"}, employee.DigitalEmployee{ID: "emp-1", AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}})
	if err != nil || status != ProposalRejected || reason == "" {
		t.Fatalf("status=%s reason=%q err=%v", status, reason, err)
	}
}
func TestValidatorAcceptsValidProposal(t *testing.T) {
	v := NewValidator()
	status, reason, err := v.Validate(context.Background(), Proposal{Type: ProposalTypeActionIntent, Payload: map[string]any{"actions": []any{map[string]any{"type": "legacy_workflow_action"}}}, Justification: "because"}, employee.DigitalEmployee{ID: "emp-1", AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}})
	if err != nil || status != ProposalValidated || reason != "" {
		t.Fatalf("status=%s reason=%q err=%v", status, reason, err)
	}
}
func assertValidation(t *testing.T, p Proposal, want ProposalStatus) {
	t.Helper()
	v := NewValidator()
	status, reason, err := v.Validate(context.Background(), p, employee.DigitalEmployee{ID: "emp-1", AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}})
	if err != nil || status != want || reason == "" {
		t.Fatalf("status=%s reason=%q err=%v", status, reason, err)
	}
}
