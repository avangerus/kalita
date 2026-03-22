package actionplan

import (
	"strings"
	"testing"
	"time"
)

func TestValidatorRejectsEmptyPlan(t *testing.T) {
	validator := NewValidator(testRegistry())
	err := validator.Validate(ActionPlan{Reason: "reason"})
	if err == nil || !strings.Contains(err.Error(), "at least one action") {
		t.Fatalf("Validate error = %v", err)
	}
}

func TestValidatorRejectsMissingReason(t *testing.T) {
	validator := NewValidator(testRegistry())
	err := validator.Validate(ActionPlan{Actions: []Action{{Type: "write_audit_log", Params: map[string]any{"entry": "x"}, Reversibility: ReversibilityIrreversible, Idempotency: IdempotencySafe, CreatedAt: time.Now()}}})
	if err == nil || !strings.Contains(err.Error(), "reason is required") {
		t.Fatalf("Validate error = %v", err)
	}
}

func TestValidatorRejectsMissingCompensation(t *testing.T) {
	validator := NewValidator(testRegistry())
	err := validator.Validate(ActionPlan{Reason: "reason", Actions: []Action{{Type: "send_notification", Params: map[string]any{"message": "x"}, Reversibility: ReversibilityCompensatable, Idempotency: IdempotencySafe, CreatedAt: time.Now()}}})
	if err == nil || !strings.Contains(err.Error(), "compensation is required") {
		t.Fatalf("Validate error = %v", err)
	}
}
