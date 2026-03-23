package capability

import (
	"context"
	"testing"
)

func TestServiceReturnsActorCapabilities(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryRepository()
	_ = repo.SaveCapability(context.Background(), Capability{ID: "cap-1", Code: "legacy_workflow_action", Type: CapabilitySkill, Level: 1})
	_ = repo.SaveCapability(context.Background(), Capability{ID: "cap-2", Code: "approval_tool", Type: CapabilityTool, Level: 1})
	_ = repo.AssignCapability(context.Background(), ActorCapability{ActorID: "emp-1", CapabilityID: "cap-1", Level: 3})
	_ = repo.AssignCapability(context.Background(), ActorCapability{ActorID: "emp-1", CapabilityID: "cap-2", Level: 2})
	service := NewService(repo, repo)
	caps, err := service.GetActorCapabilities(context.Background(), "emp-1")
	if err != nil {
		t.Fatalf("GetActorCapabilities error = %v", err)
	}
	if len(caps) != 2 || caps[0].Code != "legacy_workflow_action" || caps[0].Level != 3 || caps[1].Code != "approval_tool" {
		t.Fatalf("GetActorCapabilities = %#v", caps)
	}
}
