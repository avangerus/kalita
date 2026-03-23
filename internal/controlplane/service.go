package controlplane

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"kalita/internal/caseruntime"
	"kalita/internal/employee"
	"kalita/internal/eventcore"
	"kalita/internal/executionruntime"
	"kalita/internal/policy"
	"kalita/internal/proposal"
	"kalita/internal/trust"
	"kalita/internal/workplan"
)

type CaseRepository interface {
	GetByID(ctx context.Context, id string) (caseruntime.Case, bool, error)
	List(ctx context.Context) ([]caseruntime.Case, error)
}

type Service struct {
	cases      CaseRepository
	work       workplan.QueueRepository
	coord      workplan.CoordinationRepository
	policy     policy.PolicyRepository
	proposals  proposal.Repository
	executions executionruntime.ExecutionRepository
	employees  employee.Directory
	trust      trust.Repository
	events     eventcore.EventLog
}

func NewService(cases CaseRepository, work workplan.QueueRepository, coord workplan.CoordinationRepository, policyRepo policy.PolicyRepository, proposals proposal.Repository, executions executionruntime.ExecutionRepository, employees employee.Directory, trustRepo trust.Repository, events eventcore.EventLog) *Service {
	return &Service{cases: cases, work: work, coord: coord, policy: policyRepo, proposals: proposals, executions: executions, employees: employees, trust: trustRepo, events: events}
}

type OperationalReason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

type CaseDetail struct {
	Case                       caseruntime.Case                   `json:"case"`
	LatestWorkItems            []workplan.WorkItem                `json:"latest_work_items"`
	LatestCoordinationDecision *workplan.CoordinationDecision     `json:"latest_coordination_decision,omitempty"`
	LatestPolicyDecision       *policy.PolicyDecision             `json:"latest_policy_decision,omitempty"`
	LatestProposal             *proposal.Proposal                 `json:"latest_proposal,omitempty"`
	LatestExecutionSession     *executionruntime.ExecutionSession `json:"latest_execution_session,omitempty"`
	PendingApprovals           []policy.ApprovalRequest           `json:"pending_approvals"`
	BlockingReason             *OperationalReason                 `json:"blocking_reason,omitempty"`
	NextExpectedStep           string                             `json:"next_expected_step"`
}

type TimelineEntry struct {
	TS                time.Time `json:"ts"`
	Kind              string    `json:"kind"`
	Title             string    `json:"title"`
	Summary           string    `json:"summary"`
	RelatedWorkItemID string    `json:"related_work_item_id,omitempty"`
}

type Summary struct {
	ActiveCases       int `json:"active_cases"`
	BlockedCases      int `json:"blocked_cases"`
	DeferredWorkItems int `json:"deferred_work_items"`
	PendingApprovals  int `json:"pending_approvals"`
	ExecutingSessions int `json:"executing_sessions"`
	FailedExecutions  int `json:"failed_executions"`
	ActorsTotal       int `json:"actors_total"`
	ActorsAvailable   int `json:"actors_available"`
	TrustLow          int `json:"low_trust_count"`
	TrustMedium       int `json:"medium_trust_count"`
	TrustHigh         int `json:"high_trust_count"`
}

func (s *Service) GetCaseDetail(ctx context.Context, caseID string) (CaseDetail, bool, error) {
	if s.cases == nil {
		return CaseDetail{}, false, fmt.Errorf("case repository is nil")
	}
	c, ok, err := s.cases.GetByID(ctx, caseID)
	if err != nil || !ok {
		return CaseDetail{}, ok, err
	}
	workItems, err := s.work.ListWorkItemsByCase(ctx, caseID)
	if err != nil {
		return CaseDetail{}, true, err
	}
	sortWorkItems(workItems)
	latestCoord, latestPolicy, latestProposal, latestExec, pendingApprovals, blockReason, err := s.aggregateCaseArtifacts(ctx, c, workItems)
	if err != nil {
		return CaseDetail{}, true, err
	}
	return CaseDetail{
		Case:                       c,
		LatestWorkItems:            workItems,
		LatestCoordinationDecision: latestCoord,
		LatestPolicyDecision:       latestPolicy,
		LatestProposal:             latestProposal,
		LatestExecutionSession:     latestExec,
		PendingApprovals:           pendingApprovals,
		BlockingReason:             blockReason,
		NextExpectedStep:           deriveNextExpectedStep(latestCoord, latestPolicy, pendingApprovals, latestProposal, latestExec, blockReason),
	}, true, nil
}

