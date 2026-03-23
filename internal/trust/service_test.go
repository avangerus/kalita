package trust

import (
	"context"
	"testing"
	"time"
)

func TestServiceCreatesDefaultProfileOnFirstOutcome(t *testing.T) {
	now := time.Unix(100, 0)
	repo := NewInMemoryRepository()
	svc := NewService(repo, NewDeterministicScorer(func() time.Time { return now }))

	profile, err := svc.RecordOutcome(context.Background(), ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec-1", Succeeded: true})
	if err != nil {
		t.Fatalf("RecordOutcome error = %v", err)
	}
	if profile.ActorID != "actor-1" || profile.CompletedExecutions != 1 {
		t.Fatalf("profile = %#v", profile)
	}
	if profile.TrustLevel != TrustLow || profile.AutonomyTier != AutonomyRestricted {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestServiceUpdatesExistingProfileDeterministically(t *testing.T) {
	now := time.Unix(200, 0)
	repo := NewInMemoryRepository()
	svc := NewService(repo, NewDeterministicScorer(func() time.Time { return now }))

	for i := 0; i < 3; i++ {
		if _, err := svc.RecordOutcome(context.Background(), ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec", Succeeded: true}); err != nil {
			t.Fatalf("RecordOutcome(%d) error = %v", i, err)
		}
	}
	profile, err := svc.RecordOutcome(context.Background(), ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec-4", Succeeded: false, RequiredApproval: true})
	if err != nil {
		t.Fatalf("RecordOutcome error = %v", err)
	}
	if profile.CompletedExecutions != 3 || profile.FailedExecutions != 1 || profile.ApprovalRequests != 1 {
		t.Fatalf("profile = %#v", profile)
	}
	if profile.TrustLevel != TrustLow || profile.AutonomyTier != AutonomyRestricted {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestServiceGetTrustProfileReturnsSavedProfile(t *testing.T) {
	now := time.Unix(300, 0)
	repo := NewInMemoryRepository()
	svc := NewService(repo, NewDeterministicScorer(func() time.Time { return now }))

	recorded, err := svc.RecordOutcome(context.Background(), ExecutionOutcome{ActorID: "actor-1", ExecutionID: "exec-1", Succeeded: true, Approved: true})
	if err != nil {
		t.Fatalf("RecordOutcome error = %v", err)
	}
	got, ok, err := svc.GetTrustProfile(context.Background(), "actor-1")
	if err != nil {
		t.Fatalf("GetTrustProfile error = %v", err)
	}
	if !ok || got != recorded {
		t.Fatalf("GetTrustProfile = %#v, %v", got, ok)
	}
}

func TestServiceRejectsEmptyActorID(t *testing.T) {
	now := time.Unix(400, 0)
	repo := NewInMemoryRepository()
	svc := NewService(repo, NewDeterministicScorer(func() time.Time { return now }))

	if _, err := svc.RecordOutcome(context.Background(), ExecutionOutcome{ExecutionID: "exec-1", Succeeded: true}); err == nil {
		t.Fatal("expected error for empty actor id")
	}
}

func TestServiceNormalizesActorID(t *testing.T) {
	now := time.Unix(500, 0)
	repo := NewInMemoryRepository()
	svc := NewService(repo, NewDeterministicScorer(func() time.Time { return now }))

	profile, err := svc.RecordOutcome(context.Background(), ExecutionOutcome{ActorID: " actor-1 ", ExecutionID: "exec-1", Succeeded: true})
	if err != nil {
		t.Fatalf("RecordOutcome error = %v", err)
	}
	if profile.ActorID != "actor-1" {
		t.Fatalf("profile = %#v", profile)
	}
}
