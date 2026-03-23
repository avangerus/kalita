package trust

import (
	"testing"
	"time"
)

func TestScorerInitialSuccessProgression(t *testing.T) {
	t.Parallel()
	base := time.Unix(100, 0).UTC()
	scorer := NewScorerWithClock(func() time.Time { return base })
	profile := TrustProfile{ActorID: "actor-1", TrustLevel: TrustLow, AutonomyTier: AutonomyRestricted}
	for range 3 {
		profile = scorer.Score(profile, ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec", Succeeded: true})
	}
	if profile.CompletedExecutions != 3 {
		t.Fatalf("completed=%d", profile.CompletedExecutions)
	}
	if profile.TrustLevel != TrustMedium || profile.AutonomyTier != AutonomySupervised {
		t.Fatalf("profile=%#v", profile)
	}
	for range 7 {
		profile = scorer.Score(profile, ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec", Succeeded: true})
	}
	if profile.TrustLevel != TrustHigh || profile.AutonomyTier != AutonomyStandard {
		t.Fatalf("profile=%#v", profile)
	}
	if !profile.UpdatedAt.Equal(base) {
		t.Fatalf("updatedAt=%v", profile.UpdatedAt)
	}
}

func TestScorerFailureDowngradesTrust(t *testing.T) {
	t.Parallel()
	scorer := NewScorerWithClock(func() time.Time { return time.Unix(200, 0).UTC() })
	profile := TrustProfile{ActorID: "actor-1", CompletedExecutions: 3, TrustLevel: TrustMedium, AutonomyTier: AutonomySupervised}
	profile = scorer.Score(profile, ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec-4", Succeeded: false})
	if profile.FailedExecutions != 1 {
		t.Fatalf("failed=%d", profile.FailedExecutions)
	}
	if profile.TrustLevel != TrustLow || profile.AutonomyTier != AutonomyRestricted {
		t.Fatalf("profile=%#v", profile)
	}
}

func TestScorerCompensationPreventsHighTrust(t *testing.T) {
	t.Parallel()
	scorer := NewScorerWithClock(func() time.Time { return time.Unix(300, 0).UTC() })
	profile := TrustProfile{ActorID: "actor-1"}
	for i := 0; i < 10; i++ {
		outcome := ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec", Succeeded: true}
		if i == 4 {
			outcome.Compensated = true
		}
		profile = scorer.Score(profile, outcome)
	}
	if profile.CompletedExecutions != 10 || profile.CompensatedExecutions != 1 {
		t.Fatalf("profile=%#v", profile)
	}
	if profile.TrustLevel != TrustMedium || profile.AutonomyTier != AutonomySupervised {
		t.Fatalf("profile=%#v", profile)
	}
}

func TestScorerUpdatesApprovalCounters(t *testing.T) {
	t.Parallel()
	scorer := NewScorerWithClock(func() time.Time { return time.Unix(400, 0).UTC() })
	profile := scorer.Score(TrustProfile{ActorID: "actor-1"}, ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec-1", Succeeded: true, RequiredApproval: true, Approved: true})
	if profile.ApprovalRequests != 1 || profile.ApprovedExecutions != 1 {
		t.Fatalf("profile=%#v", profile)
	}
}
