package trust

import (
	"context"
	"testing"
	"time"
)

func TestServiceCreatesDefaultProfileOnFirstOutcome(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewInMemoryRepository()
	service := NewService(repo, NewScorerWithClock(func() time.Time { return time.Unix(500, 0).UTC() }))
	profile, err := service.RecordOutcome(ctx, ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec-1", Succeeded: true})
	if err != nil {
		t.Fatalf("RecordOutcome error = %v", err)
	}
	if profile.ActorID != "actor-1" || profile.CompletedExecutions != 1 {
		t.Fatalf("profile=%#v", profile)
	}
	if profile.TrustLevel != TrustLow || profile.AutonomyTier != AutonomyRestricted {
		t.Fatalf("profile=%#v", profile)
	}
}

func TestServiceUpdatesExistingProfileDeterministically(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewInMemoryRepository()
	scorer := NewScorerWithClock(func() time.Time { return time.Unix(600, 0).UTC() })
	service := NewService(repo, scorer)
	outcomes := []ExecutionOutcome{
		{ActorID: "actor-1", ExecutionID: "exec-1", Succeeded: true},
		{ActorID: "actor-1", ExecutionID: "exec-2", Succeeded: true, RequiredApproval: true, Approved: true},
		{ActorID: "actor-1", ExecutionID: "exec-3", Succeeded: true},
	}
	var profile TrustProfile
	var err error
	for _, outcome := range outcomes {
		profile, err = service.RecordOutcome(ctx, outcome)
		if err != nil {
			t.Fatalf("RecordOutcome error = %v", err)
		}
	}
	if profile.CompletedExecutions != 3 || profile.ApprovalRequests != 1 || profile.ApprovedExecutions != 1 {
		t.Fatalf("profile=%#v", profile)
	}
	if profile.TrustLevel != TrustMedium || profile.AutonomyTier != AutonomySupervised {
		t.Fatalf("profile=%#v", profile)
	}
	persisted, ok, err := service.GetTrustProfile(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetTrustProfile error = %v", err)
	}
	if !ok || persisted != profile {
		t.Fatalf("persisted=%#v ok=%v profile=%#v", persisted, ok, profile)
	}
}
