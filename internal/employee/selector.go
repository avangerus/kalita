package employee

import (
	"context"
	"fmt"

	"kalita/internal/actionplan"
	"kalita/internal/workplan"
)

type deterministicSelector struct{ directory Directory }

func NewSelector(directory Directory) Selector { return &deterministicSelector{directory: directory} }

func (s *deterministicSelector) SelectForWorkItem(ctx context.Context, wi workplan.WorkItem, plan actionplan.ActionPlan) (DigitalEmployee, string, error) {
	if s.directory == nil {
		return DigitalEmployee{}, "", fmt.Errorf("employee directory is nil")
	}
	employees, err := s.directory.ListEmployeesByQueue(ctx, wi.QueueID)
	if err != nil {
		return DigitalEmployee{}, "", err
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
