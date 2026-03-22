package workplan

import (
	"context"
	"time"
)

type CoordinationOutcome string

const (
	CoordinationSelected  CoordinationOutcome = "selected"
	CoordinationDeferred  CoordinationOutcome = "deferred"
	CoordinationBlocked   CoordinationOutcome = "blocked"
	CoordinationEscalated CoordinationOutcome = "escalated"
)

type CoordinationDecision struct {
	ID         string
	CaseID     string
	WorkItemID string
	QueueID    string
	Strategy   string
	SelectedBy string
	Outcome    CoordinationOutcome
	Reason     string
	CreatedAt  time.Time
}

type CoordinationRepository interface {
	SaveDecision(ctx context.Context, d CoordinationDecision) error
	GetDecision(ctx context.Context, id string) (CoordinationDecision, bool, error)
	ListByWorkItem(ctx context.Context, workItemID string) ([]CoordinationDecision, error)
	ListByCase(ctx context.Context, caseID string) ([]CoordinationDecision, error)
	ListByQueue(ctx context.Context, queueID string) ([]CoordinationDecision, error)
}

type Coordinator interface {
	CoordinateWorkItem(ctx context.Context, wi WorkItem) (CoordinationDecision, error)
}
