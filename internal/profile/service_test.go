package profile

import (
	"context"
	"testing"
)

func TestServiceReturnsActorProfile(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryRepository()
	ctx := context.Background()
	_ = repo.SaveProfile(ctx, CompetencyProfile{ID: "profile-1", ActorID: "emp-1", Name: "Balanced", MaxComplexity: 2})
	service := NewService(repo)
	profile, ok, err := service.GetActorProfile(ctx, "emp-1")
	if err != nil {
		t.Fatalf("GetActorProfile error = %v", err)
	}
	if !ok || profile.ID != "profile-1" {
		t.Fatalf("profile=%#v ok=%v", profile, ok)
	}
}
