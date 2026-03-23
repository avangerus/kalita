package employee

import (
	"context"
	"fmt"

	"kalita/internal/actionplan"
	"kalita/internal/workplan"
)

type ActorMatcher interface {
	MatchActor(ctx context.Context, wi workplan.WorkItem, plan actionplan.ActionPlan, actors []DigitalEmployee) (DigitalEmployee, string, error)
}

type deterministicSelector struct {
	directory Directory
	matcher   ActorMatcher
}

func NewSelector(directory Directory) Selector { return &deterministicSelector{directory: directory} }

func NewSelectorWithMatcher(directory Directory, matcher ActorMatcher) Selector {
	return &deterministicSelector{directory: directory, matcher: matcher}
}

func NewSelectorWithActorMatcher(directory Directory, matcher ActorMatcher) Selector {
	return NewSelectorWithMatcher(directory, matcher)
}

func (s *deterministicSelector) SelectForWorkItem(ctx context.Context, wi workplan.WorkItem, plan actionplan.ActionPlan) (DigitalEmployee, string, error) {
	if s.directory == nil {
		return DigitalEmployee{}, "", fmt.Errorf("employee directory is nil")
	}
	employees, err := s.directory.ListEmployeesByQueue(ctx, wi.QueueID)
	if err != nil {
		return DigitalEmployee{}, "", err
	}
	if s.matcher != nil {
		return s.matcher.MatchActor(ctx, wi, plan, employees)
	}
	for _, employee := range employees {
		if !employee.Enabled {
			continue
		}
		if !containsString(employee.QueueMemberships, wi.QueueID) {
			continue
		}
		if !allowsAllActionTypes(employee, plan.Actions) {
			continue
		}
		reason := fmt.Sprintf("selected employee %s for queue %s by deterministic directory order", employee.ID, wi.QueueID)
		return employee, reason, nil
	}
	return DigitalEmployee{}, "", fmt.Errorf("no eligible digital employee for queue %s and work item %s", wi.QueueID, wi.ID)
}

func allowsAllActionTypes(employee DigitalEmployee, actions []actionplan.Action) bool {
	allowed := make(map[actionplan.ActionType]struct{}, len(employee.AllowedActionTypes))
	for _, actionType := range employee.AllowedActionTypes {
		allowed[actionType] = struct{}{}
	}
	for _, action := range actions {
		if _, ok := allowed[action.Type]; !ok {
			return false
		}
	}
	return true
}
