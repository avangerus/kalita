package caseruntime

import (
	"context"
	"time"

	"kalita/internal/eventcore"
)

type CaseStatus string

const (
	CaseOpen   CaseStatus = "open"
	CaseClosed CaseStatus = "closed"
)

type Case struct {
	ID            string
	Kind          string
	Status        string
	Title         string
	SubjectRef    string
	CorrelationID string
	OpenedAt      time.Time
	UpdatedAt     time.Time
	OwnerQueueID  string
	CurrentPlanID string
	Attributes    map[string]any
}

type CaseRepository interface {
	Save(ctx context.Context, c Case) error
	GetByID(ctx context.Context, id string) (Case, bool, error)
	FindByCorrelation(ctx context.Context, correlationID string) (Case, bool, error)
	FindBySubjectRef(ctx context.Context, subjectRef string) (Case, bool, error)
}

type CaseResolver interface {
	ResolveForCommand(ctx context.Context, cmd eventcore.Command) (Case, bool, error)
}
