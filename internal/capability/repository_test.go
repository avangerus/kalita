package capability

import (
	"context"
	"testing"
)

func TestInMemoryRepositorySaveGetListCapabilities(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryRepository()
	capA := Capability{ID: "cap-1", Code: "review", Type: CapabilitySkill, Level: 2, Metadata: map[string]any{"scope": "generic"}}
	capB := Capability{ID: "cap-2", Code: "approve", Type: CapabilityTool, Level: 1}
	if err := repo.SaveCapability(context.Background(), capA); err != nil {
		t.Fatalf("SaveCapability(capA) error = %v", err)
	}
	if err := repo.SaveCapability(context.Background(), capB); err != nil {
		t.Fatalf("SaveCapability(capB) error = %v", err)
	}
	got, ok, err := repo.GetCapability(context.Background(), "cap-1")
	if err != nil || !ok {
		t.Fatalf("GetCapability ok=%v err=%v", ok, err)
	}
	if got.Code != capA.Code || got.Metadata["scope"] != "generic" {
		t.Fatalf("GetCapability = %#v", got)
	}
	list, err := repo.ListCapabilities(context.Background())
	if err != nil {
		t.Fatalf("ListCapabilities error = %v", err)
	}
	if len(list) != 2 || list[0].ID != "cap-1" || list[1].ID != "cap-2" {
		t.Fatalf("ListCapabilities = %#v", list)
	}
}

func TestInMemoryRepositoryAssignCapabilityPreservesOrder(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryRepository()
	if err := repo.AssignCapability(context.Background(), ActorCapability{ActorID: "emp-1", CapabilityID: "cap-1", Level: 1}); err != nil {
		t.Fatalf("AssignCapability(cap-1) error = %v", err)
	}
	if err := repo.AssignCapability(context.Background(), ActorCapability{ActorID: "emp-1", CapabilityID: "cap-2", Level: 3}); err != nil {
		t.Fatalf("AssignCapability(cap-2) error = %v", err)
	}
	assigned, err := repo.ListByActor(context.Background(), "emp-1")
	if err != nil {
		t.Fatalf("ListByActor error = %v", err)
	}
	if len(assigned) != 2 || assigned[0].CapabilityID != "cap-1" || assigned[1].CapabilityID != "cap-2" {
		t.Fatalf("ListByActor = %#v", assigned)
	}
}
