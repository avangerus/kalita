package eventcore

import "time"

type ActorType string

const (
	ActorHuman           ActorType = "human"
	ActorService         ActorType = "service"
	ActorDigitalEmployee ActorType = "digital_employee"
	ActorSystem          ActorType = "system"
)

type ActorContext struct {
	ActorID      string
	ActorType    ActorType
	DisplayName  string
	Roles        []string
	Capabilities []string
	RequestID    string
}

type Event struct {
	ID            string
	Type          string
	OccurredAt    time.Time
	Source        string
	CorrelationID string
	CausationID   string
	ExecutionID   string
	CaseID        string
	Actor         ActorContext
	Payload       map[string]any
}

type Command struct {
	ID             string
	Type           string
	RequestedAt    time.Time
	CorrelationID  string
	CausationID    string
	ExecutionID    string
	CaseID         string
	Actor          ActorContext
	TargetRef      string
	Payload        map[string]any
	IdempotencyKey string
}

type ExecutionEvent struct {
	ID            string
	ExecutionID   string
	CaseID        string
	Step          string
	Status        string
	OccurredAt    time.Time
	CorrelationID string
	CausationID   string
	Payload       map[string]any
}

type IDGenerator interface {
	NewID() string
}

type Clock interface {
	Now() time.Time
}
