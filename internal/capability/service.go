package capability

import "context"

type capabilityService struct {
	capabilities CapabilityRepository
	assignments  ActorCapabilityRepository
}

func NewService(capabilities CapabilityRepository, assignments ActorCapabilityRepository) Service {
	return &capabilityService{capabilities: capabilities, assignments: assignments}
}

func (s *capabilityService) GetActorCapabilities(ctx context.Context, actorID string) ([]Capability, error) {
	assigned, err := s.assignments.ListByActor(ctx, actorID)
	if err != nil {
		return nil, err
	}
	out := make([]Capability, 0, len(assigned))
	for _, assignment := range assigned {
		capability, ok, err := s.capabilities.GetCapability(ctx, assignment.CapabilityID)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if assignment.Level > 0 {
			capability.Level = assignment.Level
		}
		out = append(out, capability)
	}
	return out, nil
}
