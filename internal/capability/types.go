package capability

import (
	"context"

	"kalita/internal/actionplan"
	"kalita/internal/employee"
	"kalita/internal/workplan"
)

type CapabilityType string

const (
	CapabilitySkill CapabilityType = "skill"
	CapabilityTool  CapabilityType = "tool"
)

type Capability struct {
	ID       string
	Code     string
	Type     CapabilityType
	Level    int
	Metadata map[string]any
}

type ActorCapability struct {
	ActorID      string
	CapabilityID string
	Level        int
}

type CapabilityRepository interface {
	SaveCapability(ctx context.Context, c Capability) error
	GetCapability(ctx context.Context, id string) (Capability, bool, error)
	ListCapabilities(ctx context.Context) ([]Capability, error)
}

type ActorCapabilityRepository interface {
	AssignCapability(ctx context.Context, ac ActorCapability) error
	ListByActor(ctx context.Context, actorID string) ([]ActorCapability, error)
}

type Matcher interface {
	MatchActor(
		ctx context.Context,
		wi workplan.WorkItem,
		plan actionplan.ActionPlan,
		actors []employee.DigitalEmployee,
	) (employee.DigitalEmployee, string, error)
}

type Service interface {
	GetActorCapabilities(ctx context.Context, actorID string) ([]Capability, error)
}
