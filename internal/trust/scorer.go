package trust

import "time"

type DeterministicScorer struct {
	now func() time.Time
}

func NewScorer() Scorer {
	return NewScorerWithClock(func() time.Time { return time.Now().UTC() })
}

func NewScorerWithClock(now func() time.Time) Scorer {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &DeterministicScorer{now: now}
}

func NewDeterministicScorer(now func() time.Time) *DeterministicScorer {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &DeterministicScorer{now: now}
}

func DefaultTrustProfile(actorID string, now time.Time) TrustProfile {
	profile := TrustProfile{ActorID: actorID, UpdatedAt: now}
	profile.TrustLevel, profile.AutonomyTier = deriveTrust(profile)
	return profile
}

func (s *DeterministicScorer) Score(current TrustProfile, outcome ExecutionOutcome) TrustProfile {
	updated := current
	if updated.ActorID == "" {
		updated = DefaultTrustProfile(outcome.ActorID, s.now())
	}
	updated = withNormalizedMetrics(updated)

	if outcome.Succeeded {
		updated.Metrics.SuccessCount++
		updated.CompletedExecutions++
	} else {
		updated.Metrics.FailureCount++
		updated.FailedExecutions++
	}
	if outcome.Compensated {
		updated.Metrics.CompensationCount++
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
	profile = withNormalizedMetrics(profile)
	level := TrustLow
	if profile.Metrics.SuccessCount >= 6 {
		level = TrustHigh
	} else if profile.Metrics.SuccessCount >= 3 {
		level = TrustMedium
	}
	if profile.Metrics.FailureCount >= 2 {
		level = downgradeTrust(level)
	}
	if profile.Metrics.CompensationCount >= 1 {
		level = downgradeTrust(level)
	}
	return level, autonomyForTrust(level)
}

func withNormalizedMetrics(profile TrustProfile) TrustProfile {
	if profile.Metrics.SuccessCount < profile.CompletedExecutions {
		profile.Metrics.SuccessCount = profile.CompletedExecutions
	}
	if profile.Metrics.FailureCount < profile.FailedExecutions {
		profile.Metrics.FailureCount = profile.FailedExecutions
	}
	if profile.Metrics.CompensationCount < profile.CompensatedExecutions {
		profile.Metrics.CompensationCount = profile.CompensatedExecutions
	}
	return profile
}

func downgradeTrust(level TrustLevel) TrustLevel {
	switch level {
	case TrustHigh:
		return TrustMedium
	case TrustMedium:
		return TrustLow
	default:
		return TrustLow
	}
}

func autonomyForTrust(level TrustLevel) AutonomyTier {
	switch level {
	case TrustHigh:
		return AutonomyStandard
	case TrustMedium:
		return AutonomySupervised
	default:
		return AutonomyRestricted
	}
}
