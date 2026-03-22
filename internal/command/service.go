package command

import (
	"context"

	"kalita/internal/eventcore"
)

type PassThroughAdmissionPolicy struct{}

func (PassThroughAdmissionPolicy) Admit(context.Context, eventcore.Command) error {
	return nil
}

type Service struct {
	policy AdmissionPolicy
	log    eventcore.EventLog
	clock  eventcore.Clock
	ids    eventcore.IDGenerator
}

func NewService(log eventcore.EventLog, policy AdmissionPolicy, clock eventcore.Clock, ids eventcore.IDGenerator) *Service {
	if policy == nil {
		policy = PassThroughAdmissionPolicy{}
	}
	if clock == nil {
		clock = eventcore.RealClock{}
	}
	if ids == nil {
		ids = eventcore.NewULIDGenerator()
	}
	return &Service{policy: policy, log: log, clock: clock, ids: ids}
}

func (s *Service) Submit(ctx context.Context, cmd eventcore.Command) (eventcore.Command, error) {
	if cmd.ID == "" {
		cmd.ID = s.ids.NewID()
	}
	if cmd.RequestedAt.IsZero() {
		cmd.RequestedAt = s.clock.Now()
	}
	if cmd.CorrelationID == "" {
		cmd.CorrelationID = s.ids.NewID()
	}
	if cmd.ExecutionID == "" {
		cmd.ExecutionID = s.ids.NewID()
	}

	if err := s.policy.Admit(ctx, cmd); err != nil {
		return eventcore.Command{}, err
	}
	if s.log != nil {
		execEvent := eventcore.ExecutionEvent{
			ID:            s.ids.NewID(),
			ExecutionID:   cmd.ExecutionID,
			CaseID:        cmd.CaseID,
			Step:          "command_admission",
			Status:        "admitted",
			OccurredAt:    s.clock.Now(),
			CorrelationID: cmd.CorrelationID,
			CausationID:   cmd.ID,
			Payload: map[string]any{
				"command_type": cmd.Type,
				"target_ref":   cmd.TargetRef,
			},
		}
		if err := s.log.AppendExecutionEvent(ctx, execEvent); err != nil {
			return eventcore.Command{}, err
		}
	}

	return cmd, nil
}
