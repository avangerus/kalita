package trust

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryRepositorySaveGetAndList(t *testing.T) {
	repo := NewInMemoryRepository()
	first := TrustProfile{ActorID: "actor-1", TrustLevel: TrustLow, UpdatedAt: time.Unix(1, 0)}
	second := TrustProfile{ActorID: "actor-2", TrustLevel: TrustMedium, UpdatedAt: time.Unix(2, 0)}

	if err := repo.Save(context.Background(), first); err != nil {
		t.Fatalf("Save(first) error = %v", err)
	}
	if err := repo.Save(context.Background(), second); err != nil {
		t.Fatalf("Save(second) error = %v", err)
	}

	got, ok, err := repo.GetByActor(context.Background(), "actor-1")
	if err != nil {
		t.Fatalf("GetByActor error = %v", err)
	}
	if !ok || got.ActorID != first.ActorID || got.UpdatedAt != first.UpdatedAt {
		t.Fatalf("GetByActor = %#v, %v", got, ok)
	}

	profiles, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if len(profiles) != 2 || profiles[0].ActorID != "actor-1" || profiles[1].ActorID != "actor-2" {
		t.Fatalf("List = %#v", profiles)
	}
}

func TestInMemoryRepositoryPreservesLastSavedProfileForActor(t *testing.T) {
	repo := NewInMemoryRepository()
	if err := repo.Save(context.Background(), TrustProfile{ActorID: "actor-1", CompletedExecutions: 1, TrustLevel: TrustLow, UpdatedAt: time.Unix(1, 0)}); err != nil {
		t.Fatalf("Save initial error = %v", err)
	}
	updated := TrustProfile{ActorID: "actor-1", CompletedExecutions: 4, TrustLevel: TrustMedium, UpdatedAt: time.Unix(2, 0)}
	if err := repo.Save(context.Background(), updated); err != nil {
		t.Fatalf("Save updated error = %v", err)
	}

	got, ok, err := repo.GetByActor(context.Background(), "actor-1")
	if err != nil {
		t.Fatalf("GetByActor error = %v", err)
	}
	if !ok || got.CompletedExecutions != 4 || got.TrustLevel != TrustMedium {
		t.Fatalf("GetByActor = %#v, %v", got, ok)
	}

	profiles, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List error = %v", err)
	}
	if len(profiles) != 1 || profiles[0].CompletedExecutions != 4 {
		t.Fatalf("List = %#v", profiles)
	}
}