func (s *Service) GetCaseTimeline(ctx context.Context, caseID string) ([]TimelineEntry, bool, error) {
	if s.cases == nil {
		return nil, false, fmt.Errorf("case repository is nil")
	}
	c, ok, err := s.cases.GetByID(ctx, caseID)
	if err != nil || !ok {
		return nil, ok, err
	}
	entries := []TimelineEntry{{TS: c.OpenedAt, Kind: "case_created", Title: "Case created", Summary: fmt.Sprintf("Case %s created for %s", c.ID, c.Kind)}}
	if s.events != nil && strings.TrimSpace(c.CorrelationID) != "" {
		_, execEvents, err := s.events.ListByCorrelation(ctx, c.CorrelationID)
		if err != nil {
			return nil, true, err
		}
		for _, evt := range execEvents {
			entry, ok := normalizeTimelineEntry(evt)
			if ok {
				entries = append(entries, entry)
			}
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].TS.Equal(entries[j].TS) {
			if entries[i].Kind == entries[j].Kind {
				return entries[i].Title < entries[j].Title
			}
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].TS.Before(entries[j].TS)
	})
	return entries, true, nil
}

func (s *Service) GetSummary(ctx context.Context) (Summary, error) {
	cases, err := s.cases.List(ctx)
	if err != nil {
		return Summary{}, err
	}
	var out Summary
	for _, c := range cases {
		if strings.EqualFold(c.Status, string(caseruntime.CaseOpen)) || c.Status == "" {
			out.ActiveCases++
		}
		workItems, err := s.work.ListWorkItemsByCase(ctx, c.ID)
		if err != nil {
			return Summary{}, err
		}
		hasHardBlock := false
		for _, wi := range workItems {
			coords, err := s.coord.ListByWorkItem(ctx, wi.ID)
			if err != nil {
				return Summary{}, err
			}
			for _, coord := range coords {
				if coord.DecisionType == workplan.CoordinationDefer {
					out.DeferredWorkItems++
				}
				if coord.DecisionType == workplan.CoordinationBlock || coord.DecisionType == workplan.CoordinationEscalate {
					hasHardBlock = true
				}
				policies, err := s.policy.ListByCoordinationDecision(ctx, coord.ID)
				if err != nil {
					return Summary{}, err
				}
				for _, pol := range policies {
					if pol.Outcome == policy.PolicyDeny {
						hasHardBlock = true
					}
				}
				approvals, err := s.policy.ListApprovalRequestsByCoordinationDecision(ctx, coord.ID)
				if err != nil {
					return Summary{}, err
				}
				for _, approval := range approvals {
					if approval.Status == policy.ApprovalPending {
						out.PendingApprovals++
					}
				}
			}
			execs, err := s.executions.ListSessionsByWorkItem(ctx, wi.ID)
			if err != nil {
				return Summary{}, err
			}
			for _, exec := range execs {
				switch exec.Status {
				case executionruntime.ExecutionSessionPending, executionruntime.ExecutionSessionRunning, executionruntime.ExecutionSessionCompensating:
					out.ExecutingSessions++
				case executionruntime.ExecutionSessionFailed:
					out.FailedExecutions++
					hasHardBlock = true
				}
			}
		}
		if hasHardBlock {
			out.BlockedCases++
		}
	}
	employees, err := s.employees.ListEmployees(ctx)
	if err != nil {
		return Summary{}, err
	}
	out.ActorsTotal = len(employees)
	for _, actor := range employees {
		if actor.Enabled {
			out.ActorsAvailable++
		}
	}
	profiles, err := s.trust.List(ctx)
	if err != nil {
		return Summary{}, err
	}
	for _, p := range profiles {
		switch p.TrustLevel {
		case trust.TrustLow:
			out.TrustLow++
		case trust.TrustMedium:
			out.TrustMedium++
		case trust.TrustHigh:
			out.TrustHigh++
		}
	}
	return out, nil
}

