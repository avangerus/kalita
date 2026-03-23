package controlplane

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"kalita/internal/capability"
	"kalita/internal/caseruntime"
	"kalita/internal/employee"
	"kalita/internal/eventcore"
	"kalita/internal/executionruntime"
	"kalita/internal/policy"
	"kalita/internal/profile"
	"kalita/internal/proposal"
	"kalita/internal/trust"
	"kalita/internal/workplan"
)

const maxRecentExecutionArtifacts = 5

type CaseLister interface {
	List(ctx context.Context) ([]caseruntime.Case, error)
}
type WorkItemLister interface {
	ListWorkItems(ctx context.Context) ([]workplan.WorkItem, error)
}
type ApprovalRequestLister interface {
	ListApprovalRequests(ctx context.Context) ([]policy.ApprovalRequest, error)
}

type service struct {
	cases        caseruntime.CaseRepository
	caseLister   CaseLister
	workItems    workplan.QueueRepository
	workLister   WorkItemLister
	coordination workplan.CoordinationRepository
	policies     policy.PolicyRepository
	approvals    ApprovalRequestLister
	proposals    proposal.Repository
	actors       employee.Directory
	trust        trust.Repository
	profiles     profile.Repository
	capabilities capability.InMemoryCapabilityRepository
	executions   executionruntime.ExecutionRepository
	wal          executionruntime.WAL
	eventLog     eventcore.EventLog
	coordinator  workplan.Coordinator
}

func NewService(
	cases caseruntime.CaseRepository,
	workItems workplan.QueueRepository,
	coordination workplan.CoordinationRepository,
	policies policy.PolicyRepository,
	proposals proposal.Repository,
	actors employee.Directory,
	trustRepo trust.Repository,
	profiles profile.Repository,
	capRepo *capability.InMemoryCapabilityRepository,
	executions executionruntime.ExecutionRepository,
	wal executionruntime.WAL,
	eventLog eventcore.EventLog,
	coordinators ...workplan.Coordinator,
) Service {
	var caseLister CaseLister
	if l, ok := cases.(CaseLister); ok {
		caseLister = l
	}
	var workLister WorkItemLister
	if l, ok := workItems.(WorkItemLister); ok {
		workLister = l
	}
	var approvals ApprovalRequestLister
	if l, ok := policies.(ApprovalRequestLister); ok {
		approvals = l
	}
	var coordinator workplan.Coordinator
	if len(coordinators) > 0 {
		coordinator = coordinators[0]
	}
	return &service{cases: cases, caseLister: caseLister, workItems: workItems, workLister: workLister, coordination: coordination, policies: policies, approvals: approvals, proposals: proposals, actors: actors, trust: trustRepo, profiles: profiles, capabilities: *capRepo, executions: executions, wal: wal, eventLog: eventLog, coordinator: coordinator}
}

func (s *service) ApproveApprovalRequest(ctx context.Context, approvalRequestID string) (ApprovalInboxItem, error) {
	return s.resolveApprovalRequest(ctx, approvalRequestID, policy.ApprovalApproved, "approval_granted")
}

func (s *service) RejectApprovalRequest(ctx context.Context, approvalRequestID string) (ApprovalInboxItem, error) {
	return s.resolveApprovalRequest(ctx, approvalRequestID, policy.ApprovalRejected, "approval_rejected")
}

func (s *service) resolveApprovalRequest(ctx context.Context, approvalRequestID string, target policy.ApprovalStatus, step string) (ApprovalInboxItem, error) {
	req, ok, err := s.policies.GetApprovalRequest(ctx, approvalRequestID)
	if err != nil {
		return ApprovalInboxItem{}, err
	}
	if !ok {
		return ApprovalInboxItem{}, fmt.Errorf("approval request %s not found", approvalRequestID)
	}
	if req.Status == target || req.Status != policy.ApprovalPending {
		return s.buildApprovalInboxItem(ctx, req), nil
	}
	now := s.approvalResolutionTime(ctx, req)
	req.Status = target
	req.ResolvedAt = &now
	req.ResolutionNote = string(target)
	if err := s.policies.SaveApprovalRequest(ctx, req); err != nil {
		return ApprovalInboxItem{}, err
	}
	if err := s.appendApprovalEvent(ctx, req, step, string(target), now); err != nil {
		return ApprovalInboxItem{}, err
	}
	if target == policy.ApprovalApproved && s.coordinator != nil {
		if err := s.recoordinateApprovedWorkItem(ctx, req); err != nil {
			return ApprovalInboxItem{}, err
		}
	}
	return s.buildApprovalInboxItem(ctx, req), nil
}

