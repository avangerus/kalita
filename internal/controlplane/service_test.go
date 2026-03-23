package controlplane

import (
	"context"
	"testing"
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

func TestGetCaseDetailAggregatesArtifacts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := testControlPlaneService(t)

	detail, ok, err := svc.GetCaseDetail(ctx, "case-1")
	if err != nil || !ok {
		t.Fatalf("GetCaseDetail ok=%v err=%v", ok, err)
	}
	if detail.Case.ID != "case-1" || len(detail.LatestWorkItems) != 2 {
		t.Fatalf("detail=%#v", detail)
	}
	if detail.LatestCoordinationDecision == nil || detail.LatestCoordinationDecision.ID != "coord-2" {
		t.Fatalf("latest coordination = %#v", detail.LatestCoordinationDecision)
	}
	if detail.LatestPolicyDecision == nil || detail.LatestPolicyDecision.ID != "policy-2" {
		t.Fatalf("latest policy = %#v", detail.LatestPolicyDecision)
	}
	if detail.LatestProposal == nil || detail.LatestProposal.ID != "proposal-b" {
		t.Fatalf("latest proposal = %#v", detail.LatestProposal)
	}
	if detail.LatestExecutionSession == nil || detail.LatestExecutionSession.ID != "exec-b" {
		t.Fatalf("latest execution = %#v", detail.LatestExecutionSession)
	}
	if len(detail.PendingApprovals) != 1 || detail.PendingApprovals[0].ID != "approval-1" {
		t.Fatalf("pending approvals = %#v", detail.PendingApprovals)
	}
	if detail.BlockingReason == nil || detail.BlockingReason.Code != "approval_required" {
		t.Fatalf("blocking reason = %#v", detail.BlockingReason)
	}
	if detail.NextExpectedStep != "await_unblock" {
		t.Fatalf("next expected step = %q", detail.NextExpectedStep)
	}
}

func TestGetCaseTimelineNormalizesAndOrdersEntries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := testControlPlaneService(t)

	entries, ok, err := svc.GetCaseTimeline(ctx, "case-1")
	if err != nil || !ok {
		t.Fatalf("GetCaseTimeline ok=%v err=%v", ok, err)
	}
	if len(entries) < 6 {
		t.Fatalf("entries=%#v", entries)
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].TS.Before(entries[i-1].TS) {
			t.Fatalf("timeline not sorted: %#v", entries)
		}
	}
	wantKinds := []string{"case_created", "work_item_created", "coordination_decided", "policy_decided", "approval_requested", "proposal_created", "execution_started", "step_completed", "execution_failed", "compensation_started", "case_blocked"}
	seen := map[string]bool{}
	for _, entry := range entries {
		seen[entry.Kind] = true
	}
	for _, kind := range wantKinds {
		if !seen[kind] {
			t.Fatalf("missing kind %q in %#v", kind, entries)
		}
	}
}

func TestMapOperationalReason(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"no eligible actor in queue":                  "no_eligible_actor",
		"only low trust actors available":             "low_trust_only",
		"manager approval required":                   "approval_required",
		"blocked by policy":                           "policy_denied",
		"complexity exceeds threshold":                "complexity_too_high",
		"execution in progress":                       "execution_in_progress",
		"waiting external input from customer":        "waiting_external_input",
		"failed execution after compensation attempt": "blocked_by_failed_execution",
	}
	for input, want := range cases {
		if got := MapOperationalReason(input).Code; got != want {
			t.Fatalf("MapOperationalReason(%q) = %q want %q", input, got, want)
		}
	}
}

func TestGetSummaryCountsOperationalState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := testControlPlaneService(t)

	summary, err := svc.GetSummary(ctx)
	if err != nil {
		t.Fatalf("GetSummary error = %v", err)
	}
	if summary.ActiveCases != 2 || summary.BlockedCases != 2 || summary.DeferredWorkItems != 1 || summary.PendingApprovals != 1 || summary.ExecutingSessions != 1 || summary.FailedExecutions != 1 || summary.ActorsTotal != 2 || summary.ActorsAvailable != 1 || summary.TrustLow != 1 || summary.TrustMedium != 1 || summary.TrustHigh != 0 {
		t.Fatalf("summary=%#v", summary)
	}
}

