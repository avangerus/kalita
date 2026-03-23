package trust

import (
	"context"
	"testing"
	"time"
)

func TestServiceCreatesDefaultProfileOnFirstOutcome(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(100, 0).UTC()
	repo := NewInMemoryRepository()
	service := NewService(repo, NewDeterministicScorer(func() time.Time { return now }))

	profile, err := service.RecordOutcome(ctx, ExecutionOutcome{
		ActorID:     "actor-1",
		ExecutionID: "exec-1",
		Succeeded:   true,
	})
	if err != nil {
		t.Fatalf("RecordOutcome error = %v", err)
	}
	if profile.ActorID != "actor-1" || profile.CompletedExecutions != 1 {
		t.Fatalf("profile=%#v", profile)
	}
	if profile.TrustLevel != TrustLow || profile.AutonomyTier != AutonomyRestricted {
		t.Fatalf("profile=%#v", profile)
	}
	if !profile.UpdatedAt.Equal(now) {
		t.Fatalf("UpdatedAt = %v", profile.UpdatedAt)
	}
}

func TestServiceUpdatesExistingProfileDeterministically(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(200, 0).UTC()
	repo := NewInMemoryRepository()
	service := NewService(repo, NewDeterministicScorer(func() time.Time { return now }))

	for i := 0; i < 3; i++ {
		if _, err := service.RecordOutcome(ctx, ExecutionOutcome{
			ActorID:     "actor-1",
			ExecutionID: "exec",
			Succeeded:   true,
		}); err != nil {
			t.Fatalf("RecordOutcome(%d) error = %v", i, err)
		}
	}

	profile, err := service.RecordOutcome(ctx, ExecutionOutcome{
		ActorID:          "actor-1",
		ExecutionID:      "exec-4",
		Succeeded:        false,
		RequiredApproval: true,
	})
	if err != nil {
		t.Fatalf("RecordOutcome error = %v", err)
	}
	if profile.CompletedExecutions != 3 || profile.FailedExecutions != 1 || profile.ApprovalRequests != 1 {
		t.Fatalf("profile=%#v", profile)
	}
	if profile.TrustLevel != TrustMedium || profile.AutonomyTier != AutonomySupervised {
		t.Fatalf("profile=%#v", profile)
	}
}

func TestServiceGetTrustProfileReturnsSavedProfile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(300, 0).UTC()
	repo := NewInMemoryRepository()
	service := NewService(repo, NewDeterministicScorer(func() time.Time { return now }))

	recorded, err := service.RecordOutcome(ctx, ExecutionOutcome{
		ActorID:     "actor-1",
		ExecutionID: "exec-1",
		Succeeded:   true,
		Approved:    true,
	})
	if err != nil {
		t.Fatalf("RecordOutcome error = %v", err)
	}

	got, ok, err := service.GetTrustProfile(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetTrustProfile error = %v", err)
	}
	if !ok || got != recorded {
		t.Fatalf("GetTrustProfile = %#v, %v", got, ok)
	}
}

func TestServiceRejectsEmptyActorID(t *testing.T) {
	t.Parallel()

	now := time.Unix(400, 0).UTC()
	repo := NewInMemoryRepository()
	service := NewService(repo, NewDeterministicScorer(func() time.Time { return now }))

	if _, err := service.RecordOutcome(context.Background(), ExecutionOutcome{
		ExecutionID: "exec-1",
		Succeeded:   true,
	}); err == nil {
		t.Fatal("expected error for empty actor id")
	}
}

func TestServiceNormalizesActorID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Unix(500, 0).UTC()
	repo := NewInMemoryRepository()
	service := NewService(repo, NewDeterministicScorer(func() time.Time { return now }))

	profile, err := service.RecordOutcome(ctx, ExecutionOutcome{
		ActorID:     " actor-1 ",
		ExecutionID: "exec-1",
		Succeeded:   true,
	})
	if err != nil {
		t.Fatalf("RecordOutcome error = %v", err)
	}
	if profile.ActorID != "actor-1" {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestServiceProgressionAndPersistence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := NewInMemoryRepository()
	scorer := NewDeterministicScorer(func() time.Time { return time.Unix(600, 0).UTC() })
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
