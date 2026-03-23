package trust

import "time"

type DeterministicScorer struct {
	now func() time.Time
}

func NewDeterministicScorer(now func() time.Time) *DeterministicScorer {
	if now == nil {
		now = time.Now
	}
	return &DeterministicScorer{now: now}
}

func DefaultTrustProfile(actorID string, now time.Time) TrustProfile {
	return TrustProfile{
		ActorID:      actorID,
		TrustLevel:   TrustLow,
		AutonomyTier: AutonomyRestricted,
		UpdatedAt:    now,
	}
}

func (s *DeterministicScorer) Score(current TrustProfile, outcome ExecutionOutcome) TrustProfile {
	updated := current
	if updated.ActorID == "" {
		updated = DefaultTrustProfile(outcome.ActorID, s.now())
	}

	if outcome.Succeeded {
		updated.CompletedExecutions++
	}
	if !outcome.Succeeded {
		updated.FailedExecutions++
	}
	if outcome.Compensated {
		updated.CompensatedExecutions++
	}
	if outcome.RequiredApproval {
		updated.ApprovalRequests++
	}
	if outcome.Approved {
		updated.ApprovedExecutions++
	}

	updated.TrustLevel = TrustLow
	updated.AutonomyTier = AutonomyRestricted

	if updated.CompletedExecutions >= 3 && updated.FailedExecutions == 0 {
		updated.TrustLevel = TrustMedium
		updated.AutonomyTier = AutonomySupervised
	}
	if updated.CompletedExecutions >= 10 && updated.FailedExecutions <= 1 && updated.CompensatedExecutions == 0 {
		updated.TrustLevel = TrustHigh
		updated.AutonomyTier = AutonomyStandard
	}
	if updated.FailedExecutions >= 2 {
		updated.TrustLevel = TrustLow
		updated.AutonomyTier = AutonomyRestricted
	}
	if updated.CompensatedExecutions >= 1 && updated.TrustLevel == TrustHigh {
		updated.TrustLevel = TrustMedium
		updated.AutonomyTier = AutonomySupervised
		if updated.FailedExecutions > 0 || updated.CompletedExecutions < 3 {
			updated.TrustLevel = TrustLow
			updated.AutonomyTier = AutonomyRestricted
		}
	}

	updated.UpdatedAt = s.now()
	return updated
}
