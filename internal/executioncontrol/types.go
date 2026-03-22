package executioncontrol

import (
	"context"
	"time"

	"kalita/internal/policy"
	"kalita/internal/workplan"
)

type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

type ExecutionMode string

const (
	ExecutionModeDeterministicAPI     ExecutionMode = "deterministic_api"
	ExecutionModeGuidedUIOperator     ExecutionMode = "guided_ui_operator"
	ExecutionModeCheckpointedAutonomy ExecutionMode = "checkpointed_autonomy"
	ExecutionModeApprovalEachStep     ExecutionMode = "approval_each_step"
)

type ExecutionConstraints struct {
	ID                     string
	CoordinationDecisionID string
	PolicyDecisionID       string
	CaseID                 string
	WorkItemID             string
	QueueID                string

	RiskLevel     RiskLevel
	ExecutionMode ExecutionMode

	MaxTokens      int
	MaxCost        float64
	MaxSteps       int
	MaxDurationSec int

	AllowedScopes []string
	ForbiddenOps  []string

	CreatedAt time.Time
	Reason    string
}

type ConstraintsRepository interface {
	Save(ctx context.Context, c ExecutionConstraints) error
	Get(ctx context.Context, id string) (ExecutionConstraints, bool, error)
	ListByPolicyDecision(ctx context.Context, policyDecisionID string) ([]ExecutionConstraints, error)
	ListByCoordinationDecision(ctx context.Context, coordinationDecisionID string) ([]ExecutionConstraints, error)
	ListByCase(ctx context.Context, caseID string) ([]ExecutionConstraints, error)
}

type ConstraintsPlanner interface {
	PlanForPolicyDecision(ctx context.Context, coordination workplan.CoordinationDecision, policyDecision policy.PolicyDecision) (ExecutionConstraints, error)
}

type ConstraintsService interface {
	CreateAndRecord(ctx context.Context, coordination workplan.CoordinationDecision, policyDecision policy.PolicyDecision) (ExecutionConstraints, error)
}
