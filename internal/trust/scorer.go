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
	} else {
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

	updated.TrustLevel, updated.AutonomyTier = deriveTrust(updated)
	updated.UpdatedAt = s.now()
	return updated
}

func deriveTrust(profile TrustProfile) (TrustLevel, AutonomyTier) {
	trustLevel := TrustLow
	autonomyTier := AutonomyRestricted

	if profile.CompletedExecutions >= 3 && profile.FailedExecutions == 0 {
		trustLevel = TrustMedium
		autonomyTier = AutonomySupervised
	}
	if profile.CompletedExecutions >= 10 && profile.FailedExecutions <= 1 && profile.CompensatedExecutions == 0 {
		trustLevel = TrustHigh
		autonomyTier = AutonomyStandard
	}
	if profile.FailedExecutions >= 2 {
		trustLevel = TrustLow
		autonomyTier = AutonomyRestricted
	}

	return trustLevel, autonomyTier
}
