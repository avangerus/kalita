package employee

import (
	"context"
	"sync"

	"kalita/internal/actionplan"
)

type InMemoryDirectory struct {
	mu              sync.RWMutex
	employeesByID   map[string]DigitalEmployee
	employeeOrder   []string
	employeeByQueue map[string][]string
}

func NewInMemoryDirectory() *InMemoryDirectory {
	return &InMemoryDirectory{employeesByID: map[string]DigitalEmployee{}, employeeByQueue: map[string][]string{}}
}

func (r *InMemoryDirectory) SaveEmployee(_ context.Context, e DigitalEmployee) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.employeesByID[e.ID]; ok {
		for _, queueID := range existing.QueueMemberships {
			r.employeeByQueue[queueID] = removeString(r.employeeByQueue[queueID], e.ID)
		}
	} else {
		r.employeeOrder = append(r.employeeOrder, e.ID)
	}
	r.employeesByID[e.ID] = cloneEmployee(e)
	for _, queueID := range e.QueueMemberships {
		if !containsString(r.employeeByQueue[queueID], e.ID) {
			r.employeeByQueue[queueID] = append(r.employeeByQueue[queueID], e.ID)
		}
	}
	return nil
}

func (r *InMemoryDirectory) GetEmployee(_ context.Context, id string) (DigitalEmployee, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.employeesByID[id]
	if !ok {
		return DigitalEmployee{}, false, nil
	}
	return cloneEmployee(e), true, nil
}

func (r *InMemoryDirectory) ListEmployees(_ context.Context) ([]DigitalEmployee, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]DigitalEmployee, 0, len(r.employeeOrder))
	for _, id := range r.employeeOrder {
		out = append(out, cloneEmployee(r.employeesByID[id]))
	}
	return out, nil
}

func (r *InMemoryDirectory) ListEmployeesByQueue(_ context.Context, queueID string) ([]DigitalEmployee, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.employeeByQueue[queueID]
	out := make([]DigitalEmployee, 0, len(ids))
	for _, id := range ids {
		out = append(out, cloneEmployee(r.employeesByID[id]))
	}
	return out, nil
}

type InMemoryAssignmentRepository struct {
	mu                 sync.RWMutex
	assignmentsByID    map[string]Assignment
	assignmentOrder    []string
	assignmentsByWork  map[string][]string
	assignmentsByEmpID map[string][]string
}

func NewInMemoryAssignmentRepository() *InMemoryAssignmentRepository {
	return &InMemoryAssignmentRepository{assignmentsByID: map[string]Assignment{}, assignmentsByWork: map[string][]string{}, assignmentsByEmpID: map[string][]string{}}
}

func (r *InMemoryAssignmentRepository) SaveAssignment(_ context.Context, a Assignment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.assignmentsByID[a.ID]; ok {
		if existing.WorkItemID != a.WorkItemID {
			r.assignmentsByWork[existing.WorkItemID] = removeString(r.assignmentsByWork[existing.WorkItemID], a.ID)
		}
		if existing.EmployeeID != a.EmployeeID {
			r.assignmentsByEmpID[existing.EmployeeID] = removeString(r.assignmentsByEmpID[existing.EmployeeID], a.ID)
		}
	} else {
		r.assignmentOrder = append(r.assignmentOrder, a.ID)
	}
	r.assignmentsByID[a.ID] = a
	if !containsString(r.assignmentsByWork[a.WorkItemID], a.ID) {
		r.assignmentsByWork[a.WorkItemID] = append(r.assignmentsByWork[a.WorkItemID], a.ID)
	}
	if !containsString(r.assignmentsByEmpID[a.EmployeeID], a.ID) {
		r.assignmentsByEmpID[a.EmployeeID] = append(r.assignmentsByEmpID[a.EmployeeID], a.ID)
	}
	return nil
}

func (r *InMemoryAssignmentRepository) GetAssignment(_ context.Context, id string) (Assignment, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.assignmentsByID[id]
	if !ok {
		return Assignment{}, false, nil
	}
	return a, true, nil
}

func (r *InMemoryAssignmentRepository) ListAssignmentsByWorkItem(_ context.Context, workItemID string) ([]Assignment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.assignmentsByWork[workItemID]
	out := make([]Assignment, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.assignmentsByID[id])
	}
	return out, nil
}

func (r *InMemoryAssignmentRepository) ListAssignmentsByEmployee(_ context.Context, employeeID string) ([]Assignment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.assignmentsByEmpID[employeeID]
	out := make([]Assignment, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.assignmentsByID[id])
	}
	return out, nil
}

func cloneEmployee(e DigitalEmployee) DigitalEmployee {
	out := e
	out.QueueMemberships = append([]string(nil), e.QueueMemberships...)
	out.AllowedActionTypes = append([]actionplan.ActionType(nil), e.AllowedActionTypes...)
	out.AllowedCommandTypes = append([]string(nil), e.AllowedCommandTypes...)
	return out
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func removeString(items []string, target string) []string {
	out := items[:0]
	for _, item := range items {
		if item != target {
			out = append(out, item)
		}
	}
	return out
}

func (r *InMemoryAssignmentRepository) ListAssignments(_ context.Context) ([]Assignment, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Assignment, 0, len(r.assignmentOrder))
	for _, id := range r.assignmentOrder {
		out = append(out, r.assignmentsByID[id])
	}
	return out, nil
}
