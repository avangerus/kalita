package executionruntime

import (
	"context"
	"time"

	"kalita/internal/actionplan"
	"kalita/internal/executioncontrol"
)

type ExecutionSessionStatus string

const (
	ExecutionSessionPending      ExecutionSessionStatus = "pending"
	ExecutionSessionRunning      ExecutionSessionStatus = "running"
	ExecutionSessionSucceeded    ExecutionSessionStatus = "succeeded"
	ExecutionSessionFailed       ExecutionSessionStatus = "failed"
	ExecutionSessionCompensating ExecutionSessionStatus = "compensating"
	ExecutionSessionCompensated  ExecutionSessionStatus = "compensated"
)

type StepStatus string

const (
	StepPending      StepStatus = "pending"
	StepRunning      StepStatus = "running"
	StepSucceeded    StepStatus = "succeeded"
	StepFailed       StepStatus = "failed"
	StepCompensating StepStatus = "compensating"
	StepCompensated  StepStatus = "compensated"
)

type ExecutionSession struct {
	ID                     string
	ActionPlanID           string
	CaseID                 string
	WorkItemID             string
	CoordinationDecisionID string
	PolicyDecisionID       string
	ExecutionConstraintsID string
	Status                 ExecutionSessionStatus
	CurrentStepIndex       int
	CreatedAt              time.Time
	UpdatedAt              time.Time
	FailureReason          string
}

type StepExecution struct {
	ID                 string
	ExecutionSessionID string
	ActionID           string
	StepIndex          int
	Status             StepStatus
	StartedAt          *time.Time
	FinishedAt         *time.Time
	FailureReason      string
}

type WALRecordType string

const (
	WALStepIntent         WALRecordType = "step_intent"
	WALStepResult         WALRecordType = "step_result"
	WALCompensationIntent WALRecordType = "compensation_intent"
	WALCompensationResult WALRecordType = "compensation_result"
)

type WALRecord struct {
	ID                 string
	ExecutionSessionID string
	StepExecutionID    string
	ActionID           string
	Type               WALRecordType
	CreatedAt          time.Time
	Payload            map[string]any
}

type ExecutionRepository interface {
	SaveSession(ctx context.Context, s ExecutionSession) error
	GetSession(ctx context.Context, id string) (ExecutionSession, bool, error)
	ListSessionsByWorkItem(ctx context.Context, workItemID string) ([]ExecutionSession, error)
	SaveStep(ctx context.Context, s StepExecution) error
	GetStep(ctx context.Context, id string) (StepExecution, bool, error)
	ListStepsBySession(ctx context.Context, sessionID string) ([]StepExecution, error)
}

type WAL interface {
	Append(ctx context.Context, r WALRecord) error
	ListBySession(ctx context.Context, sessionID string) ([]WALRecord, error)
}

type ActionExecutor interface {
	ExecuteAction(ctx context.Context, action actionplan.Action, constraints executioncontrol.ExecutionConstraints) error
	CompensateAction(ctx context.Context, action actionplan.Action, constraints executioncontrol.ExecutionConstraints) error
}

type Runner interface {
	RunPlan(ctx context.Context, plan actionplan.ActionPlan, constraints executioncontrol.ExecutionConstraints, metadata RunMetadata) (ExecutionSession, error)
}

type Service interface {
	StartExecution(ctx context.Context, plan actionplan.ActionPlan, constraints executioncontrol.ExecutionConstraints, metadata RunMetadata) (ExecutionSession, error)
}

type RunMetadata struct {
	CaseID                 string
	WorkItemID             string
	CoordinationDecisionID string
	PolicyDecisionID       string
	ActorID                string
}