func (s *Service) aggregateCaseArtifacts(ctx context.Context, c caseruntime.Case, workItems []workplan.WorkItem) (*workplan.CoordinationDecision, *policy.PolicyDecision, *proposal.Proposal, *executionruntime.ExecutionSession, []policy.ApprovalRequest, *OperationalReason, error) {
	var coords []workplan.CoordinationDecision
	var policies []policy.PolicyDecision
	var proposals []proposal.Proposal
	var executions []executionruntime.ExecutionSession
	pendingApprovals := make([]policy.ApprovalRequest, 0)
	for _, wi := range workItems {
		wiCoords, err := s.coord.ListByWorkItem(ctx, wi.ID)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
		coords = append(coords, wiCoords...)
		for _, cd := range wiCoords {
			wiPolicies, err := s.policy.ListByCoordinationDecision(ctx, cd.ID)
			if err != nil {
				return nil, nil, nil, nil, nil, nil, err
			}
			policies = append(policies, wiPolicies...)
			approvals, err := s.policy.ListApprovalRequestsByCoordinationDecision(ctx, cd.ID)
			if err != nil {
				return nil, nil, nil, nil, nil, nil, err
			}
			for _, approval := range approvals {
				if approval.Status == policy.ApprovalPending {
					pendingApprovals = append(pendingApprovals, approval)
				}
			}
		}
		wiProps, err := s.proposals.ListByWorkItem(ctx, wi.ID)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
		proposals = append(proposals, wiProps...)
		wiExecs, err := s.executions.ListSessionsByWorkItem(ctx, wi.ID)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
		executions = append(executions, wiExecs...)
	}
	sortApprovals(pendingApprovals)
	latestCoord := latestCoordination(coords)
	latestPolicy := latestPolicyDecision(policies)
	latestProposal := latestProposalEntry(proposals)
	latestExec := latestExecution(executions)
	blockReason := deriveBlockingReason(c, latestCoord, latestPolicy, pendingApprovals, latestExec)
	return latestCoord, latestPolicy, latestProposal, latestExec, pendingApprovals, blockReason, nil
}

func sortWorkItems(items []workplan.WorkItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
}
func sortApprovals(items []policy.ApprovalRequest) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
}
func latestCoordination(items []workplan.CoordinationDecision) *workplan.CoordinationDecision {
	if len(items) == 0 {
		return nil
	}
	best := items[0]
	for _, item := range items[1:] {
		if item.CreatedAt.After(best.CreatedAt) || (item.CreatedAt.Equal(best.CreatedAt) && item.ID > best.ID) {
			best = item
		}
	}
	return &best
}
func latestPolicyDecision(items []policy.PolicyDecision) *policy.PolicyDecision {
	if len(items) == 0 {
		return nil
	}
	best := items[0]
	for _, item := range items[1:] {
		if item.CreatedAt.After(best.CreatedAt) || (item.CreatedAt.Equal(best.CreatedAt) && item.ID > best.ID) {
			best = item
		}
	}
	return &best
}
func latestProposalEntry(items []proposal.Proposal) *proposal.Proposal {
	if len(items) == 0 {
		return nil
	}
	best := items[0]
	for _, item := range items[1:] {
		if item.UpdatedAt.After(best.UpdatedAt) || (item.UpdatedAt.Equal(best.UpdatedAt) && item.ID > best.ID) {
			best = item
		}
	}
	return &best
}
func latestExecution(items []executionruntime.ExecutionSession) *executionruntime.ExecutionSession {
	if len(items) == 0 {
		return nil
	}
	best := items[0]
	for _, item := range items[1:] {
		if item.UpdatedAt.After(best.UpdatedAt) || (item.UpdatedAt.Equal(best.UpdatedAt) && item.ID > best.ID) {
			best = item
		}
	}
	return &best
}

