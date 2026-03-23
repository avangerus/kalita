package trust

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryRepositorySaveGetList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := NewInMemoryRepository()
	first := TrustProfile{ActorID: "actor-1", TrustLevel: TrustLow, AutonomyTier: AutonomyRestricted, UpdatedAt: time.Unix(10, 0).UTC()}
	second := TrustProfile{ActorID: "actor-2", TrustLevel: TrustMedium, AutonomyTier: AutonomySupervised, UpdatedAt: time.Unix(20, 0).UTC()}
	if err := repo.Save(ctx, first); err != nil {
		t.Fatalf("Save first error = %v", err)
	}
	if err := repo.Save(ctx, second); err != nil {
		t.Fatalf("Save second error = %v", err)
	}
	got, ok, err := repo.GetByActor(ctx, "actor-1")
	if err != nil {
		t.Fatalf("GetByActor error = %v", err)
	}
	if !ok || got.ActorID != first.ActorID || !got.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("got=%#v ok=%v", got, ok)
	}
	listed, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if len(listed) != 2 || listed[0].ActorID != "actor-1" || listed[1].ActorID != "actor-2" {
		t.Fatalf("listed=%#v", listed)
	}
}
