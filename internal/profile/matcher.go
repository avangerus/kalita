package profile

import (
	"context"
	"fmt"
	"strings"

	"kalita/internal/actionplan"
	"kalita/internal/capability"
	"kalita/internal/employee"
	"kalita/internal/workplan"
)

type deterministicMatcher struct {
	requirements RequirementRepository
	profiles     Repository
	capabilities capability.CapabilityRepository
	assignments  capability.ActorCapabilityRepository
}

func NewMatcher(requirements RequirementRepository, profiles Repository, capabilities capability.CapabilityRepository, assignments capability.ActorCapabilityRepository) Matcher {
	return &deterministicMatcher{requirements: requirements, profiles: profiles, capabilities: capabilities, assignments: assignments}
}

func (m *deterministicMatcher) MatchActor(ctx context.Context, wi workplan.WorkItem, plan actionplan.ActionPlan, actors []employee.DigitalEmployee) (employee.DigitalEmployee, string, error) {
	if m.requirements == nil {
		return employee.DigitalEmployee{}, "", fmt.Errorf("requirement repository is nil")
	}
	if m.capabilities == nil {
		return employee.DigitalEmployee{}, "", fmt.Errorf("capability repository is nil")
	}
	if m.assignments == nil {
		return employee.DigitalEmployee{}, "", fmt.Errorf("actor capability repository is nil")
	}
	requirementsByAction, err := m.requirementsByAction(ctx)
	if err != nil {
		return employee.DigitalEmployee{}, "", err
	}
	complexity := len(plan.Actions)

	var selected employee.DigitalEmployee
	selectedPreferred := false
	selectedFound := false
	selectedReason := ""
	for _, actor := range actors {
		if !actor.Enabled || !containsString(actor.QueueMemberships, wi.QueueID) || !allowsAllActionTypes(actor, plan.Actions) {
			continue
		}
		eligible, reason, preferred, err := m.evaluateActor(ctx, actor, wi, plan, complexity, requirementsByAction)
		if err != nil {
			return employee.DigitalEmployee{}, "", err
		}
		if !eligible {
			continue
		}
		if !selectedFound || (preferred && !selectedPreferred) {
			selected = actor
			selectedPreferred = preferred
			selectedFound = true
			selectedReason = reason
		}
	}
	if selectedFound {
		return selected, selectedReason, nil
	}
	return employee.DigitalEmployee{}, "", fmt.Errorf("no eligible digital employee for queue %s and work item %s with required competencies", wi.QueueID, wi.ID)
}

func (m *deterministicMatcher) evaluateActor(ctx context.Context, actor employee.DigitalEmployee, wi workplan.WorkItem, plan actionplan.ActionPlan, complexity int, requirementsByAction map[actionplan.ActionType]CapabilityRequirement) (bool, string, bool, error) {
	capabilitiesByCode, err := m.actorCapabilitiesByCode(ctx, actor.ID)
	if err != nil {
		return false, "", false, err
	}
	for _, action := range plan.Actions {
		req, ok := requirementsByAction[action.Type]
		if !ok {
			return false, "", false, nil
		}
		for _, code := range req.CapabilityCodes {
			level, ok := capabilitiesByCode[code]
			if !ok || level < req.MinimumLevel {
				return false, "", false, nil
			}
		}
	}
	preferred := false
	reasonParts := []string{fmt.Sprintf("selected actor %s for queue %s by deterministic competency match", actor.ID, wi.QueueID)}
	if m.profiles != nil {
		profile, ok, err := m.profiles.GetProfileByActor(ctx, actor.ID)
		if err != nil {
			return false, "", false, err
		}
		if ok {
			if profile.MaxComplexity > 0 && profile.MaxComplexity < complexity {
				return false, "", false, nil
			}
			preferred = containsString(profile.PreferredWorkKinds, wi.Type)
			reasonParts = append(reasonParts, fmt.Sprintf("profile %s supports complexity %d", profile.ID, complexity))
			if preferred {
				reasonParts = append(reasonParts, fmt.Sprintf("preferred work kind %s matched", wi.Type))
			}
		}
	}
	return true, strings.Join(reasonParts, "; "), preferred, nil
}

func (m *deterministicMatcher) requirementsByAction(ctx context.Context) (map[actionplan.ActionType]CapabilityRequirement, error) {
	requirements, err := m.requirements.ListRequirements(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[actionplan.ActionType]CapabilityRequirement, len(requirements))
	for _, req := range requirements {
		out[req.ActionType] = req
	}
	return out, nil
}

func (m *deterministicMatcher) actorCapabilitiesByCode(ctx context.Context, actorID string) (map[string]int, error) {
	assigned, err := m.assignments.ListByActor(ctx, actorID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(assigned))
	for _, assignment := range assigned {
		capabilityDef, ok, err := m.capabilities.GetCapability(ctx, assignment.CapabilityID)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		level := capabilityDef.Level
		if assignment.Level > level {
			level = assignment.Level
		}
		out[capabilityDef.Code] = level
	}
	return out, nil
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
