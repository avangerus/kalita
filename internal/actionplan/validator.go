package actionplan

import (
	"fmt"
	"strings"
)

type DefaultValidator struct {
	registry       Registry
	forbiddenTypes map[ActionType]struct{}
}

func NewValidator(registry Registry, forbiddenTypes ...ActionType) *DefaultValidator {
	forbidden := make(map[ActionType]struct{}, len(forbiddenTypes))
	for _, actionType := range forbiddenTypes {
		forbidden[actionType] = struct{}{}
	}
	return &DefaultValidator{registry: registry, forbiddenTypes: forbidden}
}

func (v *DefaultValidator) Validate(plan ActionPlan) error {
	if strings.TrimSpace(plan.Reason) == "" {
		return fmt.Errorf("reason is required")
	}
	if len(plan.Actions) == 0 {
		return fmt.Errorf("action plan must contain at least one action")
	}
	for i, action := range plan.Actions {
		if _, forbidden := v.forbiddenTypes[action.Type]; forbidden {
			return fmt.Errorf("actions[%d] uses forbidden action type %q", i, action.Type)
		}
		def, ok := v.registry.Get(action.Type)
		if !ok {
			return fmt.Errorf("actions[%d] has unknown action type %q", i, action.Type)
		}
		if def.Validate != nil {
			if err := def.Validate(action.Params); err != nil {
				return fmt.Errorf("actions[%d] validation failed: %w", i, err)
			}
		}
		if action.Reversibility == "" {
			return fmt.Errorf("actions[%d] reversibility is required", i)
		}
		if action.Idempotency == "" {
			return fmt.Errorf("actions[%d] idempotency is required", i)
		}
		if def.Reversibility != action.Reversibility {
			return fmt.Errorf("actions[%d] reversibility mismatch for %q", i, action.Type)
		}
		if def.Idempotency != action.Idempotency {
			return fmt.Errorf("actions[%d] idempotency mismatch for %q", i, action.Type)
		}
		if def.Reversibility == ReversibilityIrreversible {
			if action.Compensation != nil {
				return fmt.Errorf("actions[%d] compensation must be nil for irreversible action %q", i, action.Type)
			}
			continue
		}
		if action.Compensation == nil {
			return fmt.Errorf("actions[%d] compensation is required for %q", i, action.Type)
		}
		if action.Compensation.Type != action.Type {
			return fmt.Errorf("actions[%d] compensation type mismatch for %q", i, action.Type)
		}
		if def.Validate != nil {
			if err := def.Validate(action.Compensation.Params); err != nil {
				return fmt.Errorf("actions[%d] compensation validation failed: %w", i, err)
			}
		}
	}
	return nil
}
