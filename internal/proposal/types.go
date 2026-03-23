package proposal

import (
	"context"
	"time"

	"kalita/internal/actionplan"
	"kalita/internal/employee"
	"kalita/internal/workplan"
)

type ProposalType string

const (
	ProposalTypeActionIntent ProposalType = "action_intent"
)

type ProposalStatus string

const (
	ProposalDraft     ProposalStatus = "draft"
	ProposalValidated ProposalStatus = "validated"
	ProposalRejected  ProposalStatus = "rejected"
	ProposalCompiled  ProposalStatus = "compiled"
)

type Proposal struct {
	ID     string
	Type   ProposalType
	Status ProposalStatus

	ActorID      string
	CaseID       string
	WorkItemID   string
	AssignmentID string

	Payload       map[string]any
	Justification string

	CreatedAt time.Time
	UpdatedAt time.Time

	RejectionReason string
	ActionPlanID    string
}

type Repository interface {
	Save(ctx context.Context, p Proposal) error
	Get(ctx context.Context, id string) (Proposal, bool, error)
	ListByWorkItem(ctx context.Context, workItemID string) ([]Proposal, error)
	ListByActor(ctx context.Context, actorID string) ([]Proposal, error)
}

type Validator interface {
	Validate(ctx context.Context, p Proposal, actor employee.DigitalEmployee) (ProposalStatus, string, error)
}

type CompilerAdapter interface {
	CompileToActionPlan(ctx context.Context, p Proposal) (actionplan.ActionPlan, error)
}

type Service interface {
	CreateProposal(ctx context.Context, actor employee.DigitalEmployee, wi workplan.WorkItem, assignment employee.Assignment, payload map[string]any, justification string) (Proposal, error)
	ValidateProposal(ctx context.Context, proposalID string, actor employee.DigitalEmployee) (Proposal, error)
	CompileProposal(ctx context.Context, proposalID string) (Proposal, actionplan.ActionPlan, error)
}
