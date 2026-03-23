package profile

import (
	"context"
	"strings"
	"testing"

	"kalita/internal/actionplan"
	"kalita/internal/capability"
	"kalita/internal/employee"
	"kalita/internal/trust"
	"kalita/internal/workplan"
)

func TestMatcherRejectsActorBelowMinimumCapabilityLevel(t *testing.T) {
	t.Parallel()
	profiles := NewInMemoryRepository()
	caps := capability.NewInMemoryRepository()
	ctx := context.Background()
	_ = profiles.SaveRequirement(ctx, CapabilityRequirement{ActionType: "legacy_workflow_action", CapabilityCodes: []string{"workflow.execute"}, MinimumLevel: 3})
	_ = caps.SaveCapability(ctx, capability.Capability{ID: "cap-1", Code: "workflow.execute", Level: 1})
	_ = caps.AssignCapability(ctx, capability.ActorCapability{ActorID: "emp-1", CapabilityID: "cap-1", Level: 2})
	matcher := NewMatcher(profiles, profiles, caps, caps)
	_, _, err := matcher.MatchActor(ctx, workplan.WorkItem{ID: "work-1", QueueID: "q-1", Type: "review"}, actionplan.ActionPlan{Actions: []actionplan.Action{{ID: "a-1", Type: "legacy_workflow_action"}}}, []employee.DigitalEmployee{{ID: "emp-1", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}}})
	if err == nil {
		t.Fatal("expected error for actor below minimum capability level")
	}
}

func TestMatcherRejectsActorWhoseMaxComplexityIsTooLow(t *testing.T) {
	t.Parallel()
	profiles := NewInMemoryRepository()
	caps := capability.NewInMemoryRepository()
	ctx := context.Background()
	_ = profiles.SaveRequirement(ctx, CapabilityRequirement{ActionType: "legacy_workflow_action", CapabilityCodes: []string{"workflow.execute"}, MinimumLevel: 2})
	_ = profiles.SaveProfile(ctx, CompetencyProfile{ID: "profile-1", ActorID: "emp-1", MaxComplexity: 1})
	_ = caps.SaveCapability(ctx, capability.Capability{ID: "cap-1", Code: "workflow.execute", Level: 2})
	_ = caps.AssignCapability(ctx, capability.ActorCapability{ActorID: "emp-1", CapabilityID: "cap-1", Level: 2})
	matcher := NewMatcher(profiles, profiles, caps, caps)
	plan := actionplan.ActionPlan{Actions: []actionplan.Action{{ID: "a-1", Type: "legacy_workflow_action"}, {ID: "a-2", Type: "legacy_workflow_action"}}}
	_, _, err := matcher.MatchActor(ctx, workplan.WorkItem{ID: "work-1", QueueID: "q-1", Type: "review"}, plan, []employee.DigitalEmployee{{ID: "emp-1", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}}})
	if err == nil {
		t.Fatal("expected error for actor below max complexity")
	}
}

func TestMatcherPrefersActorWhoseProfileMatchesPreferredWorkKinds(t *testing.T) {
	t.Parallel()
	profiles := NewInMemoryRepository()
	caps := capability.NewInMemoryRepository()
	ctx := context.Background()
	_ = profiles.SaveRequirement(ctx, CapabilityRequirement{ActionType: "legacy_workflow_action", CapabilityCodes: []string{"workflow.execute"}, MinimumLevel: 1})
	_ = profiles.SaveProfile(ctx, CompetencyProfile{ID: "profile-1", ActorID: "emp-1", MaxComplexity: 3})
	_ = profiles.SaveProfile(ctx, CompetencyProfile{ID: "profile-2", ActorID: "emp-2", MaxComplexity: 3, PreferredWorkKinds: []string{"review"}})
	_ = caps.SaveCapability(ctx, capability.Capability{ID: "cap-1", Code: "workflow.execute", Level: 1})
	_ = caps.AssignCapability(ctx, capability.ActorCapability{ActorID: "emp-1", CapabilityID: "cap-1", Level: 1})
	_ = caps.AssignCapability(ctx, capability.ActorCapability{ActorID: "emp-2", CapabilityID: "cap-1", Level: 1})
	matcher := NewMatcher(profiles, profiles, caps, caps)
	actors := []employee.DigitalEmployee{
		{ID: "emp-1", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}},
		{ID: "emp-2", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}},
	}
	actor, reason, err := matcher.MatchActor(ctx, workplan.WorkItem{ID: "work-1", QueueID: "q-1", Type: "review"}, actionplan.ActionPlan{Actions: []actionplan.Action{{ID: "a-1", Type: "legacy_workflow_action"}}}, actors)
	if err != nil {
		t.Fatalf("MatchActor error = %v", err)
	}
	if actor.ID != "emp-2" || reason == "" {
		t.Fatalf("actor=%#v reason=%q", actor, reason)
	}
}