func deriveNextExpectedStep(coord *workplan.CoordinationDecision, pol *policy.PolicyDecision, approvals []policy.ApprovalRequest, prop *proposal.Proposal, exec *executionruntime.ExecutionSession, block *OperationalReason) string {
	if block != nil {
		return "await_unblock"
	}
	if len(approvals) > 0 {
		return "await_approval"
	}
	if exec != nil {
		switch exec.Status {
		case executionruntime.ExecutionSessionPending, executionruntime.ExecutionSessionRunning, executionruntime.ExecutionSessionCompensating:
			return "await_execution_completion"
		case executionruntime.ExecutionSessionFailed:
			return "review_failed_execution"
		}
	}
	if prop != nil && prop.Status == proposal.ProposalDraft {
		return "validate_proposal"
	}
	if prop != nil && prop.Status == proposal.ProposalValidated {
		return "compile_proposal"
	}
	if pol != nil && pol.Outcome == policy.PolicyAllow {
		return "start_execution"
	}
	if coord != nil && coord.DecisionType == workplan.CoordinationDefer {
		return "reschedule_work"
	}
	return "await_operator_review"
}

func deriveBlockingReason(c caseruntime.Case, coord *workplan.CoordinationDecision, pol *policy.PolicyDecision, approvals []policy.ApprovalRequest, exec *executionruntime.ExecutionSession) *OperationalReason {
	if len(approvals) > 0 {
		r := MapOperationalReason("approval required")
		return &r
	}
	if exec != nil {
		switch exec.Status {
		case executionruntime.ExecutionSessionPending, executionruntime.ExecutionSessionRunning, executionruntime.ExecutionSessionCompensating:
			r := MapOperationalReason("execution in progress")
			return &r
		case executionruntime.ExecutionSessionFailed:
			r := MapOperationalReason(exec.FailureReason)
			return &r
		}
	}
	if pol != nil && pol.Outcome == policy.PolicyDeny {
		r := MapOperationalReason(pol.Reason)
		return &r
	}
	if coord != nil && (coord.DecisionType == workplan.CoordinationBlock || coord.DecisionType == workplan.CoordinationEscalate) {
		r := MapOperationalReason(coord.Reason)
		return &r
	}
	if strings.EqualFold(c.Status, "blocked") {
		r := MapOperationalReason("waiting external input")
		return &r
	}
	return nil
}

func MapOperationalReason(raw string) OperationalReason {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(normalized, "no eligible actor"):
		return OperationalReason{"no_eligible_actor", "No eligible actor is currently available.", "Enable or assign an actor with matching capabilities."}
	case strings.Contains(normalized, "low trust"):
		return OperationalReason{"low_trust_only", "Only low-trust actors matched the work.", "Wait for a higher-trust actor or reduce autonomy requirements."}
	case strings.Contains(normalized, "approval"):
		return OperationalReason{"approval_required", "Approval is required before work can continue.", "Review the pending approval request."}
	case strings.Contains(normalized, "deny"), strings.Contains(normalized, "denied"), strings.Contains(normalized, "blocked by policy"):
		return OperationalReason{"policy_denied", "Policy denied automatic execution.", "Review the policy decision and route manually if needed."}
	case strings.Contains(normalized, "complexity"):
		return OperationalReason{"complexity_too_high", "Work complexity exceeds the currently eligible actor capacity.", "Split the work or route it to a more capable actor."}
	case strings.Contains(normalized, "in progress"), strings.Contains(normalized, "running"):
		return OperationalReason{"execution_in_progress", "Execution is already in progress.", "Wait for the current execution session to finish."}
	case strings.Contains(normalized, "external input"):
		return OperationalReason{"waiting_external_input", "The case is waiting for external input.", "Provide the required external information and retry."}
	case strings.Contains(normalized, "failed"), strings.Contains(normalized, "compensation"):
		return OperationalReason{"blocked_by_failed_execution", "A failed execution is blocking further automation.", "Inspect the failed session and decide whether to retry or compensate."}
	default:
		return OperationalReason{"waiting_external_input", strings.TrimSpace(raw), "Review the latest case artifacts for the blocking condition."}
	}
}

