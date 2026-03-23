package trust

import (
	"testing"
	"time"
)

func TestDeterministicScorerFirstSuccessStaysLowUntilThreshold(t *testing.T) {
	t.Parallel()

	now := time.Unix(10, 0).UTC()
	scorer := NewDeterministicScorer(func() time.Time { return now })

	profile := scorer.Score(
		TrustProfile{ActorID: "actor-1"},
		ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec-1", Succeeded: true},
	)

	if profile.CompletedExecutions != 1 {
		t.Fatalf("CompletedExecutions = %d", profile.CompletedExecutions)
	}
	if profile.TrustLevel != TrustLow || profile.AutonomyTier != AutonomyRestricted {
		t.Fatalf("profile = %#v", profile)
	}
	if !profile.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %v", profile.UpdatedAt)
	}
}

func TestDeterministicScorerThreeSuccessesPromotesToMediumSupervised(t *testing.T) {
	t.Parallel()

	now := time.Unix(20, 0).UTC()
	scorer := NewDeterministicScorer(func() time.Time { return now })

	profile := TrustProfile{ActorID: "actor-1"}
	for i := 0; i < 3; i++ {
		profile = scorer.Score(
			profile,
			ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec", Succeeded: true},
		)
	}

	if profile.CompletedExecutions != 3 {
		t.Fatalf("CompletedExecutions = %d", profile.CompletedExecutions)
	}
	if profile.TrustLevel != TrustMedium || profile.AutonomyTier != AutonomySupervised {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestDeterministicScorerTenSuccessesWithAtMostOneFailurePromotesToHighStandard(t *testing.T) {
	t.Parallel()

	now := time.Unix(30, 0).UTC()
	scorer := NewDeterministicScorer(func() time.Time { return now })

	profile := TrustProfile{
		ActorID:             "actor-1",
		CompletedExecutions: 9,
		FailedExecutions:    1,
		TrustLevel:          TrustMedium,
		AutonomyTier:        AutonomySupervised,
	}

	profile = scorer.Score(
		profile,
		ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec-10", Succeeded: true},
	)

	if profile.TrustLevel != TrustHigh || profile.AutonomyTier != AutonomyStandard {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestDeterministicScorerCompensationBlocksHighTrust(t *testing.T) {
	t.Parallel()

	now := time.Unix(40, 0).UTC()
	scorer := NewDeterministicScorer(func() time.Time { return now })

	profile := TrustProfile{
		ActorID:             "actor-1",
		CompletedExecutions: 9,
		FailedExecutions:    0,
	}

	profile = scorer.Score(
		profile,
		ExecutionOutcome{
			ActorID:     "actor-1",
			ExecutionID: "exec-10",
			Succeeded:   true,
			Compensated: true,
		},
	)

	if profile.CompletedExecutions != 10 || profile.CompensatedExecutions != 1 {
		t.Fatalf("profile = %#v", profile)
	}
	if profile.TrustLevel == TrustHigh {
		t.Fatalf("expected compensation to block high trust: %#v", profile)
	}
	if profile.TrustLevel != TrustMedium || profile.AutonomyTier != AutonomySupervised {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestDeterministicScorerRepeatedFailuresDowngradeToLowRestricted(t *testing.T) {
	t.Parallel()

	now := time.Unix(50, 0).UTC()
	scorer := NewDeterministicScorer(func() time.Time { return now })

	profile := TrustProfile{
		ActorID:             "actor-1",
		CompletedExecutions: 12,
		FailedExecutions:    1,
		TrustLevel:          TrustHigh,
		AutonomyTier:        AutonomyStandard,
	}

	profile = scorer.Score(
		profile,
		ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec-13", Succeeded: false},
	)

	if profile.FailedExecutions != 2 {
		t.Fatalf("FailedExecutions = %d", profile.FailedExecutions)
	}
	if profile.TrustLevel != TrustMedium || profile.AutonomyTier != AutonomySupervised {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestDeterministicScorerApprovalCountersUpdate(t *testing.T) {
	t.Parallel()

	now := time.Unix(60, 0).UTC()
	scorer := NewDeterministicScorer(func() time.Time { return now })

	profile := scorer.Score(
		TrustProfile{ActorID: "actor-1"},
		ExecutionOutcome{
			ActorID:          "actor-1",
			ExecutionID:      "exec-1",
			Succeeded:        true,
			RequiredApproval: true,
			Approved:         true,
		},
	)

	if profile.ApprovalRequests != 1 || profile.ApprovedExecutions != 1 {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestScorerInitialSuccessProgression(t *testing.T) {
	t.Parallel()

	now := time.Unix(100, 0).UTC()
	scorer := NewDeterministicScorer(func() time.Time { return now })

	profile := TrustProfile{ActorID: "actor-1", TrustLevel: TrustLow, AutonomyTier: AutonomyRestricted}

	for i := 0; i < 3; i++ {
		profile = scorer.Score(profile, ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec", Succeeded: true})
	}
	if profile.CompletedExecutions != 3 {
		t.Fatalf("completed=%d", profile.CompletedExecutions)
	}
	if profile.TrustLevel != TrustMedium || profile.AutonomyTier != AutonomySupervised {
		t.Fatalf("profile=%#v", profile)
	}

	for i := 0; i < 7; i++ {
		profile = scorer.Score(profile, ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec", Succeeded: true})
	}
	if profile.TrustLevel != TrustHigh || profile.AutonomyTier != AutonomyStandard {
		t.Fatalf("profile=%#v", profile)
	}
	if !profile.UpdatedAt.Equal(now) {
		t.Fatalf("updatedAt=%v", profile.UpdatedAt)
	}
}

func TestScorerFailureDowngradesTrust(t *testing.T) {
	t.Parallel()

	scorer := NewDeterministicScorer(func() time.Time { return time.Unix(200, 0).UTC() })

	profile := TrustProfile{
		ActorID:             "actor-1",
		CompletedExecutions: 3,
		TrustLevel:          TrustMedium,
		AutonomyTier:        AutonomySupervised,
	}

	profile = scorer.Score(profile, ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec-4", Succeeded: false})

	if profile.FailedExecutions != 1 {
		t.Fatalf("failed=%d", profile.FailedExecutions)
	}
	if profile.TrustLevel != TrustMedium || profile.AutonomyTier != AutonomySupervised {
		t.Fatalf("profile=%#v", profile)
	}
}

func TestScorerCompensationPreventsHighTrust(t *testing.T) {
	t.Parallel()

	scorer := NewDeterministicScorer(func() time.Time { return time.Unix(300, 0).UTC() })

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

	scorer := NewDeterministicScorer(func() time.Time { return time.Unix(400, 0).UTC() })

	profile := scorer.Score(
		TrustProfile{ActorID: "actor-1"},
		ExecutionOutcome{
			ActorID:          "actor-1",
			ExecutionID:      "exec-1",
			Succeeded:        true,
			RequiredApproval: true,
			Approved:         true,
		},
	)

	if profile.ApprovalRequests != 1 || profile.ApprovedExecutions != 1 {
		t.Fatalf("profile=%#v", profile)
	}
}
