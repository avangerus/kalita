package actionplan

import (
	"context"
	"fmt"
	"strings"

	"kalita/internal/eventcore"
)

type compilerExecutionContextKey struct{}

type ExecutionContext struct {
	ExecutionID   string
	CorrelationID string
	CausationID   string
}

func ContextWithExecution(ctx context.Context, meta ExecutionContext) context.Context {
	return context.WithValue(ctx, compilerExecutionContextKey{}, meta)
}

func executionFromContext(ctx context.Context) ExecutionContext {
	meta, _ := ctx.Value(compilerExecutionContextKey{}).(ExecutionContext)
	return meta
}

func ExecutionMetadataFromContext(ctx context.Context) ExecutionContext {
	return executionFromContext(ctx)
}

type DefaultCompiler struct {
	registry Registry
	clock    eventcore.Clock
	ids      eventcore.IDGenerator
}

func NewCompiler(registry Registry, clock eventcore.Clock, ids eventcore.IDGenerator) *DefaultCompiler {
	if clock == nil {
		clock = eventcore.RealClock{}
	}
	if ids == nil {
		ids = eventcore.NewULIDGenerator()
	}
	return &DefaultCompiler{registry: registry, clock: clock, ids: ids}
}

func (c *DefaultCompiler) Compile(_ context.Context, input map[string]any) (ActionPlan, error) {
	if c.registry == nil {
		return ActionPlan{}, fmt.Errorf("action registry is nil")
	}
	reason := strings.TrimSpace(stringValue(input["reason"]))
	rawActions, ok := input["actions"]
	if !ok {
		return ActionPlan{}, fmt.Errorf("actions is required")
	}
	items, err := actionItems(rawActions)
	if err != nil {
		return ActionPlan{}, err
	}
	createdAt := c.clock.Now()
	plan := ActionPlan{
		ID:        c.ids.NewID(),
		CreatedAt: createdAt,
		Reason:    reason,
		Actions:   make([]Action, 0, len(items)),
	}
	for idx, item := range items {
		actionType := ActionType(strings.TrimSpace(stringValue(item["type"])))
		if actionType == "" {
			return ActionPlan{}, fmt.Errorf("actions[%d].type is required", idx)
		}
		def, ok := c.registry.Get(actionType)
		if !ok {
			return ActionPlan{}, fmt.Errorf("unknown action type %q", actionType)
		}
		params, err := paramsValue(item["params"])
		if err != nil {
			return ActionPlan{}, fmt.Errorf("actions[%d].params: %w", idx, err)
		}
		if def.Validate != nil {
			if err := def.Validate(params); err != nil {
				return ActionPlan{}, fmt.Errorf("actions[%d] validation failed: %w", idx, err)
			}
		}
		action := Action{
			ID:            c.ids.NewID(),
			Type:          def.Type,
			Params:        cloneMap(params),
			Reversibility: def.Reversibility,
			Idempotency:   def.Idempotency,
			CreatedAt:     createdAt,
		}
		if def.Reversibility != ReversibilityIrreversible {
			if def.CompensationBuilder == nil {
				return ActionPlan{}, fmt.Errorf("action type %q requires compensation", def.Type)
			}
			compensationParams, err := def.CompensationBuilder(params)
			if err != nil {
				return ActionPlan{}, fmt.Errorf("actions[%d] compensation failed: %w", idx, err)
			}
			action.Compensation = &Action{
				ID:            c.ids.NewID(),
				Type:          def.Type,
				Params:        cloneMap(compensationParams),
				Reversibility: ReversibilityIrreversible,
				Idempotency:   def.Idempotency,
				CreatedAt:     createdAt,
			}
		}
		plan.Actions = append(plan.Actions, action)
	}
	return plan, nil
}

func actionItems(raw any) ([]map[string]any, error) {
	switch v := raw.(type) {
	case []map[string]any:
		return v, nil
	case []any:
		out := make([]map[string]any, 0, len(v))
		for i, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("actions[%d] must be an object", i)
			}
			out = append(out, m)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("actions must be an array")
	}
}

func paramsValue(raw any) (map[string]any, error) {
	if raw == nil {
		return map[string]any{}, nil
	}
	params, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("must be an object")
	}
	return cloneMap(params), nil
}

func stringValue(raw any) string {
	s, _ := raw.(string)
	return s
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneSlice(in []any) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		return cloneSlice(typed)
	default:
		return typed
	}
}