func (s *service) buildApprovalInboxItem(ctx context.Context, req policy.ApprovalRequest) ApprovalInboxItem {
	coord, _ := s.latestCoordination(ctx, req.WorkItemID)
	policyOverview, _ := s.latestPolicyApproval(ctx, coord)
	return ApprovalInboxItem{ApprovalRequestID: req.ID, Status: string(req.Status), RequestedFromRole: req.RequestedFromRole, CaseID: req.CaseID, WorkItemID: req.WorkItemID, QueueID: req.QueueID, CreatedAt: req.CreatedAt, ResolvedAt: req.ResolvedAt, ResolutionNote: req.ResolutionNote, Coordination: coord, PolicyApproval: policyOverview}
}

func (s *service) approvalResolutionTime(ctx context.Context, req policy.ApprovalRequest) time.Time {
	if req.ResolvedAt != nil {
		return *req.ResolvedAt
	}
	coord, ok, err := s.coordination.GetDecision(ctx, req.CoordinationDecisionID)
	if err == nil && ok {
		return coord.CreatedAt.Add(time.Nanosecond)
	}
	return req.CreatedAt.Add(time.Nanosecond)
}

func (s *service) appendApprovalEvent(ctx context.Context, req policy.ApprovalRequest, step string, status string, now time.Time) error {
	if s.eventLog == nil {
		return nil
	}
	c, ok, err := s.cases.GetByID(ctx, req.CaseID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return s.eventLog.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{
		ID:            fmt.Sprintf("%s-%s", req.ID, step),
		ExecutionID:   fmt.Sprintf("approval:%s", req.ID),
		CaseID:        req.CaseID,
		Step:          step,
		Status:        status,
		OccurredAt:    now,
		CorrelationID: c.CorrelationID,
		CausationID:   req.ID,
		Payload: map[string]any{
			"approval_request_id":      req.ID,
			"coordination_decision_id": req.CoordinationDecisionID,
			"policy_decision_id":       req.PolicyDecisionID,
			"case_id":                  req.CaseID,
			"work_item_id":             req.WorkItemID,
			"queue_id":                 req.QueueID,
			"approval_request_status":  req.Status,
			"approval_resolution_note": req.ResolutionNote,
		},
	})
}

func (s *service) recoordinateApprovedWorkItem(ctx context.Context, req policy.ApprovalRequest) error {
	wi, ok, err := s.workItems.GetWorkItem(ctx, req.WorkItemID)
	if err != nil || !ok {
		return err
	}
	coordinationCtx, err := s.buildCoordinationContext(ctx, wi)
	if err != nil {
		return err
	}
	planningCtx := workplan.ContextWithPlanningExecution(ctx, workplan.PlanningExecutionContext{
		ExecutionID:   fmt.Sprintf("approval:%s", req.ID),
		CorrelationID: s.caseCorrelationID(ctx, req.CaseID),
		CausationID:   req.ID,
	})
	_, err = s.coordinator.Decide(planningCtx, wi, coordinationCtx)
	return err
}

func (s *service) caseCorrelationID(ctx context.Context, caseID string) string {
	c, ok, err := s.cases.GetByID(ctx, caseID)
	if err == nil && ok {
		return c.CorrelationID
	}
	return ""
}

