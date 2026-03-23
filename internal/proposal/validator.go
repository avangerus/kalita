package proposal

import (
	"context"
	"fmt"
	"strings"

	"kalita/internal/actionplan"
	"kalita/internal/employee"
)

type deterministicValidator struct{}

func NewValidator() Validator { return &deterministicValidator{} }

func (v *deterministicValidator) Validate(_ context.Context, p Proposal, actor employee.DigitalEmployee) (ProposalStatus, string, error) {
	if len(p.Payload) == 0 {
		return ProposalRejected, "proposal payload is required", nil
	}
	if strings.TrimSpace(p.Justification) == "" {
		return ProposalRejected, "proposal justification is required", nil
	}
	switch p.Type {
	case ProposalTypeActionIntent:
	default:
		return ProposalRejected, fmt.Sprintf("proposal type %q is not supported", p.Type), nil
	}
	rawActions, ok := p.Payload["actions"]
	if !ok {
		return ProposalRejected, "proposal payload actions are required", nil
	}
	actions, err := actionItems(rawActions)
	if err != nil {
		return ProposalRejected, fmt.Sprintf("proposal payload actions are invalid: %v", err), nil
	}
	if len(actions) == 0 {
		return ProposalRejected, "proposal payload actions are required", nil
	}
	allowed := map[actionplan.ActionType]struct{}{}
	for _, t := range actor.AllowedActionTypes {
		allowed[t] = struct{}{}
	}
	for i, item := range actions {
		rawType, _ := item["type"].(string)
		actionType := actionplan.ActionType(strings.TrimSpace(rawType))
		if actionType == "" {
			return ProposalRejected, fmt.Sprintf("proposal action %d type is required", i), nil
		}
		if _, ok := allowed[actionType]; !ok {
			return ProposalRejected, fmt.Sprintf("proposal action type %q is not allowed for actor %s", actionType, actor.ID), nil
		}
	}
	return ProposalValidated, "", nil
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
