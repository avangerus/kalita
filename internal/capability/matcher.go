package capability

import (
	"context"
	"fmt"
	"sort"

	"kalita/internal/actionplan"
	"kalita/internal/employee"
	"kalita/internal/workplan"
)

type deterministicMatcher struct {
	capabilities CapabilityRepository
	assignments  ActorCapabilityRepository
}

func NewMatcher(capabilities CapabilityRepository, assignments ActorCapabilityRepository) Matcher {
	return &deterministicMatcher{capabilities: capabilities, assignments: assignments}
}

func (m *deterministicMatcher) MatchActor(ctx context.Context, wi workplan.WorkItem, plan actionplan.ActionPlan, actors []employee.DigitalEmployee) (employee.DigitalEmployee, string, error) {
	if m.capabilities == nil {
		return employee.DigitalEmployee{}, "", fmt.Errorf("capability repository is nil")
	}
	if m.assignments == nil {
		return employee.DigitalEmployee{}, "", fmt.Errorf("actor capability repository is nil")
	}
	requiredCodes := requiredCapabilityCodes(plan)
	for _, actor := range actors {
		if !actor.Enabled {
			continue
		}
		if !containsString(actor.QueueMemberships, wi.QueueID) {
			continue
		}
		if !allowsAllActionTypes(actor, plan.Actions) {
			continue
		}
		matches, err := m.actorHasCapabilities(ctx, actor.ID, requiredCodes)
		if err != nil {
			return employee.DigitalEmployee{}, "", err
		}
		if !matches {
			continue
		}
		reason := fmt.Sprintf("selected actor %s for queue %s by deterministic capability match", actor.ID, wi.QueueID)
		return actor, reason, nil
	}
	return employee.DigitalEmployee{}, "", fmt.Errorf("no eligible digital employee for queue %s and work item %s with required capabilities", wi.QueueID, wi.ID)
}

func (m *deterministicMatcher) actorHasCapabilities(ctx context.Context, actorID string, requiredCodes []string) (bool, error) {
	assigned, err := m.assignments.ListByActor(ctx, actorID)
	if err != nil {
		return false, err
	}
	codes := make(map[string]struct{}, len(assigned))
	for _, assignment := range assigned {
		capability, ok, err := m.capabilities.GetCapability(ctx, assignment.CapabilityID)
		if err != nil {
			return false, err
		}
		if !ok {
			continue
		}
		codes[capability.Code] = struct{}{}
	}
	for _, code := range requiredCodes {
		if _, ok := codes[code]; !ok {
			return false, nil
		}
	}
	return true, nil
}

func requiredCapabilityCodes(plan actionplan.ActionPlan) []string {
	set := make(map[string]struct{}, len(plan.Actions))
	for _, action := range plan.Actions {
		set[string(action.Type)] = struct{}{}
	}
	codes := make([]string, 0, len(set))
	for code := range set {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func allowsAllActionTypes(actor employee.DigitalEmployee, actions []actionplan.Action) bool {
	allowed := make(map[actionplan.ActionType]struct{}, len(actor.AllowedActionTypes))
	for _, actionType := range actor.AllowedActionTypes {
		allowed[actionType] = struct{}{}
	}
	for _, action := range actions {
		if _, ok := allowed[action.Type]; !ok {
			return false
		}
	}
	return true
}