func (s *service) buildCoordinationContext(ctx context.Context, wi workplan.WorkItem) (workplan.CoordinationContext, error) {
	employees, err := s.actors.ListEmployees(ctx)
	if err != nil {
		return workplan.CoordinationContext{}, err
	}
	actors := make([]workplan.CoordinationActor, 0, len(employees))
	profiles := make(map[string]workplan.CoordinationActorProfile, len(employees))
	for _, emp := range employees {
		actionTypes := make([]string, 0, len(emp.AllowedActionTypes))
		for _, actionType := range emp.AllowedActionTypes {
			actionTypes = append(actionTypes, string(actionType))
		}
		actors = append(actors, workplan.CoordinationActor{ID: emp.ID, Enabled: emp.Enabled, QueueMemberships: append([]string(nil), emp.QueueMemberships...), AllowedActionTypes: actionTypes})
		profileView := workplan.CoordinationActorProfile{ActorID: emp.ID}
		if prof, ok, err := s.profiles.GetProfileByActor(ctx, emp.ID); err != nil {
			return workplan.CoordinationContext{}, err
		} else if ok {
			profileView.MaxComplexity = prof.MaxComplexity
		}
		if trustProfile, ok, err := s.trust.GetByActor(ctx, emp.ID); err != nil {
			return workplan.CoordinationContext{}, err
		} else if ok {
			profileView.TrustLevel = string(trustProfile.TrustLevel)
			profileView.TrustAvailable = true
		}
		profiles[emp.ID] = profileView
	}
	actionTypes := []string{"legacy_workflow_action"}
	complexity := 1
	if wi.ActionPlan != nil && len(wi.ActionPlan.Actions) > 0 {
		actionTypes = make([]string, 0, len(wi.ActionPlan.Actions))
		for _, action := range wi.ActionPlan.Actions {
			actionTypes = append(actionTypes, string(action.Type))
		}
		complexity = len(actionTypes)
	}
	return workplan.CoordinationContext{ActionTypes: actionTypes, Complexity: complexity, Actors: actors, Profiles: profiles}, nil
}

func (s *service) GetSummary(ctx context.Context) (Summary, error) {
	cases, err := s.ListCases(ctx)
	if err != nil {
		return Summary{}, err
	}
	items, err := s.ListWorkItems(ctx)
	if err != nil {
		return Summary{}, err
	}
	approvals, err := s.GetApprovalInbox(ctx)
	if err != nil {
		return Summary{}, err
	}
	blocked, err := s.GetBlockedOrDeferredWork(ctx)
	if err != nil {
		return Summary{}, err
	}
	summary := Summary{WorkItemCount: len(items), ApprovalPendingCount: len(approvals), BlockedOrDeferredCount: len(blocked)}
	for _, c := range cases {
		if c.Status == string(caseruntime.CaseOpen) || c.Status == "open" {
			summary.OpenCaseCount++
		}
	}
	return summary, nil
}

func (s *service) GetCaseTimeline(ctx context.Context, caseID string) ([]TimelineEntry, error) {
	c, ok, err := s.cases.GetByID(ctx, caseID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("case %s not found", caseID)
	}
	if s.eventLog == nil {
		return []TimelineEntry{}, nil
	}
	_, executionEvents, err := s.eventLog.ListByCorrelation(ctx, c.CorrelationID)
	if err != nil {
		return nil, err
	}
	timeline := make([]TimelineEntry, 0, len(executionEvents))
	for _, e := range executionEvents {
		if e.CaseID != "" && e.CaseID != caseID {
			continue
		}
		step, include := normalizeTimelineStep(e)
		if !include {
			continue
		}
		timeline = append(timeline, TimelineEntry{Step: step, Status: e.Status, OccurredAt: e.OccurredAt, Payload: clonePayload(e.Payload)})
	}
	sort.SliceStable(timeline, func(i, j int) bool { return timeline[i].OccurredAt.Before(timeline[j].OccurredAt) })
	return timeline, nil
}

func (s *service) GetCaseOverview(ctx context.Context, caseID string) (CaseOverview, error) {
	c, ok, err := s.cases.GetByID(ctx, caseID)
	if err != nil {
		return CaseOverview{}, err
	}
	if !ok {
		return CaseOverview{}, fmt.Errorf("case %s not found", caseID)
	}
	return mapCase(c), nil
}

