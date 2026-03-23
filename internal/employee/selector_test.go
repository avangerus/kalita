package employee

import (
	"context"
	"strings"
	"testing"

	"kalita/internal/actionplan"
	"kalita/internal/workplan"
)

func TestSelectorSelectsEnabledEmployeeInMatchingQueue(t *testing.T) {
	t.Parallel()
	directory := NewInMemoryDirectory()
	_ = directory.SaveEmployee(context.Background(), DigitalEmployee{ID: "emp-1", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}})
	selector := NewSelector(directory)
	emp, reason, err := selector.SelectForWorkItem(context.Background(), workplan.WorkItem{ID: "work-1", QueueID: "q-1"}, actionplan.ActionPlan{Actions: []actionplan.Action{{ID: "a-1", Type: "legacy_workflow_action"}}})
	if err != nil {
		t.Fatalf("SelectForWorkItem error = %v", err)
	}
	if emp.ID != "emp-1" || strings.TrimSpace(reason) == "" {
		t.Fatalf("emp=%#v reason=%q", emp, reason)
	}
}

func TestSelectorRejectsDisabledEmployee(t *testing.T) {
	t.Parallel()
	directory := NewInMemoryDirectory()
	_ = directory.SaveEmployee(context.Background(), DigitalEmployee{ID: "emp-1", Enabled: false, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}})
	selector := NewSelector(directory)
	_, _, err := selector.SelectForWorkItem(context.Background(), workplan.WorkItem{ID: "work-1", QueueID: "q-1"}, actionplan.ActionPlan{Actions: []actionplan.Action{{ID: "a-1", Type: "legacy_workflow_action"}}})
	if err == nil {
		t.Fatal("expected error for disabled employee")
	}
}

func TestSelectorRejectsEmployeeMissingRequiredActionType(t *testing.T) {
	t.Parallel()
	directory := NewInMemoryDirectory()
	_ = directory.SaveEmployee(context.Background(), DigitalEmployee{ID: "emp-1", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"other_action"}})
	selector := NewSelector(directory)
	_, _, err := selector.SelectForWorkItem(context.Background(), workplan.WorkItem{ID: "work-1", QueueID: "q-1"}, actionplan.ActionPlan{Actions: []actionplan.Action{{ID: "a-1", Type: "legacy_workflow_action"}}})
	if err == nil {
		t.Fatal("expected error for missing action type")
	}
}

func TestSelectorUsesDeterministicInsertionOrder(t *testing.T) {
	t.Parallel()
	directory := NewInMemoryDirectory()
	_ = directory.SaveEmployee(context.Background(), DigitalEmployee{ID: "emp-1", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}})
	_ = directory.SaveEmployee(context.Background(), DigitalEmployee{ID: "emp-2", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}})
	selector := NewSelector(directory)
	emp, _, err := selector.SelectForWorkItem(context.Background(), workplan.WorkItem{ID: "work-1", QueueID: "q-1"}, actionplan.ActionPlan{Actions: []actionplan.Action{{ID: "a-1", Type: "legacy_workflow_action"}}})
	if err != nil {
		t.Fatalf("SelectForWorkItem error = %v", err)
	}
	if emp.ID != "emp-1" {
		t.Fatalf("selected employee = %#v", emp)
	}
}
