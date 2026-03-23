package trust

import (
	"context"
	"fmt"
)

type trustService struct {
	repo   Repository
	scorer Scorer
}

func NewService(repo Repository, scorer Scorer) Service {
	return &trustService{repo: repo, scorer: scorer}
}

func (s *trustService) RecordOutcome(ctx context.Context, outcome ExecutionOutcome) (TrustProfile, error) {
	if s.repo == nil {
		return TrustProfile{}, fmt.Errorf("trust repository is nil")
	}
	if s.scorer == nil {
		return TrustProfile{}, fmt.Errorf("trust scorer is nil")
	}
	current, ok, err := s.repo.GetByActor(ctx, outcome.ActorID)
	if err != nil {
		return TrustProfile{}, err
	}
	if !ok {
		current = TrustProfile{ActorID: outcome.ActorID}
	}
	updated := s.scorer.Score(current, outcome)
	if err := s.repo.Save(ctx, updated); err != nil {
		return TrustProfile{}, err
	}
	return updated, nil
}

func (s *trustService) GetTrustProfile(ctx context.Context, actorID string) (TrustProfile, bool, error) {
	if s.repo == nil {
		return TrustProfile{}, false, fmt.Errorf("trust repository is nil")
	}
	return s.repo.GetByActor(ctx, actorID)
}
