package actionplan

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (f fakeClock) Now() time.Time { return f.now }

type fakeIDGenerator struct {
	ids []string
	idx int
}

func (f *fakeIDGenerator) NewID() string { id := f.ids[f.idx]; f.idx++; return id }

func testRegistry() Registry {
	registry := NewRegistry()
	registry.Register(ActionDefinition{
		Type:          "send_notification",
		Reversibility: ReversibilityCompensatable,
		Idempotency:   IdempotencySafe,
		Validate: func(params map[string]any) error {
			if strings.TrimSpace(stringValue(params["message"])) == "" {
				return fmt.Errorf("message is required")
			}
			return nil
		},
		CompensationBuilder: func(params map[string]any) (map[string]any, error) {
			return map[string]any{"message": params["message"]}, nil
		},
	})
	registry.Register(ActionDefinition{
		Type:          "write_audit_log",
		Reversibility: ReversibilityIrreversible,
		Idempotency:   IdempotencySafe,
		Validate: func(params map[string]any) error {
			if strings.TrimSpace(stringValue(params["entry"])) == "" {
				return fmt.Errorf("entry is required")
			}
			return nil
		},
	})
	return registry
}

func TestCompilerCompileValidPlan(t *testing.T) {
	clock := fakeClock{now: time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"plan-1", "act-1", "comp-1", "act-2"}}
	compiler := NewCompiler(testRegistry(), clock, ids)

	plan, err := compiler.Compile(context.Background(), map[string]any{
		"reason": "policy-approved outreach",
		"actions": []any{
			map[string]any{"type": "send_notification", "params": map[string]any{"message": "hello"}},
			map[string]any{"type": "write_audit_log", "params": map[string]any{"entry": "logged"}},
		},
	})
	if err != nil {
		t.Fatalf("Compile error = %v", err)
	}
	if plan.ID != "plan-1" || len(plan.Actions) != 2 {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.Actions[0].Compensation == nil || plan.Actions[0].Compensation.ID != "comp-1" {
		t.Fatalf("first compensation = %#v", plan.Actions[0].Compensation)
	}
	if plan.Actions[1].Compensation != nil {
		t.Fatalf("second compensation = %#v, want nil", plan.Actions[1].Compensation)
	}
}

func TestCompilerFailsForUnknownActionType(t *testing.T) {
	compiler := NewCompiler(testRegistry(), fakeClock{now: time.Now()}, &fakeIDGenerator{ids: []string{"plan-1"}})
	_, err := compiler.Compile(context.Background(), map[string]any{"reason": "x", "actions": []any{map[string]any{"type": "unknown"}}})
	if err == nil || !strings.Contains(err.Error(), "unknown action type") {
		t.Fatalf("Compile error = %v, want unknown action type", err)
	}
}

func TestCompilerFailsWhenCompensationMissingForReversibleAction(t *testing.T) {
	registry := NewRegistry()
	registry.Register(ActionDefinition{Type: "reversible", Reversibility: ReversibilityCompensatable, Idempotency: IdempotencySafe, Validate: func(map[string]any) error { return nil }})
	compiler := NewCompiler(registry, fakeClock{now: time.Now()}, &fakeIDGenerator{ids: []string{"plan-1", "act-1"}})
	_, err := compiler.Compile(context.Background(), map[string]any{"reason": "x", "actions": []any{map[string]any{"type": "reversible"}}})
	if err == nil || !strings.Contains(err.Error(), "requires compensation") {
		t.Fatalf("Compile error = %v, want compensation failure", err)
	}
}