func normalizeTimelineEntry(evt eventcore.ExecutionEvent) (TimelineEntry, bool) {
	kind, title := "", ""
	summary := evt.Status
	related := stringFromPayload(evt.Payload, "work_item_id")
	switch evt.Step {
	case "case_resolution":
		if evt.Status != "opened_new" {
			return TimelineEntry{}, false
		}
		kind, title = "case_created", "Case created"
		summary = fmt.Sprintf("Case opened for command %s", stringFromPayload(evt.Payload, "command_type"))
	case "work_item_intake":
		kind, title = "work_item_created", "Work item created"
		summary = fmt.Sprintf("Work item %s created", related)
	case "coordination_decision_made":
		kind, title = "coordination_decided", "Coordination decided"
		summary = fmt.Sprintf("Decision: %s", stringFromPayload(evt.Payload, "decision_type"))
	case "policy_evaluation":
		kind, title = "policy_decided", "Policy decided"
		summary = fmt.Sprintf("Outcome: %s", evt.Status)
	case "approval_request_created":
		kind, title = "approval_requested", "Approval requested"
		summary = fmt.Sprintf("Approval requested for work item %s", related)
	case "proposal_created":
		kind, title = "proposal_created", "Proposal created"
		summary = fmt.Sprintf("Proposal %s created", stringFromPayload(evt.Payload, "proposal_id"))
	case "execution_session_created":
		kind, title = "execution_started", "Execution started"
		summary = fmt.Sprintf("Execution session %s started", stringFromPayload(evt.Payload, "execution_session_id"))
	case "execution_step_succeeded", "execution_compensation_succeeded":
		kind, title = "step_completed", "Step completed"
		summary = fmt.Sprintf("Step %v completed", evt.Payload["step_index"])
	case "execution_step_failed", "execution_session_failed":
		kind, title = "execution_failed", "Execution failed"
		summary = firstNonEmpty(stringFromPayload(evt.Payload, "failure_reason"), evt.Status)
	case "execution_compensation_started":
		kind, title = "compensation_started", "Compensation started"
		summary = fmt.Sprintf("Compensation started for session %s", stringFromPayload(evt.Payload, "execution_session_id"))
	default:
		return TimelineEntry{}, false
	}
	if kind == "coordination_decided" {
		decisionType := stringFromPayload(evt.Payload, "decision_type")
		if decisionType == string(workplan.CoordinationBlock) || decisionType == string(workplan.CoordinationEscalate) {
			return TimelineEntry{TS: evt.OccurredAt, Kind: "case_blocked", Title: "Case blocked", Summary: firstNonEmpty(stringFromPayload(evt.Payload, "reason"), "Case blocked"), RelatedWorkItemID: related}, true
		}
	}
	return TimelineEntry{TS: evt.OccurredAt, Kind: kind, Title: title, Summary: summary, RelatedWorkItemID: related}, true
}

func stringFromPayload(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	if v, ok := payload[key]; ok {
		return fmt.Sprint(v)
	}
	return ""
}
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
