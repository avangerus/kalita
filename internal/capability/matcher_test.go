package capability

import (
	"context"
	"testing"

	"kalita/internal/actionplan"
	"kalita/internal/employee"
	"kalita/internal/workplan"
)

func TestMatcherSelectsActorWithRequiredCapability(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryRepository()
	_ = repo.SaveCapability(context.Background(), Capability{ID: "cap-1", Code: "legacy_workflow_action", Type: CapabilitySkill})
	_ = repo.AssignCapability(context.Background(), ActorCapability{ActorID: "emp-1", CapabilityID: "cap-1", Level: 2})
	matcher := NewMatcher(repo, repo)
	actor, reason, err := matcher.MatchActor(context.Background(), workplan.WorkItem{ID: "work-1", QueueID: "q-1"}, actionplan.ActionPlan{Actions: []actionplan.Action{{ID: "a-1", Type: "legacy_workflow_action"}}}, []employee.DigitalEmployee{{ID: "emp-1", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}}})
	if err != nil {
		t.Fatalf("MatchActor error = %v", err)
	}
	if actor.ID != "emp-1" || reason == "" {
		t.Fatalf("actor=%#v reason=%q", actor, reason)
	}
}

func TestMatcherRejectsActorWithoutRequiredCapability(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryRepository()
	matcher := NewMatcher(repo, repo)
	_, _, err := matcher.MatchActor(context.Background(), workplan.WorkItem{ID: "work-1", QueueID: "q-1"}, actionplan.ActionPlan{Actions: []actionplan.Action{{ID: "a-1", Type: "legacy_workflow_action"}}}, []employee.DigitalEmployee{{ID: "emp-1", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}}})
	if err == nil {
		t.Fatal("expected error for missing capability")
	}
}

func TestMatcherUsesDeterministicSelectionOrder(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryRepository()
	_ = repo.SaveCapability(context.Background(), Capability{ID: "cap-1", Code: "legacy_workflow_action", Type: CapabilitySkill})
	_ = repo.AssignCapability(context.Background(), ActorCapability{ActorID: "emp-1", CapabilityID: "cap-1", Level: 1})
	_ = repo.AssignCapability(context.Background(), ActorCapability{ActorID: "emp-2", CapabilityID: "cap-1", Level: 1})
	matcher := NewMatcher(repo, repo)
	actors := []employee.DigitalEmployee{
		{ID: "emp-1", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}},
		{ID: "emp-2", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}},
	}
	actor, _, err := matcher.MatchActor(context.Background(), workplan.WorkItem{ID: "work-1", QueueID: "q-1"}, actionplan.ActionPlan{Actions: []actionplan.Action{{ID: "a-1", Type: "legacy_workflow_action"}}}, actors)
	if err != nil {
		t.Fatalf("MatchActor error = %v", err)
	}
	if actor.ID != "emp-1" {
		t.Fatalf("selected actor = %#v", actor)
	}
}
