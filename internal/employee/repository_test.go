package employee

import (
	"context"
	"testing"
	"time"

	"kalita/internal/actionplan"
)

func TestInMemoryDirectorySaveGetListEmployee(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryDirectory()
	now := time.Date(2026, 3, 22, 18, 0, 0, 0, time.UTC)
	emp := DigitalEmployee{ID: "emp-1", Code: "legacy-op", Role: "legacy_operator", Enabled: false, QueueMemberships: []string{"default-intake"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}, AllowedCommandTypes: []string{"workflow.action"}, CreatedAt: now, UpdatedAt: now}
	if err := repo.SaveEmployee(context.Background(), emp); err != nil {
		t.Fatalf("SaveEmployee error = %v", err)
	}
	got, ok, err := repo.GetEmployee(context.Background(), emp.ID)
	if err != nil || !ok {
		t.Fatalf("GetEmployee = %#v ok=%v err=%v", got, ok, err)
	}
	got.QueueMemberships[0] = "mutated"
	reloaded, _, _ := repo.GetEmployee(context.Background(), emp.ID)
	if reloaded.QueueMemberships[0] != "default-intake" {
		t.Fatalf("employee clone failed: %#v", reloaded)
	}
	list, err := repo.ListEmployees(context.Background())
	if err != nil {
		t.Fatalf("ListEmployees error = %v", err)
	}
	if len(list) != 1 || list[0].ID != emp.ID || list[0].Enabled {
		t.Fatalf("list = %#v", list)
	}
}

func TestInMemoryDirectoryListEmployeesByQueue(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryDirectory()
	employees := []DigitalEmployee{{ID: "emp-1", QueueMemberships: []string{"q-1"}}, {ID: "emp-2", QueueMemberships: []string{"q-1", "q-2"}}, {ID: "emp-3", QueueMemberships: []string{"q-2"}}}
	for _, employee := range employees {
		if err := repo.SaveEmployee(context.Background(), employee); err != nil {
			t.Fatalf("SaveEmployee(%s) error = %v", employee.ID, err)
		}
	}
	list, err := repo.ListEmployeesByQueue(context.Background(), "q-1")
	if err != nil {
		t.Fatalf("ListEmployeesByQueue error = %v", err)
	}
	if len(list) != 2 || list[0].ID != "emp-1" || list[1].ID != "emp-2" {
		t.Fatalf("list = %#v", list)
	}
}

func TestInMemoryAssignmentRepositorySaveGetListByWorkItemAndEmployee(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryAssignmentRepository()
	assignments := []Assignment{{ID: "asn-1", WorkItemID: "work-1", EmployeeID: "emp-1"}, {ID: "asn-2", WorkItemID: "work-1", EmployeeID: "emp-2"}, {ID: "asn-3", WorkItemID: "work-2", EmployeeID: "emp-1"}}
	for _, assignment := range assignments {
		if err := repo.SaveAssignment(context.Background(), assignment); err != nil {
			t.Fatalf("SaveAssignment(%s) error = %v", assignment.ID, err)
		}
	}
	got, ok, err := repo.GetAssignment(context.Background(), "asn-1")
	if err != nil || !ok {
		t.Fatalf("GetAssignment = %#v ok=%v err=%v", got, ok, err)
	}
	byWork, err := repo.ListAssignmentsByWorkItem(context.Background(), "work-1")
	if err != nil {
		t.Fatalf("ListAssignmentsByWorkItem error = %v", err)
	}
	if len(byWork) != 2 || byWork[0].ID != "asn-1" || byWork[1].ID != "asn-2" {
		t.Fatalf("byWork = %#v", byWork)
	}
	byEmployee, err := repo.ListAssignmentsByEmployee(context.Background(), "emp-1")
	if err != nil {
		t.Fatalf("ListAssignmentsByEmployee error = %v", err)
	}
	if len(byEmployee) != 2 || byEmployee[0].ID != "asn-1" || byEmployee[1].ID != "asn-3" {
		t.Fatalf("byEmployee = %#v", byEmployee)
	}
}
