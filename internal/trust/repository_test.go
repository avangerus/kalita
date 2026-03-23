package trust

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryRepositorySaveGetAndList(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := NewInMemoryRepository()

	first := TrustProfile{
		ActorID:      "actor-1",
		TrustLevel:   TrustLow,
		AutonomyTier: AutonomyRestricted,
		UpdatedAt:    time.Unix(1, 0).UTC(),
	}
	second := TrustProfile{
		ActorID:      "actor-2",
		TrustLevel:   TrustMedium,
		AutonomyTier: AutonomySupervised,
		UpdatedAt:    time.Unix(2, 0).UTC(),
	}

	if err := repo.Save(ctx, first); err != nil {
		t.Fatalf("Save(first) error = %v", err)
	}
	if err := repo.Save(ctx, second); err != nil {
		t.Fatalf("Save(second) error = %v", err)
	}

	got, ok, err := repo.GetByActor(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetByActor error = %v", err)
	}
	if !ok ||
		got.ActorID != first.ActorID ||
		got.TrustLevel != first.TrustLevel ||
		got.AutonomyTier != first.AutonomyTier ||
		!got.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("GetByActor = %#v, %v", got, ok)
	}

	profiles, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if len(profiles) != 2 || profiles[0].ActorID != "actor-1" || profiles[1].ActorID != "actor-2" {
		t.Fatalf("List = %#v", profiles)
	}
}

func TestInMemoryRepositoryPreservesLastSavedProfileForActor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := NewInMemoryRepository()

	initial := TrustProfile{
		ActorID:             "actor-1",
		CompletedExecutions: 1,
		TrustLevel:          TrustLow,
		AutonomyTier:        AutonomyRestricted,
		UpdatedAt:           time.Unix(1, 0).UTC(),
	}
	if err := repo.Save(ctx, initial); err != nil {
		t.Fatalf("Save initial error = %v", err)
	}

	updated := TrustProfile{
		ActorID:             "actor-1",
		CompletedExecutions: 4,
		TrustLevel:          TrustMedium,
		AutonomyTier:        AutonomySupervised,
		UpdatedAt:           time.Unix(2, 0).UTC(),
	}
	if err := repo.Save(ctx, updated); err != nil {
		t.Fatalf("Save updated error = %v", err)
	}

	got, ok, err := repo.GetByActor(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetByActor error = %v", err)
	}
	if !ok ||
		got.CompletedExecutions != 4 ||
		got.TrustLevel != TrustMedium ||
		got.AutonomyTier != AutonomySupervised {
		t.Fatalf("GetByActor = %#v, %v", got, ok)
	}

	profiles, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if len(profiles) != 1 || profiles[0].CompletedExecutions != 4 {
		t.Fatalf("List = %#v", profiles)
	}
}

func TestInMemoryRepositoryRejectsEmptyActorID(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryRepository()
	if err := repo.Save(context.Background(), TrustProfile{}); err == nil {
		t.Fatal("expected error for empty actor id")
	}
}

func TestInMemoryRepositoryNormalizesActorID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := NewInMemoryRepository()

	if err := repo.Save(ctx, TrustProfile{
		ActorID:      " actor-1 ",
		TrustLevel:   TrustLow,
		AutonomyTier: AutonomyRestricted,
	}); err != nil {
		t.Fatalf("Save error = %v", err)
	}

	got, ok, err := repo.GetByActor(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetByActor error = %v", err)
	}
	if !ok || got.ActorID != "actor-1" {
		t.Fatalf("GetByActor = %#v, %v", got, ok)
	}
}