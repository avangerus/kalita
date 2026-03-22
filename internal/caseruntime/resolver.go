package caseruntime

import (
	"context"
	"fmt"
	"strings"

	"kalita/internal/eventcore"
)

type Resolver struct {
	repo  CaseRepository
	clock eventcore.Clock
	ids   eventcore.IDGenerator
}

func NewResolver(repo CaseRepository, clock eventcore.Clock, ids eventcore.IDGenerator) *Resolver {
	if clock == nil {
		clock = eventcore.RealClock{}
	}
	if ids == nil {
		ids = eventcore.NewULIDGenerator()
	}
	return &Resolver{repo: repo, clock: clock, ids: ids}
}

func (r *Resolver) ResolveForCommand(ctx context.Context, cmd eventcore.Command) (Case, bool, error) {
	if r.repo == nil {
		return Case{}, false, fmt.Errorf("case repository is nil")
	}

	if existing, ok, err := r.repo.FindByCorrelation(ctx, cmd.CorrelationID); err != nil || ok {
		return existing, ok, err
	}
	if existing, ok, err := r.repo.FindBySubjectRef(ctx, cmd.TargetRef); err != nil || ok {
		return existing, ok, err
	}

	now := r.clock.Now()
	opened := Case{
		ID:            r.ids.NewID(),
		Kind:          caseKindForCommand(cmd.Type),
		Status:        string(CaseOpen),
		Title:         caseTitleForCommand(cmd),
		SubjectRef:    cmd.TargetRef,
		CorrelationID: cmd.CorrelationID,
		OpenedAt:      now,
		UpdatedAt:     now,
		Attributes: map[string]any{
			"command_type": cmd.Type,
		},
	}
	if err := r.repo.Save(ctx, opened); err != nil {
		return Case{}, false, err
	}
	return opened, false, nil
}

func caseKindForCommand(commandType string) string {
	trimmed := strings.TrimSpace(commandType)
	if trimmed == "" {
		return "generic"
	}
	return trimmed
}

func caseTitleForCommand(cmd eventcore.Command) string {
	if cmd.TargetRef != "" {
		return fmt.Sprintf("%s for %s", caseKindForCommand(cmd.Type), cmd.TargetRef)
	}
	return fmt.Sprintf("%s case", caseKindForCommand(cmd.Type))
}
