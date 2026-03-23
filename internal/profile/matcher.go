package profile

import (
	"context"
	"fmt"
	"strings"

	"kalita/internal/actionplan"
	"kalita/internal/capability"
	"kalita/internal/employee"
	"kalita/internal/trust"
	"kalita/internal/workplan"
)

type deterministicMatcher struct {
	requirements RequirementRepository
	profiles     Repository
	capabilities capability.CapabilityRepository
	assignments  capability.ActorCapabilityRepository
	trust        trust.Service
}

type evaluatedActor struct {
	actor      employee.DigitalEmployee
	reason     string
	preferred  bool
	trustLevel trust.TrustLevel
	index      int
}

func NewMatcher(requirements RequirementRepository, profiles Repository, capabilities capability.CapabilityRepository, assignments capability.ActorCapabilityRepository, trustServices ...trust.Service) Matcher {
	var trustService trust.Service
	if len(trustServices) > 0 {
		trustService = trustServices[0]
	}
	return &deterministicMatcher{requirements: requirements, profiles: profiles, capabilities: capabilities, assignments: assignments, trust: trustService}
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

	rejected := make([]string, 0, len(actors))
	eligibleActors := make([]evaluatedActor, 0, len(actors))
	for idx, actor := range actors {
		if !actor.Enabled {
			rejected = append(rejected, fmt.Sprintf("%s rejected: disabled", actor.ID))
			continue
		}
		if !containsString(actor.QueueMemberships, wi.QueueID) {
			rejected = append(rejected, fmt.Sprintf("%s rejected: not in queue %s", actor.ID, wi.QueueID))
			continue
		}
		if !allowsAllActionTypes(actor, plan.Actions) {
			rejected = append(rejected, fmt.Sprintf("%s rejected: action types not allowed", actor.ID))
			continue
		}
		eligible, reason, preferred, trustLevel, err := m.evaluateActor(ctx, actor, wi, plan, complexity, requirementsByAction)
		if err != nil {
			return employee.DigitalEmployee{}, "", err
		}
		if !eligible {
			rejected = append(rejected, fmt.Sprintf("%s rejected: %s", actor.ID, reason))
			continue
		}
		eligibleActors = append(eligibleActors, evaluatedActor{actor: actor, reason: reason, preferred: preferred, trustLevel: trustLevel, index: idx})
	}
	if len(eligibleActors) > 0 {
		selected := eligibleActors[0]
		for _, candidate := range eligibleActors[1:] {
			if outranks(candidate, selected) {
				rejected = append(rejected, fmt.Sprintf("%s eligible but not selected: lower priority than %s", selected.actor.ID, candidate.actor.ID))
				selected = candidate
				continue
			}
			rejected = append(rejected, fmt.Sprintf("%s eligible but not selected: lower priority than %s", candidate.actor.ID, selected.actor.ID))
		}
		reason := selected.reason
		if len(rejected) > 0 {
			reason += "; others not chosen: " + strings.Join(rejected, "; ")
		}
		return selected.actor, reason, nil
	}
	return employee.DigitalEmployee{}, "", fmt.Errorf("no eligible digital employee for queue %s and work item %s with required competencies", wi.QueueID, wi.ID)
}

func (m *deterministicMatcher) evaluateActor(ctx context.Context, actor employee.DigitalEmployee, wi workplan.WorkItem, plan actionplan.ActionPlan, complexity int, requirementsByAction map[actionplan.ActionType]CapabilityRequirement) (bool, string, bool, trust.TrustLevel, error) {
	capabilitiesByCode, err := m.actorCapabilitiesByCode(ctx, actor.ID)
	if err != nil {
		return false, "", false, trust.TrustLow, err
	}
	for _, action := range plan.Actions {
		req, ok := requirementsByAction[action.Type]
		if !ok {
			return false, fmt.Sprintf("missing capability requirements for action type %s", action.Type), false, trust.TrustLow, nil
		}
		for _, code := range req.CapabilityCodes {
			level, ok := capabilitiesByCode[code]
			if !ok || level < req.MinimumLevel {
				return false, fmt.Sprintf("capability %s requires level %d", code, req.MinimumLevel), false, trust.TrustLow, nil
			}
		}
	}
	preferred := false
	trustLevel := trust.TrustLow
	reasonParts := []string{fmt.Sprintf("selected actor %s for queue %s by deterministic capability/profile/trust match", actor.ID, wi.QueueID)}
	if m.profiles != nil {
		profile, ok, err := m.profiles.GetProfileByActor(ctx, actor.ID)
		if err != nil {
			return false, "", false, trust.TrustLow, err
		}
		if ok {
			if profile.MaxComplexity > 0 && profile.MaxComplexity < complexity {
				return false, fmt.Sprintf("profile %s max complexity %d below required complexity %d", profile.ID, profile.MaxComplexity, complexity), false, trust.TrustLow, nil
			}
			preferred = containsString(profile.PreferredWorkKinds, wi.Type)
			reasonParts = append(reasonParts, fmt.Sprintf("profile %s supports complexity %d", profile.ID, complexity))
			if preferred {
				reasonParts = append(reasonParts, fmt.Sprintf("preferred work kind %s matched", wi.Type))
			}
		}
	}
	if m.trust != nil {
		profile, ok, err := m.trust.GetTrustProfile(ctx, actor.ID)
		if err != nil {
			return false, "", false, trust.TrustLow, err
		}
		if ok {
			trustLevel = profile.TrustLevel
		}
	}
	reasonParts = append(reasonParts, fmt.Sprintf("trust level %s applied", trustLevel))
	return true, strings.Join(reasonParts, "; "), preferred, trustLevel, nil
}

func outranks(left, right evaluatedActor) bool {
	if trustRank(left.trustLevel) != trustRank(right.trustLevel) {
		return trustRank(left.trustLevel) > trustRank(right.trustLevel)
	}
	if left.preferred != right.preferred {
		return left.preferred
	}
	return left.index < right.index
}

func trustRank(level trust.TrustLevel) int {
	switch level {
	case trust.TrustHigh:
		return 3
	case trust.TrustMedium:
		return 2
	default:
		return 1
	}
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
