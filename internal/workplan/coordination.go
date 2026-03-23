package workplan

import (
	"context"
	"time"
)

type CoordinationDecisionType string

const (
	CoordinationExecuteNow CoordinationDecisionType = "execute_now"
	CoordinationDefer      CoordinationDecisionType = "defer"
	CoordinationEscalate   CoordinationDecisionType = "escalate"
	CoordinationBlock      CoordinationDecisionType = "block"
)

const (
	CoordinationPriorityBlock      = 1
	CoordinationPriorityDefer      = 2
	CoordinationPriorityExecuteNow = 3
	CoordinationPriorityEscalate   = 4
)

type CoordinationDecision struct {
	ID           string
	WorkItemID   string
	CaseID       string
	QueueID      string
	DecisionType CoordinationDecisionType
	Priority     int
	Reason       string
	CreatedAt    time.Time
}

type CoordinationActor struct {
	ID                 string
	Enabled            bool
	QueueMemberships   []string
	AllowedActionTypes []string
}

type CoordinationActorProfile struct {
	ActorID        string
	MaxComplexity  int
	TrustLevel     string
	TrustAvailable bool
}

type CoordinationContext struct {
	ActionTypes []string
	Complexity  int
	Actors      []CoordinationActor
	Profiles    map[string]CoordinationActorProfile
}

type CoordinationRepository interface {
	SaveDecision(ctx context.Context, d CoordinationDecision) error
	GetDecision(ctx context.Context, id string) (CoordinationDecision, bool, error)
	ListByWorkItem(ctx context.Context, workItemID string) ([]CoordinationDecision, error)
	ListByCase(ctx context.Context, caseID string) ([]CoordinationDecision, error)
	ListByQueue(ctx context.Context, queueID string) ([]CoordinationDecision, error)
}

type Coordinator interface {
	Decide(ctx context.Context, wi WorkItem, coordinationContext CoordinationContext) (CoordinationDecision, error)
}
