package actionplan

import (
	"context"
	"time"
)

type ActionType string

const (
	ReversibilityFullyReversible = "fully_reversible"
	ReversibilityCompensatable   = "compensatable"
	ReversibilityIrreversible    = "irreversible"

	IdempotencySafe        = "safe"
	IdempotencyConditional = "conditional"
	IdempotencyUnsafe      = "unsafe"
)

type Action struct {
	ID string

	Type   ActionType
	Params map[string]any

	Reversibility string
	Compensation  *Action

	Idempotency string

	CreatedAt time.Time
}

type ActionPlan struct {
	ID string

	WorkItemID string
	CaseID     string

	Actions []Action

	CreatedAt time.Time
	Reason    string
}

type ActionDefinition struct {
	Type ActionType

	Validate func(params map[string]any) error

	Reversibility string
	Idempotency   string

	CompensationBuilder func(params map[string]any) (map[string]any, error)
}

type Registry interface {
	Register(def ActionDefinition)
	Get(actionType ActionType) (ActionDefinition, bool)
}

type Compiler interface {
	Compile(ctx context.Context, input map[string]any) (ActionPlan, error)
}

type Validator interface {
	Validate(plan ActionPlan) error
}

type Service interface {
	CreatePlan(ctx context.Context, workItemID string, caseID string, input map[string]any) (ActionPlan, error)
}
