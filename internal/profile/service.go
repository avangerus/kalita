package profile

import "context"

type profileService struct {
	repository Repository
}

func NewService(repository Repository) Service {
	return &profileService{repository: repository}
}

func (s *profileService) GetActorProfile(ctx context.Context, actorID string) (CompetencyProfile, bool, error) {
	if s.repository == nil {
		return CompetencyProfile{}, false, nil
	}
	return s.repository.GetProfileByActor(ctx, actorID)
}
