package profile

import (
	"context"

	"kalita/internal/actionplan"
	"kalita/internal/employee"
	"kalita/internal/workplan"
)

type ExecutionStyle string

const (
	ExecutionStyleCareful     ExecutionStyle = "careful"
	ExecutionStyleFast        ExecutionStyle = "fast"
	ExecutionStyleBalanced    ExecutionStyle = "balanced"
	ExecutionStyleStrict      ExecutionStyle = "strict"
	ExecutionStyleExploratory ExecutionStyle = "exploratory"
)

type CompetencyProfile struct {
	ID                 string
	ActorID            string
	Name               string
	ExecutionStyle     ExecutionStyle
	MaxComplexity      int
	MaxRiskLevel       string
	PreferredWorkKinds []string
	Metadata           map[string]any
}

type CapabilityRequirement struct {
	ActionType      actionplan.ActionType
	CapabilityCodes []string
	MinimumLevel    int
}

type Repository interface {
	SaveProfile(ctx context.Context, p CompetencyProfile) error
	GetProfile(ctx context.Context, id string) (CompetencyProfile, bool, error)
	GetProfileByActor(ctx context.Context, actorID string) (CompetencyProfile, bool, error)
	ListProfiles(ctx context.Context) ([]CompetencyProfile, error)
}

type RequirementRepository interface {
	SaveRequirement(ctx context.Context, r CapabilityRequirement) error
	ListRequirements(ctx context.Context) ([]CapabilityRequirement, error)
}

type Matcher interface {
	MatchActor(ctx context.Context, wi workplan.WorkItem, plan actionplan.ActionPlan, actors []employee.DigitalEmployee) (employee.DigitalEmployee, string, error)
}

type Service interface {
	GetActorProfile(ctx context.Context, actorID string) (CompetencyProfile, bool, error)
}
