package profile

import (
	"context"
	"testing"

	"kalita/internal/actionplan"
)

func TestInMemoryRepositorySaveGetAndListProfiles(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryRepository()
	ctx := context.Background()
	if err := repo.SaveProfile(ctx, CompetencyProfile{ID: "profile-1", ActorID: "emp-1", Name: "Careful", MaxComplexity: 3, PreferredWorkKinds: []string{"review"}, Metadata: map[string]any{"mode": "deterministic"}}); err != nil {
		t.Fatalf("SaveProfile error = %v", err)
	}
	if err := repo.SaveProfile(ctx, CompetencyProfile{ID: "profile-2", ActorID: "emp-2", Name: "Fast", MaxComplexity: 5}); err != nil {
		t.Fatalf("SaveProfile error = %v", err)
	}
	profile, ok, err := repo.GetProfile(ctx, "profile-1")
	if err != nil || !ok {
		t.Fatalf("GetProfile ok=%v err=%v", ok, err)
	}
	if profile.ActorID != "emp-1" || profile.Metadata["mode"] != "deterministic" {
		t.Fatalf("GetProfile = %#v", profile)
	}
	byActor, ok, err := repo.GetProfileByActor(ctx, "emp-2")
	if err != nil || !ok || byActor.ID != "profile-2" {
		t.Fatalf("GetProfileByActor profile=%#v ok=%v err=%v", byActor, ok, err)
	}
	profiles, err := repo.ListProfiles(ctx)
	if err != nil {
		t.Fatalf("ListProfiles error = %v", err)
	}
	if len(profiles) != 2 || profiles[0].ID != "profile-1" || profiles[1].ID != "profile-2" {
		t.Fatalf("ListProfiles = %#v", profiles)
	}
}

func TestInMemoryRepositorySaveAndListRequirements(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryRepository()
	ctx := context.Background()
	if err := repo.SaveRequirement(ctx, CapabilityRequirement{ActionType: actionplan.ActionType("legacy_workflow_action"), CapabilityCodes: []string{"workflow.execute"}, MinimumLevel: 2}); err != nil {
		t.Fatalf("SaveRequirement error = %v", err)
	}
	if err := repo.SaveRequirement(ctx, CapabilityRequirement{ActionType: actionplan.ActionType("approval_action"), CapabilityCodes: []string{"approval.review"}, MinimumLevel: 1}); err != nil {
		t.Fatalf("SaveRequirement error = %v", err)
	}
	requirements, err := repo.ListRequirements(ctx)
	if err != nil {
		t.Fatalf("ListRequirements error = %v", err)
	}
	if len(requirements) != 2 || requirements[0].ActionType != "legacy_workflow_action" || requirements[1].ActionType != "approval_action" {
		t.Fatalf("ListRequirements = %#v", requirements)
	}
}
