package controlplane

import "time"

type Summary struct {
	OpenCaseCount          int            `json:"open_case_count"`
	WorkItemCount          int            `json:"work_item_count"`
	ApprovalPendingCount   int            `json:"approval_pending_count"`
	BlockedOrDeferredCount int            `json:"blocked_or_deferred_count"`
	ExecutingSessionCount  int            `json:"executing_session_count"`
	DeferredCount          int            `json:"deferred_count"`
	BlockedCount           int            `json:"blocked_count"`
	TrustLevelCounts       map[string]int `json:"trust_level_counts,omitempty"`
}

type TimelineEntry struct {
	Step       string         `json:"step"`
	Status     string         `json:"status,omitempty"`
	OccurredAt time.Time      `json:"occurred_at"`
	Payload    map[string]any `json:"payload,omitempty"`
}

type CaseOverview struct {
	CaseID        string    `json:"case_id"`
	Kind          string    `json:"kind"`
	Status        string    `json:"status"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	SubjectRef    string    `json:"subject_ref,omitempty"`
	OpenedAt      time.Time `json:"opened_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CoordinationOverview struct {
	DecisionID   string    `json:"decision_id,omitempty"`
	DecisionType string    `json:"decision_type,omitempty"`
	Priority     int       `json:"priority,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	CreatedAt    time.Time `json:"created_at,omitempty"`
}

type PolicyApprovalOverview struct {
	PolicyDecisionID      string     `json:"policy_decision_id,omitempty"`
	Outcome               string     `json:"outcome,omitempty"`
	Reason                string     `json:"reason,omitempty"`
	CreatedAt             time.Time  `json:"created_at,omitempty"`
	ApprovalRequestID     string     `json:"approval_request_id,omitempty"`
	ApprovalRequestStatus string     `json:"approval_request_status,omitempty"`
	RequestedFromRole     string     `json:"requested_from_role,omitempty"`
	ApprovalRequestedAt   time.Time  `json:"approval_requested_at,omitempty"`
	ApprovalResolvedAt    *time.Time `json:"approval_resolved_at,omitempty"`
	ResolutionNote        string     `json:"resolution_note,omitempty"`
}

type ProposalOverview struct {
	ProposalID    string    `json:"proposal_id,omitempty"`
	Type          string    `json:"type,omitempty"`
	Status        string    `json:"status,omitempty"`
	ActorID       string    `json:"actor_id,omitempty"`
	Justification string    `json:"justification,omitempty"`
	ActionPlanID  string    `json:"action_plan_id,omitempty"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

type StepExecutionOverview struct {
	StepExecutionID string     `json:"step_execution_id"`
	ActionID        string     `json:"action_id"`
	StepIndex       int        `json:"step_index"`
	Status          string     `json:"status"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	FailureReason   string     `json:"failure_reason,omitempty"`
}

type WALRecordOverview struct {
	WALRecordID     string    `json:"wal_record_id"`
	StepExecutionID string    `json:"step_execution_id,omitempty"`
	ActionID        string    `json:"action_id,omitempty"`
	Type            string    `json:"type"`
	CreatedAt       time.Time `json:"created_at"`
}

type ExecutionOverview struct {
	SessionID            string                  `json:"session_id,omitempty"`
	Status               string                  `json:"session_status,omitempty"`
	CurrentStepIndex     int                     `json:"current_step_index,omitempty"`
	FailureReason        string                  `json:"failure_reason,omitempty"`
	CreatedAt            time.Time               `json:"created_at,omitempty"`
	UpdatedAt            time.Time               `json:"updated_at,omitempty"`
	RecentStepExecutions []StepExecutionOverview `json:"recent_step_executions,omitempty"`
	RecentWALRecords     []WALRecordOverview     `json:"recent_wal_records,omitempty"`
}

type WorkItemOverview struct {
	WorkItemID         string                 `json:"work_item_id"`
	CaseID             string                 `json:"case_id"`
	QueueID            string                 `json:"queue_id"`
	Type               string                 `json:"type"`
	Status             string                 `json:"status"`
	Priority           string                 `json:"priority,omitempty"`
	AssignedEmployeeID string                 `json:"assigned_employee_id,omitempty"`
	PlanID             string                 `json:"plan_id,omitempty"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
	Coordination       CoordinationOverview   `json:"coordination,omitempty"`
	PolicyApproval     PolicyApprovalOverview `json:"policy_approval,omitempty"`
	Proposal           ProposalOverview       `json:"proposal,omitempty"`
	Execution          ExecutionOverview      `json:"execution,omitempty"`
}

type ActorOverview struct {
	ActorID           string   `json:"actor_id"`
	Role              string   `json:"role"`
	Enabled           bool     `json:"enabled"`
	QueueMemberships  []string `json:"queue_memberships,omitempty"`
	TrustLevel        string   `json:"trust_level,omitempty"`
	AutonomyTier      string   `json:"autonomy_tier,omitempty"`
	SuccessCount      int      `json:"success_count,omitempty"`
	FailureCount      int      `json:"failure_count,omitempty"`
	CompensationCount int      `json:"compensation_count,omitempty"`
	ProfileSummary    string   `json:"profile_summary,omitempty"`
	CapabilitySummary string   `json:"capability_summary,omitempty"`
}

type ApprovalInboxItem struct {
	ApprovalRequestID string                 `json:"approval_request_id"`
	Status            string                 `json:"status"`
	RequestedFromRole string                 `json:"requested_from_role,omitempty"`
	CaseID            string                 `json:"case_id"`
	WorkItemID        string                 `json:"work_item_id"`
	QueueID           string                 `json:"queue_id"`
	CreatedAt         time.Time              `json:"created_at"`
	ResolvedAt        *time.Time             `json:"resolved_at,omitempty"`
	ResolutionNote    string                 `json:"resolution_note,omitempty"`
	Coordination      CoordinationOverview   `json:"coordination,omitempty"`
	PolicyApproval    PolicyApprovalOverview `json:"policy_approval,omitempty"`
}
