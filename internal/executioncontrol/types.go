package executioncontrol

import (
	"context"
	"time"

	"kalita/internal/policy"
	"kalita/internal/trust"
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
	ExecutionModeSupervised           ExecutionMode = "supervised"
	ExecutionModeStandard             ExecutionMode = "standard"
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

func AdjustForTrust(base ExecutionConstraints, profile trust.TrustProfile) (ExecutionConstraints, string) {
	adjusted := base

	switch profile.TrustLevel {
	case trust.TrustHigh:
		adjusted.ExecutionMode = ExecutionModeStandard
		if adjusted.MaxSteps < 10 {
			adjusted.MaxSteps = 10
		}
		if adjusted.MaxDurationSec < 300 {
			adjusted.MaxDurationSec = 300
		}
	case trust.TrustMedium:
		adjusted.ExecutionMode = ExecutionModeSupervised
		if adjusted.MaxSteps > 5 || adjusted.MaxSteps == 0 {
			adjusted.MaxSteps = 5
		}
		if adjusted.MaxDurationSec > 180 || adjusted.MaxDurationSec == 0 {
			adjusted.MaxDurationSec = 180
		}
	case trust.TrustLow:
		if adjusted.ExecutionMode != ExecutionModeGuidedUIOperator {
			adjusted.ExecutionMode = ExecutionModeApprovalEachStep
		}
		adjusted.MaxSteps = 1
		adjusted.MaxDurationSec = 60
	default:
		adjusted.ExecutionMode = ExecutionModeApprovalEachStep
		adjusted.MaxSteps = 1
		adjusted.MaxDurationSec = 60
		profile.TrustLevel = trust.TrustLow
	}

	reason := "execution constraints adjusted deterministically by trust profile"
	if base.Reason != "" {
		reason = base.Reason + "; " + reason
	}
	adjusted.Reason = reason
	return adjusted, reason
}
