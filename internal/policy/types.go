package policy

import (
	"context"
	"time"

	"kalita/internal/workplan"
)

type PolicyOutcome string

const (
	PolicyAllow           PolicyOutcome = "allow"
	PolicyRequireApproval PolicyOutcome = "require_approval"
	PolicyDeny            PolicyOutcome = "deny"
)

type PolicyDecision struct {
	ID                     string
	CoordinationDecisionID string
	CaseID                 string
	WorkItemID             string
	QueueID                string
	Outcome                PolicyOutcome
	Reason                 string
	CreatedAt              time.Time
}

type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
)

type ApprovalRequest struct {
	ID                     string
	CoordinationDecisionID string
	PolicyDecisionID       string
	CaseID                 string
	WorkItemID             string
	QueueID                string
	Status                 ApprovalStatus
	RequestedFromRole      string
	CreatedAt              time.Time
	ResolvedAt             *time.Time
	ResolutionNote         string
}

type PolicyRepository interface {
	SaveDecision(ctx context.Context, d PolicyDecision) error
	GetDecision(ctx context.Context, id string) (PolicyDecision, bool, error)
	ListByCoordinationDecision(ctx context.Context, coordinationDecisionID string) ([]PolicyDecision, error)

	SaveApprovalRequest(ctx context.Context, r ApprovalRequest) error
	GetApprovalRequest(ctx context.Context, id string) (ApprovalRequest, bool, error)
	ListApprovalRequestsByCoordinationDecision(ctx context.Context, coordinationDecisionID string) ([]ApprovalRequest, error)
}

type Evaluator interface {
	EvaluateCoordinationDecision(ctx context.Context, d workplan.CoordinationDecision) (PolicyOutcome, string, error)
}

type Service interface {
	EvaluateAndRecord(ctx context.Context, d workplan.CoordinationDecision) (PolicyDecision, *ApprovalRequest, error)
}
