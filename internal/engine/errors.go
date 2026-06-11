// Package engine is the runtime kernel: it executes a compiled dsl.Model over
// the event journal. Records, validation, permissions — every state change is
// an event; current state is an in-memory projection (ADR-001).
package engine

import "fmt"

// Closed error code list, mirrored by the MCP contract (§6). Every error gives
// an agent enough data to self-correct without a human.
const (
	CodePermissionDenied = "PERMISSION_DENIED"
	CodeValidation       = "VALIDATION_ERROR"
	CodeNotFound         = "NOT_FOUND"
	CodeConflict         = "CONFLICT"
	CodeBasisRequired    = "BASIS_REQUIRED"
)

type Err struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`    // VALIDATION_ERROR
	Rule    string `json:"rule,omitempty"`     // PERMISSION_DENIED: which rule decided
	FixHint string `json:"fix_hint,omitempty"`
}

func (e *Err) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

func denied(role, verb, entity, rule string) *Err {
	return &Err{
		Code:    CodePermissionDenied,
		Message: fmt.Sprintf("role %s may not %s %s", role, verb, entity),
		Rule:    rule,
	}
}

func invalid(field, msg, hint string) *Err {
	return &Err{Code: CodeValidation, Message: msg, Field: field, FixHint: hint}
}