func (s *service) ListCases(ctx context.Context) ([]CaseOverview, error) {
	if s.caseLister == nil {
		return nil, fmt.Errorf("case listing is not supported")
	}
	cases, err := s.caseLister.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]CaseOverview, 0, len(cases))
	for _, c := range cases {
		out = append(out, mapCase(c))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].OpenedAt.Before(out[j].OpenedAt) })
	return out, nil
}

func (s *service) GetWorkItemOverview(ctx context.Context, workItemID string) (WorkItemOverview, error) {
	wi, ok, err := s.workItems.GetWorkItem(ctx, workItemID)
	if err != nil {
		return WorkItemOverview{}, err
	}
	if !ok {
		return WorkItemOverview{}, fmt.Errorf("work item %s not found", workItemID)
	}
	return s.buildWorkItemOverview(ctx, wi)
}

func (s *service) ListWorkItems(ctx context.Context) ([]WorkItemOverview, error) {
	if s.workLister == nil {
		return nil, fmt.Errorf("work item listing is not supported")
	}
	items, err := s.workLister.ListWorkItems(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]WorkItemOverview, 0, len(items))
	for _, wi := range items {
		overview, err := s.buildWorkItemOverview(ctx, wi)
		if err != nil {
			return nil, err
		}
		out = append(out, overview)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *service) GetActorOverview(ctx context.Context, actorID string) (ActorOverview, error) {
	a, ok, err := s.actors.GetEmployee(ctx, actorID)
	if err != nil {
		return ActorOverview{}, err
	}
	if !ok {
		return ActorOverview{}, fmt.Errorf("actor %s not found", actorID)
	}
	return s.buildActorOverview(ctx, a)
}

func (s *service) ListActors(ctx context.Context) ([]ActorOverview, error) {
	actors, err := s.actors.ListEmployees(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ActorOverview, 0, len(actors))
	for _, a := range actors {
		overview, err := s.buildActorOverview(ctx, a)
		if err != nil {
			return nil, err
		}
		out = append(out, overview)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ActorID < out[j].ActorID })
	return out, nil
}

