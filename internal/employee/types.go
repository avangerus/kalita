package employee

import (
	"context"
	"time"

	"kalita/internal/actionplan"
	"kalita/internal/executioncontrol"
	"kalita/internal/executionruntime"
	"kalita/internal/workplan"
)

type DigitalEmployee struct {
	ID                  string
	Code                string
	Role                string
	Enabled             bool
	QueueMemberships    []string
	AllowedActionTypes  []actionplan.ActionType
	AllowedCommandTypes []string
	PolicyProfile       string
	ExecutionProfile    string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type Assignment struct {
	ID         string
	WorkItemID string
	CaseID     string
	QueueID    string
	EmployeeID string
	AssignedAt time.Time
	Reason     string
}

type Directory interface {
	SaveEmployee(ctx context.Context, e DigitalEmployee) error
	GetEmployee(ctx context.Context, id string) (DigitalEmployee, bool, error)
	ListEmployees(ctx context.Context) ([]DigitalEmployee, error)
	ListEmployeesByQueue(ctx context.Context, queueID string) ([]DigitalEmployee, error)
}

type AssignmentRepository interface {
	SaveAssignment(ctx context.Context, a Assignment) error
	GetAssignment(ctx context.Context, id string) (Assignment, bool, error)
	ListAssignmentsByWorkItem(ctx context.Context, workItemID string) ([]Assignment, error)
	ListAssignmentsByEmployee(ctx context.Context, employeeID string) ([]Assignment, error)
}

type Selector interface {
	SelectForWorkItem(ctx context.Context, wi workplan.WorkItem, plan actionplan.ActionPlan) (DigitalEmployee, string, error)
}

type Service interface {
	AssignAndStartExecution(ctx context.Context, wi workplan.WorkItem, plan actionplan.ActionPlan, constraints executioncontrol.ExecutionConstraints, metadata RunMetadata) (Assignment, executionruntime.ExecutionSession, error)
}

type RunMetadata struct {
	CaseID                 string
	QueueID                string
	CoordinationDecisionID string
	PolicyDecisionID       string
}
