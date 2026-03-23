package trust

import (
	"context"
	"time"
)

type TrustLevel string

const (
	TrustLow    TrustLevel = "low"
	TrustMedium TrustLevel = "medium"
	TrustHigh   TrustLevel = "high"
)

type AutonomyTier string

const (
	AutonomyRestricted AutonomyTier = "restricted"
	AutonomySupervised AutonomyTier = "supervised"
	AutonomyStandard   AutonomyTier = "standard"
)

type TrustMetrics struct {
	SuccessCount      int
	FailureCount      int
	CompensationCount int
}

type TrustProfile struct {
	ActorID               string
	Metrics               TrustMetrics
	CompletedExecutions   int
	FailedExecutions      int
	CompensatedExecutions int
	ApprovalRequests      int
	ApprovedExecutions    int
	TrustLevel            TrustLevel
	AutonomyTier          AutonomyTier
	UpdatedAt             time.Time
}

type ExecutionOutcome struct {
	ActorID          string
	ExecutionID      string
	Succeeded        bool
	Compensated      bool
	RequiredApproval bool
	Approved         bool
}

type Repository interface {
	Save(ctx context.Context, p TrustProfile) error
	GetByActor(ctx context.Context, actorID string) (TrustProfile, bool, error)
	List(ctx context.Context) ([]TrustProfile, error)
}

type Scorer interface {
	Score(current TrustProfile, outcome ExecutionOutcome) TrustProfile
}

type Service interface {
	RecordOutcome(ctx context.Context, outcome ExecutionOutcome) (TrustProfile, error)
	GetTrustProfile(ctx context.Context, actorID string) (TrustProfile, bool, error)
}
