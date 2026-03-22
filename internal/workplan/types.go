package workplan

import (
	"context"
	"time"

	"kalita/internal/actionplan"
	"kalita/internal/caseruntime"
)

type WorkItemStatus string

const (
	WorkItemOpen WorkItemStatus = "open"
	WorkItemDone WorkItemStatus = "done"
)

type WorkItem struct {
	ID                 string
	CaseID             string
	QueueID            string
	Type               string
	Status             string
	Priority           string
	Reason             string
	AssignedEmployeeID string
	PlanID             string
	DueAt              *time.Time
	ActionPlan         *actionplan.ActionPlan
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type WorkQueue struct {
	ID                 string
	Name               string
	Department         string
	Purpose            string
	AllowedCaseKinds   []string
	DefaultEmployeeIDs []string
	PolicyRef          string
}

type QueueRepository interface {
	SaveQueue(ctx context.Context, q WorkQueue) error
	GetQueue(ctx context.Context, id string) (WorkQueue, bool, error)
	ListQueues(ctx context.Context) ([]WorkQueue, error)

	SaveWorkItem(ctx context.Context, wi WorkItem) error
	GetWorkItem(ctx context.Context, id string) (WorkItem, bool, error)
	ListWorkItemsByCase(ctx context.Context, caseID string) ([]WorkItem, error)
	ListWorkItemsByQueue(ctx context.Context, queueID string) ([]WorkItem, error)
}

type AssignmentRouter interface {
	RouteCase(ctx context.Context, c caseruntime.Case) (WorkQueue, error)
}
