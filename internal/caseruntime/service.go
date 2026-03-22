package caseruntime

import (
	"context"
	"fmt"

	"kalita/internal/eventcore"
)

type Service struct {
	resolver CaseResolver
	log      eventcore.EventLog
	clock    eventcore.Clock
	ids      eventcore.IDGenerator
}

type ResolutionResult struct {
	Command eventcore.Command
	Case    Case
	Event   eventcore.ExecutionEvent
	Existed bool
}

func NewService(resolver CaseResolver, log eventcore.EventLog, clock eventcore.Clock, ids eventcore.IDGenerator) *Service {
	if clock == nil {
		clock = eventcore.RealClock{}
	}
	if ids == nil {
		ids = eventcore.NewULIDGenerator()
	}
	return &Service{resolver: resolver, log: log, clock: clock, ids: ids}
}

func (s *Service) ResolveCommand(ctx context.Context, cmd eventcore.Command) (ResolutionResult, error) {
	if s.resolver == nil {
		return ResolutionResult{}, fmt.Errorf("case resolver is nil")
	}

	resolvedCase, existed, err := s.resolver.ResolveForCommand(ctx, cmd)
	if err != nil {
		return ResolutionResult{}, err
	}

	cmd.CaseID = resolvedCase.ID
	status := "opened_new"
	if existed {
		status = "resolved_existing"
	}

	execEvent := eventcore.ExecutionEvent{
		ID:            s.ids.NewID(),
		ExecutionID:   cmd.ExecutionID,
		CaseID:        resolvedCase.ID,
		Step:          "case_resolution",
		Status:        status,
		OccurredAt:    s.clock.Now(),
		CorrelationID: cmd.CorrelationID,
		CausationID:   cmd.ID,
		Payload: map[string]any{
			"case_id":      resolvedCase.ID,
			"case_kind":    resolvedCase.Kind,
			"subject_ref":  resolvedCase.SubjectRef,
			"command_type": cmd.Type,
		},
	}

	if s.log != nil {
		if err := s.log.AppendExecutionEvent(ctx, execEvent); err != nil {
			return ResolutionResult{}, err
		}
	}

	return ResolutionResult{Command: cmd, Case: resolvedCase, Event: execEvent, Existed: existed}, nil
}
