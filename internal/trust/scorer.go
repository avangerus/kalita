package trust

import "time"

type deterministicScorer struct {
	now func() time.Time
}

func NewScorer() Scorer {
	return NewScorerWithClock(func() time.Time { return time.Now().UTC() })
}

func NewScorerWithClock(now func() time.Time) Scorer {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &deterministicScorer{now: now}
}

func (s *deterministicScorer) Score(current TrustProfile, outcome ExecutionOutcome) TrustProfile {
	next := current
	if next.ActorID == "" {
		next.ActorID = outcome.ActorID
	}
	if outcome.Succeeded {
		next.CompletedExecutions++
	}
	if !outcome.Succeeded {
		next.FailedExecutions++
	}
	if outcome.Compensated {
		next.CompensatedExecutions++
	}
	if outcome.RequiredApproval {
		next.ApprovalRequests++
	}
	if outcome.Approved {
		next.ApprovedExecutions++
	}

	next.TrustLevel, next.AutonomyTier = deriveTrust(next)
	next.UpdatedAt = s.now()
	return next
}

func deriveTrust(profile TrustProfile) (TrustLevel, AutonomyTier) {
	if profile.CompletedExecutions >= 10 && profile.FailedExecutions <= 1 && profile.CompensatedExecutions == 0 {
		return TrustHigh, AutonomyStandard
	}
	if profile.CompletedExecutions >= 3 && profile.FailedExecutions == 0 {
		return TrustMedium, AutonomySupervised
	}
	return TrustLow, AutonomyRestricted
}