func (s *service) GetApprovalInbox(ctx context.Context) ([]ApprovalInboxItem, error) {
	if s.approvals == nil {
		return nil, fmt.Errorf("approval listing is not supported")
	}
	requests, err := s.approvals.ListApprovalRequests(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ApprovalInboxItem, 0, len(requests))
	for _, req := range requests {
		if req.Status != policy.ApprovalPending {
			continue
		}
		out = append(out, s.buildApprovalInboxItem(ctx, req))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *service) GetBlockedOrDeferredWork(ctx context.Context) ([]WorkItemOverview, error) {
	items, err := s.ListWorkItems(ctx)
	if err != nil {
		return nil, err
	}
	filtered := make([]WorkItemOverview, 0)
	for _, item := range items {
		if item.Coordination.DecisionType == string(workplan.CoordinationBlock) || item.Coordination.DecisionType == string(workplan.CoordinationDefer) || item.PolicyApproval.Outcome == string(policy.PolicyRequireApproval) || item.PolicyApproval.ApprovalRequestStatus == string(policy.ApprovalPending) {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (s *service) buildWorkItemOverview(ctx context.Context, wi workplan.WorkItem) (WorkItemOverview, error) {
	coord, coordDecision := s.latestCoordination(ctx, wi.ID)
	policyOverview, _ := s.latestPolicyApproval(ctx, coord)
	if policyOverview.PolicyDecisionID == "" && policyOverview.ApprovalRequestID == "" {
		policyOverview = s.latestPolicyApprovalForWorkItem(ctx, wi.ID)
	}
	proposalOverview, _ := s.latestProposal(ctx, wi.ID)
	execOverview, _ := s.latestExecution(ctx, wi.ID)
	assigned := wi.AssignedEmployeeID
	if assigned == "" && proposalOverview.ActorID != "" {
		assigned = proposalOverview.ActorID
	}
	if assigned == "" {
		if execOverview.SessionID != "" {
			assigned = wi.AssignedEmployeeID
		}
	}
	_ = coordDecision
	return WorkItemOverview{WorkItemID: wi.ID, CaseID: wi.CaseID, QueueID: wi.QueueID, Type: wi.Type, Status: wi.Status, Priority: wi.Priority, AssignedEmployeeID: assigned, PlanID: wi.PlanID, CreatedAt: wi.CreatedAt, UpdatedAt: wi.UpdatedAt, Coordination: coord, PolicyApproval: policyOverview, Proposal: proposalOverview, Execution: execOverview}, nil
}

func (s *service) latestPolicyApprovalForWorkItem(ctx context.Context, workItemID string) PolicyApprovalOverview {
	decisions, err := s.coordination.ListByWorkItem(ctx, workItemID)
	if err != nil || len(decisions) == 0 {
		return PolicyApprovalOverview{}
	}
	var (
		latestDecision *policy.PolicyDecision
		latestApproval *policy.ApprovalRequest
	)
	for _, decision := range decisions {
		policyDecisions, err := s.policies.ListByCoordinationDecision(ctx, decision.ID)
		if err == nil && len(policyDecisions) > 0 {
			current := latestBy(policyDecisions, func(d policy.PolicyDecision) string { return d.ID }, func(d policy.PolicyDecision) int64 { return d.CreatedAt.UnixNano() })
			if latestDecision == nil || current.CreatedAt.After(latestDecision.CreatedAt) || (current.CreatedAt.Equal(latestDecision.CreatedAt) && current.ID > latestDecision.ID) {
				copy := current
				latestDecision = &copy
			}
		}
		approvals, err := s.policies.ListApprovalRequestsByCoordinationDecision(ctx, decision.ID)
		if err == nil && len(approvals) > 0 {
			current := latestBy(approvals, func(a policy.ApprovalRequest) string { return a.ID }, func(a policy.ApprovalRequest) int64 { return a.CreatedAt.UnixNano() })
			if latestApproval == nil || current.CreatedAt.After(latestApproval.CreatedAt) || (current.CreatedAt.Equal(latestApproval.CreatedAt) && current.ID > latestApproval.ID) {
				copy := current
				latestApproval = &copy
			}
		}
	}
	overview := PolicyApprovalOverview{}
	if latestDecision != nil {
		overview.PolicyDecisionID = latestDecision.ID
		overview.Outcome = string(latestDecision.Outcome)
		overview.Reason = latestDecision.Reason
		overview.CreatedAt = latestDecision.CreatedAt
	}
	if latestApproval != nil {
		overview.ApprovalRequestID = latestApproval.ID
		overview.ApprovalRequestStatus = string(latestApproval.Status)
		overview.RequestedFromRole = latestApproval.RequestedFromRole
		overview.ApprovalRequestedAt = latestApproval.CreatedAt
		overview.ApprovalResolvedAt = latestApproval.ResolvedAt
		overview.ResolutionNote = latestApproval.ResolutionNote
	}
	return overview
}

func (s *service) buildActorOverview(ctx context.Context, actor employee.DigitalEmployee) (ActorOverview, error) {
	overview := ActorOverview{ActorID: actor.ID, Role: actor.Role, Enabled: actor.Enabled, QueueMemberships: append([]string(nil), actor.QueueMemberships...)}
	if p, ok, err := s.trust.GetByActor(ctx, actor.ID); err != nil {
		return ActorOverview{}, err
	} else if ok {
		overview.TrustLevel = string(p.TrustLevel)
		overview.AutonomyTier = string(p.AutonomyTier)
	}
	if prof, ok, err := s.profiles.GetProfileByActor(ctx, actor.ID); err != nil {
		return ActorOverview{}, err
	} else if ok {
		overview.ProfileSummary = profileSummary(prof)
	}
	caps, err := s.capabilities.ListByActor(ctx, actor.ID)
	if err != nil {
		return ActorOverview{}, err
	}
	allCaps, err := s.capabilities.ListCapabilities(ctx)
	if err != nil {
		return ActorOverview{}, err
	}
	capByID := make(map[string]capability.Capability, len(allCaps))
	for _, c := range allCaps {
		capByID[c.ID] = c
	}
	parts := make([]string, 0, len(caps))
	for _, ac := range caps {
		code := ac.CapabilityID
		if c, ok := capByID[ac.CapabilityID]; ok {
			code = c.Code
		}
		parts = append(parts, fmt.Sprintf("%s@L%d", code, ac.Level))
	}
	sort.Strings(parts)
	overview.CapabilitySummary = strings.Join(parts, ", ")
	return overview, nil
}

func (s *service) latestCoordination(ctx context.Context, workItemID string) (CoordinationOverview, *workplan.CoordinationDecision) {
	decisions, err := s.coordination.ListByWorkItem(ctx, workItemID)
	if err != nil || len(decisions) == 0 {
		return CoordinationOverview{}, nil
	}
	latest := latestBy(decisions, func(d workplan.CoordinationDecision) string { return d.ID }, func(d workplan.CoordinationDecision) int64 { return d.CreatedAt.UnixNano() })
	return CoordinationOverview{DecisionID: latest.ID, DecisionType: string(latest.DecisionType), Priority: latest.Priority, Reason: latest.Reason, CreatedAt: latest.CreatedAt}, &latest
}

func (s *service) latestPolicyApproval(ctx context.Context, coord CoordinationOverview) (PolicyApprovalOverview, *policy.PolicyDecision) {
	if coord.DecisionID == "" {
		return PolicyApprovalOverview{}, nil
	}
	decisions, err := s.policies.ListByCoordinationDecision(ctx, coord.DecisionID)
	if err != nil || len(decisions) == 0 {
		return PolicyApprovalOverview{}, nil
	}
	latestDecision := latestBy(decisions, func(d policy.PolicyDecision) string { return d.ID }, func(d policy.PolicyDecision) int64 { return d.CreatedAt.UnixNano() })
	overview := PolicyApprovalOverview{PolicyDecisionID: latestDecision.ID, Outcome: string(latestDecision.Outcome), Reason: latestDecision.Reason, CreatedAt: latestDecision.CreatedAt}
	approvals, err := s.policies.ListApprovalRequestsByCoordinationDecision(ctx, coord.DecisionID)
	if err == nil && len(approvals) > 0 {
		latestApproval := latestBy(approvals, func(a policy.ApprovalRequest) string { return a.ID }, func(a policy.ApprovalRequest) int64 { return a.CreatedAt.UnixNano() })
		overview.ApprovalRequestID = latestApproval.ID
		overview.ApprovalRequestStatus = string(latestApproval.Status)
		overview.RequestedFromRole = latestApproval.RequestedFromRole
		overview.ApprovalRequestedAt = latestApproval.CreatedAt
		overview.ApprovalResolvedAt = latestApproval.ResolvedAt
		overview.ResolutionNote = latestApproval.ResolutionNote
	}
	return overview, &latestDecision
}

func (s *service) latestProposal(ctx context.Context, workItemID string) (ProposalOverview, *proposal.Proposal) {
	proposals, err := s.proposals.ListByWorkItem(ctx, workItemID)
	if err != nil || len(proposals) == 0 {
		return ProposalOverview{}, nil
	}
	latest := latestBy(proposals, func(p proposal.Proposal) string { return p.ID }, func(p proposal.Proposal) int64 { return p.CreatedAt.UnixNano() })
	return ProposalOverview{ProposalID: latest.ID, Type: string(latest.Type), Status: string(latest.Status), ActorID: latest.ActorID, Justification: latest.Justification, ActionPlanID: latest.ActionPlanID, CreatedAt: latest.CreatedAt, UpdatedAt: latest.UpdatedAt}, &latest
}

func (s *service) latestExecution(ctx context.Context, workItemID string) (ExecutionOverview, *executionruntime.ExecutionSession) {
	sessions, err := s.executions.ListSessionsByWorkItem(ctx, workItemID)
	if err != nil || len(sessions) == 0 {
		return ExecutionOverview{}, nil
	}
	latest := latestBy(sessions, func(es executionruntime.ExecutionSession) string { return es.ID }, func(es executionruntime.ExecutionSession) int64 { return es.CreatedAt.UnixNano() })
	overview := ExecutionOverview{SessionID: latest.ID, Status: string(latest.Status), CurrentStepIndex: latest.CurrentStepIndex, FailureReason: latest.FailureReason, CreatedAt: latest.CreatedAt, UpdatedAt: latest.UpdatedAt}
	steps, err := s.executions.ListStepsBySession(ctx, latest.ID)
	if err == nil && len(steps) > 0 {
		sort.SliceStable(steps, func(i, j int) bool {
			if steps[i].StepIndex == steps[j].StepIndex {
				return steps[i].ID > steps[j].ID
			}
			return steps[i].StepIndex > steps[j].StepIndex
		})
		for i, step := range steps {
			if i >= maxRecentExecutionArtifacts {
				break
			}
			overview.RecentStepExecutions = append(overview.RecentStepExecutions, StepExecutionOverview{StepExecutionID: step.ID, ActionID: step.ActionID, StepIndex: step.StepIndex, Status: string(step.Status), StartedAt: step.StartedAt, FinishedAt: step.FinishedAt, FailureReason: step.FailureReason})
		}
	}
	records, err := s.wal.ListBySession(ctx, latest.ID)
	if err == nil && len(records) > 0 {
		sort.SliceStable(records, func(i, j int) bool {
			if records[i].CreatedAt.Equal(records[j].CreatedAt) {
				return records[i].ID > records[j].ID
			}
			return records[i].CreatedAt.After(records[j].CreatedAt)
		})
		for i, record := range records {
			if i >= maxRecentExecutionArtifacts {
				break
			}
			overview.RecentWALRecords = append(overview.RecentWALRecords, WALRecordOverview{WALRecordID: record.ID, StepExecutionID: record.StepExecutionID, ActionID: record.ActionID, Type: string(record.Type), CreatedAt: record.CreatedAt})
		}
	}
	return overview, &latest
}

func mapCase(c caseruntime.Case) CaseOverview {
	return CaseOverview{CaseID: c.ID, Kind: c.Kind, Status: c.Status, CorrelationID: c.CorrelationID, SubjectRef: c.SubjectRef, OpenedAt: c.OpenedAt, UpdatedAt: c.UpdatedAt}
}

func profileSummary(p profile.CompetencyProfile) string {
	parts := []string{p.Name}
	if p.ExecutionStyle != "" {
		parts = append(parts, fmt.Sprintf("style=%s", p.ExecutionStyle))
	}
	if p.MaxComplexity > 0 {
		parts = append(parts, fmt.Sprintf("max_complexity=%d", p.MaxComplexity))
	}
	if len(p.PreferredWorkKinds) > 0 {
		parts = append(parts, fmt.Sprintf("prefers=%s", strings.Join(p.PreferredWorkKinds, "/")))
	}
	return strings.Join(parts, "; ")
}

// latestBy chooses the artifact with the greatest CreatedAt-style timestamp; ties are broken by lexical ID order so results stay deterministic.
func latestBy[T any](items []T, id func(T) string, ts func(T) int64) T {
	latest := items[0]
	for _, item := range items[1:] {
		if ts(item) > ts(latest) || (ts(item) == ts(latest) && id(item) > id(latest)) {
			latest = item
		}
	}
	return latest
}

func normalizeTimelineStep(e eventcore.ExecutionEvent) (string, bool) {
	switch e.Step {
	case "case_resolution":
		if e.Status == "opened_new" {
			return "case_created", true
		}
		return "case_resolved", true
	case "work_item_intake":
		return "work_item_created", true
	case "coordination_decision_made":
		return "coordination_decided", true
	case "policy_evaluation":
		return "policy_decided", true
	case "approval_request_created":
		return "approval_requested", true
	case "approval_granted":
		return "approval_granted", true
	case "approval_rejected":
		return "approval_rejected", true
	case "execution_session_created":
		return "execution_started", true
	default:
		return "", false
	}
}

func clonePayload(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