func TestMatcherUsesDeterministicSelectionWhenMultipleActorsMatch(t *testing.T) {
	t.Parallel()
	profiles := NewInMemoryRepository()
	caps := capability.NewInMemoryRepository()
	ctx := context.Background()
	_ = profiles.SaveRequirement(ctx, CapabilityRequirement{ActionType: "legacy_workflow_action", CapabilityCodes: []string{"workflow.execute"}, MinimumLevel: 1})
	_ = caps.SaveCapability(ctx, capability.Capability{ID: "cap-1", Code: "workflow.execute", Level: 1})
	_ = caps.AssignCapability(ctx, capability.ActorCapability{ActorID: "emp-1", CapabilityID: "cap-1", Level: 1})
	_ = caps.AssignCapability(ctx, capability.ActorCapability{ActorID: "emp-2", CapabilityID: "cap-1", Level: 1})
	matcher := NewMatcher(profiles, profiles, caps, caps)
	actors := []employee.DigitalEmployee{
		{ID: "emp-1", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}},
		{ID: "emp-2", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}},
	}
	actor, _, err := matcher.MatchActor(ctx, workplan.WorkItem{ID: "work-1", QueueID: "q-1", Type: "review"}, actionplan.ActionPlan{Actions: []actionplan.Action{{ID: "a-1", Type: "legacy_workflow_action"}}}, actors)
	if err != nil {
		t.Fatalf("MatchActor error = %v", err)
	}
	if actor.ID != "emp-1" {
		t.Fatalf("selected actor = %#v", actor)
	}
}

func TestMatcherPrefersHigherTrustBeforeProfilePreference(t *testing.T) {
	t.Parallel()
	profiles := NewInMemoryRepository()
	caps := capability.NewInMemoryRepository()
	trustRepo := trust.NewInMemoryRepository()
	trustService := trust.NewService(trustRepo, trust.NewScorerWithClock(nil))
	ctx := context.Background()
	_ = profiles.SaveRequirement(ctx, CapabilityRequirement{ActionType: "legacy_workflow_action", CapabilityCodes: []string{"workflow.execute"}, MinimumLevel: 1})
	_ = profiles.SaveProfile(ctx, CompetencyProfile{ID: "profile-1", ActorID: "emp-1", MaxComplexity: 3, PreferredWorkKinds: []string{"review"}})
	_ = profiles.SaveProfile(ctx, CompetencyProfile{ID: "profile-2", ActorID: "emp-2", MaxComplexity: 3})
	_ = caps.SaveCapability(ctx, capability.Capability{ID: "cap-1", Code: "workflow.execute", Level: 1})
	_ = caps.AssignCapability(ctx, capability.ActorCapability{ActorID: "emp-1", CapabilityID: "cap-1", Level: 1})
	_ = caps.AssignCapability(ctx, capability.ActorCapability{ActorID: "emp-2", CapabilityID: "cap-1", Level: 1})
	_ = trustRepo.Save(ctx, trust.TrustProfile{ActorID: "emp-1", TrustLevel: trust.TrustMedium})
	_ = trustRepo.Save(ctx, trust.TrustProfile{ActorID: "emp-2", TrustLevel: trust.TrustHigh})
	matcher := NewMatcher(profiles, profiles, caps, caps, trustService)
	actors := []employee.DigitalEmployee{
		{ID: "emp-1", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}},
		{ID: "emp-2", Enabled: true, QueueMemberships: []string{"q-1"}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}},
	}
	actor, reason, err := matcher.MatchActor(ctx, workplan.WorkItem{ID: "work-1", QueueID: "q-1", Type: "review"}, actionplan.ActionPlan{Actions: []actionplan.Action{{ID: "a-1", Type: "legacy_workflow_action"}}}, actors)
	if err != nil {
		t.Fatalf("MatchActor error = %v", err)
	}
	if actor.ID != "emp-2" {
		t.Fatalf("selected actor = %#v", actor)
	}
	if !strings.Contains(reason, "trust level high applied") || !strings.Contains(reason, "others not chosen") {
		t.Fatalf("reason = %q", reason)
	}
}
