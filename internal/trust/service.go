package trust

import (
	"context"
	"fmt"
)

type trustService struct {
	repository Repository
	scorer     Scorer
}

func NewService(repository Repository, scorer Scorer) Service {
	if scorer == nil {
		scorer = NewScorer()
	}
	return &trustService{
		repository: repository,
		scorer:     scorer,
	}
}

func (s *trustService) RecordOutcome(ctx context.Context, outcome ExecutionOutcome) (TrustProfile, error) {
	if s.repository == nil {
		return TrustProfile{}, fmt.Errorf("trust repository is nil")
	}
	if s.scorer == nil {
		return TrustProfile{}, fmt.Errorf("trust scorer is nil")
	}

	outcome.ActorID = normalizeActorID(outcome.ActorID)
	if outcome.ActorID == "" {
		return TrustProfile{}, fmt.Errorf("actor id is required")
	}

	current, ok, err := s.repository.GetByActor(ctx, outcome.ActorID)
	if err != nil {
		return TrustProfile{}, err
	}
	if !ok {
		current = DefaultTrustProfile(outcome.ActorID, timeNowUTC())
	}

	updated := s.scorer.Score(current, outcome)
	if err := s.repository.Save(ctx, updated); err != nil {
		return TrustProfile{}, err
	}
	return updated, nil
}

func (s *trustService) GetTrustProfile(ctx context.Context, actorID string) (TrustProfile, bool, error) {
	if s.repository == nil {
		return TrustProfile{}, false, fmt.Errorf("trust repository is nil")
	}

	actorID = normalizeActorID(actorID)
	if actorID == "" {
		return TrustProfile{}, false, nil
	}

	return s.repository.GetByActor(ctx, actorID)
}

func timeNowUTC() time.Time {
	return time.Now().UTC()
}