func testControlPlaneService(t *testing.T) (*Service, *eventcore.InMemoryEventLog) {
	t.Helper()
	ctx := context.Background()
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	workRepo := workplan.NewInMemoryQueueRepository()
	coordRepo := workplan.NewInMemoryCoordinationRepository()
	policyRepo := policy.NewInMemoryRepository()
	proposalRepo := proposal.NewInMemoryRepository()
	execRepo := executionruntime.NewInMemoryExecutionRepository()
	employees := employee.NewInMemoryDirectory()
	trustRepo := trust.NewInMemoryRepository()
	log := eventcore.NewInMemoryEventLog()
	base := time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC)

	must(t, caseRepo.Save(ctx, caseruntime.Case{ID: "case-1", Kind: "workflow.action", Status: string(caseruntime.CaseOpen), CorrelationID: "corr-1", OpenedAt: base, UpdatedAt: base.Add(10 * time.Minute)}))
	must(t, caseRepo.Save(ctx, caseruntime.Case{ID: "case-2", Kind: "workflow.action", Status: string(caseruntime.CaseOpen), CorrelationID: "corr-2", OpenedAt: base.Add(time.Hour), UpdatedAt: base.Add(time.Hour)}))

	must(t, workRepo.SaveWorkItem(ctx, workplan.WorkItem{ID: "work-a", CaseID: "case-1", QueueID: "q-1", Status: string(workplan.WorkItemOpen), CreatedAt: base.Add(time.Minute), UpdatedAt: base.Add(time.Minute)}))
	must(t, workRepo.SaveWorkItem(ctx, workplan.WorkItem{ID: "work-b", CaseID: "case-1", QueueID: "q-1", Status: string(workplan.WorkItemOpen), CreatedAt: base.Add(2 * time.Minute), UpdatedAt: base.Add(2 * time.Minute)}))
	must(t, workRepo.SaveWorkItem(ctx, workplan.WorkItem{ID: "work-c", CaseID: "case-2", QueueID: "q-1", Status: string(workplan.WorkItemOpen), CreatedAt: base.Add(3 * time.Minute), UpdatedAt: base.Add(3 * time.Minute)}))

	must(t, coordRepo.SaveDecision(ctx, workplan.CoordinationDecision{ID: "coord-1", CaseID: "case-1", WorkItemID: "work-a", QueueID: "q-1", DecisionType: workplan.CoordinationExecuteNow, CreatedAt: base.Add(3 * time.Minute)}))
	must(t, coordRepo.SaveDecision(ctx, workplan.CoordinationDecision{ID: "coord-2", CaseID: "case-1", WorkItemID: "work-b", QueueID: "q-1", DecisionType: workplan.CoordinationDefer, Reason: "manager approval required", CreatedAt: base.Add(4 * time.Minute)}))
	must(t, coordRepo.SaveDecision(ctx, workplan.CoordinationDecision{ID: "coord-3", CaseID: "case-2", WorkItemID: "work-c", QueueID: "q-1", DecisionType: workplan.CoordinationBlock, Reason: "no eligible actor in queue", CreatedAt: base.Add(5 * time.Minute)}))

	must(t, policyRepo.SaveDecision(ctx, policy.PolicyDecision{ID: "policy-1", CoordinationDecisionID: "coord-1", CaseID: "case-1", WorkItemID: "work-a", QueueID: "q-1", Outcome: policy.PolicyAllow, CreatedAt: base.Add(5 * time.Minute)}))
	must(t, policyRepo.SaveDecision(ctx, policy.PolicyDecision{ID: "policy-2", CoordinationDecisionID: "coord-2", CaseID: "case-1", WorkItemID: "work-b", QueueID: "q-1", Outcome: policy.PolicyRequireApproval, Reason: "manager approval required", CreatedAt: base.Add(6 * time.Minute)}))
	must(t, policyRepo.SaveApprovalRequest(ctx, policy.ApprovalRequest{ID: "approval-1", CoordinationDecisionID: "coord-2", PolicyDecisionID: "policy-2", CaseID: "case-1", WorkItemID: "work-b", QueueID: "q-1", Status: policy.ApprovalPending, CreatedAt: base.Add(7 * time.Minute)}))

	must(t, proposalRepo.Save(ctx, proposal.Proposal{ID: "proposal-a", CaseID: "case-1", WorkItemID: "work-a", Status: proposal.ProposalDraft, CreatedAt: base.Add(7 * time.Minute), UpdatedAt: base.Add(7 * time.Minute)}))
	must(t, proposalRepo.Save(ctx, proposal.Proposal{ID: "proposal-b", CaseID: "case-1", WorkItemID: "work-b", Status: proposal.ProposalValidated, CreatedAt: base.Add(8 * time.Minute), UpdatedAt: base.Add(8 * time.Minute)}))

	must(t, execRepo.SaveSession(ctx, executionruntime.ExecutionSession{ID: "exec-a", CaseID: "case-1", WorkItemID: "work-a", Status: executionruntime.ExecutionSessionRunning, CreatedAt: base.Add(8 * time.Minute), UpdatedAt: base.Add(9 * time.Minute)}))
	must(t, execRepo.SaveSession(ctx, executionruntime.ExecutionSession{ID: "exec-b", CaseID: "case-1", WorkItemID: "work-b", Status: executionruntime.ExecutionSessionFailed, FailureReason: "failed execution after compensation attempt", CreatedAt: base.Add(9 * time.Minute), UpdatedAt: base.Add(10 * time.Minute)}))

	must(t, employees.SaveEmployee(ctx, employee.DigitalEmployee{ID: "actor-1", Enabled: true}))
	must(t, employees.SaveEmployee(ctx, employee.DigitalEmployee{ID: "actor-2", Enabled: false}))
	must(t, trustRepo.Save(ctx, trust.TrustProfile{ActorID: "actor-1", TrustLevel: trust.TrustMedium}))
	must(t, trustRepo.Save(ctx, trust.TrustProfile{ActorID: "actor-2", TrustLevel: trust.TrustLow}))

	events := []eventcore.ExecutionEvent{
		{ID: "evt-1", CaseID: "case-1", CorrelationID: "corr-1", OccurredAt: base, Step: "case_resolution", Status: "opened_new", Payload: map[string]any{"command_type": "workflow.action"}},
		{ID: "evt-2", CaseID: "case-1", CorrelationID: "corr-1", OccurredAt: base.Add(time.Minute), Step: "work_item_intake", Status: "created", Payload: map[string]any{"work_item_id": "work-a"}},
		{ID: "evt-3", CaseID: "case-1", CorrelationID: "corr-1", OccurredAt: base.Add(2 * time.Minute), Step: "coordination_decision_made", Status: string(workplan.CoordinationExecuteNow), Payload: map[string]any{"work_item_id": "work-a", "decision_type": string(workplan.CoordinationExecuteNow)}},
		{ID: "evt-4", CaseID: "case-1", CorrelationID: "corr-1", OccurredAt: base.Add(3 * time.Minute), Step: "policy_evaluation", Status: string(policy.PolicyAllow), Payload: map[string]any{"work_item_id": "work-a"}},
		{ID: "evt-5", CaseID: "case-1", CorrelationID: "corr-1", OccurredAt: base.Add(4 * time.Minute), Step: "approval_request_created", Status: string(policy.ApprovalPending), Payload: map[string]any{"work_item_id": "work-b"}},
		{ID: "evt-6", CaseID: "case-1", CorrelationID: "corr-1", OccurredAt: base.Add(5 * time.Minute), Step: "proposal_created", Status: string(proposal.ProposalDraft), Payload: map[string]any{"proposal_id": "proposal-a", "work_item_id": "work-a"}},
		{ID: "evt-7", CaseID: "case-1", CorrelationID: "corr-1", OccurredAt: base.Add(6 * time.Minute), Step: "execution_session_created", Status: string(executionruntime.ExecutionSessionPending), Payload: map[string]any{"execution_session_id": "exec-a", "work_item_id": "work-a"}},
		{ID: "evt-8", CaseID: "case-1", CorrelationID: "corr-1", OccurredAt: base.Add(7 * time.Minute), Step: "execution_step_succeeded", Status: string(executionruntime.StepSucceeded), Payload: map[string]any{"step_index": 0, "work_item_id": "work-a"}},
		{ID: "evt-9", CaseID: "case-1", CorrelationID: "corr-1", OccurredAt: base.Add(8 * time.Minute), Step: "execution_step_failed", Status: string(executionruntime.StepFailed), Payload: map[string]any{"failure_reason": "boom", "work_item_id": "work-b"}},
		{ID: "evt-10", CaseID: "case-1", CorrelationID: "corr-1", OccurredAt: base.Add(9 * time.Minute), Step: "execution_compensation_started", Status: string(executionruntime.ExecutionSessionCompensating), Payload: map[string]any{"execution_session_id": "exec-b", "work_item_id": "work-b"}},
		{ID: "evt-11", CaseID: "case-1", CorrelationID: "corr-1", OccurredAt: base.Add(10 * time.Minute), Step: "coordination_decision_made", Status: string(workplan.CoordinationBlock), Payload: map[string]any{"work_item_id": "work-b", "decision_type": string(workplan.CoordinationBlock), "reason": "no eligible actor in queue"}},
	}
	for _, evt := range events {
		must(t, log.AppendExecutionEvent(ctx, evt))
	}
	return NewService(caseRepo, workRepo, coordRepo, policyRepo, proposalRepo, execRepo, employees, trustRepo, log), log
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
