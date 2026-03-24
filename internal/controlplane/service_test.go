package controlplane

import (
	"context"
	"testing"
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

func TestCaseOverviewAggregation(t *testing.T) {
	t.Parallel()
	svc := seededService(t)
	overview, err := svc.GetCaseOverview(context.Background(), "case-1")
	if err != nil {
		t.Fatalf("GetCaseOverview error = %v", err)
	}
	if overview.Kind != "workflow.action" || overview.CorrelationID != "corr-1" || overview.SubjectRef != "subject-1" {
		t.Fatalf("overview = %#v", overview)
	}
}

func TestWorkItemOverviewAggregationUsesLatestArtifacts(t *testing.T) {
	t.Parallel()
	svc := seededService(t)
	overview, err := svc.GetWorkItemOverview(context.Background(), "work-1")
	if err != nil {
		t.Fatalf("GetWorkItemOverview error = %v", err)
	}
	if overview.Coordination.DecisionType != string(workplan.CoordinationDefer) {
		t.Fatalf("coordination = %#v", overview.Coordination)
	}
	if overview.PolicyApproval.Outcome != string(policy.PolicyRequireApproval) || overview.PolicyApproval.ApprovalRequestStatus != string(policy.ApprovalPending) {
		t.Fatalf("policy = %#v", overview.PolicyApproval)
	}
	if overview.Proposal.ProposalID != "proposal-2" || overview.Proposal.ActionPlanID != "plan-compiled" {
		t.Fatalf("proposal = %#v", overview.Proposal)
	}
	if overview.Execution.SessionID != "exec-2" || overview.Execution.FailureReason != "operator waiting" {
		t.Fatalf("execution = %#v", overview.Execution)
	}
	if len(overview.Execution.RecentStepExecutions) == 0 || overview.Execution.RecentStepExecutions[0].StepExecutionID != "step-2" {
		t.Fatalf("steps = %#v", overview.Execution.RecentStepExecutions)
	}
	if len(overview.Execution.RecentWALRecords) == 0 || overview.Execution.RecentWALRecords[0].WALRecordID != "wal-2" {
		t.Fatalf("wal = %#v", overview.Execution.RecentWALRecords)
	}
}

func TestActorOverviewAggregationSummarizesTrustProfileAndCapabilities(t *testing.T) {
	t.Parallel()
	svc := seededService(t)
	overview, err := svc.GetActorOverview(context.Background(), "actor-1")
	if err != nil {
		t.Fatalf("GetActorOverview error = %v", err)
	}
	if overview.TrustLevel != string(trust.TrustHigh) || overview.AutonomyTier != string(trust.AutonomyStandard) {
		t.Fatalf("overview = %#v", overview)
	}
	if overview.ProfileSummary == "" || overview.CapabilitySummary != "workflow.execute@L2" {
		t.Fatalf("overview = %#v", overview)
	}
}

func TestApprovalInboxContents(t *testing.T) {
	t.Parallel()
	svc := seededService(t)
	items, err := svc.GetApprovalInbox(context.Background())
	if err != nil {
		t.Fatalf("GetApprovalInbox error = %v", err)
	}
	if len(items) != 1 || items[0].ApprovalRequestID != "approval-1" || items[0].Status != string(policy.ApprovalPending) {
		t.Fatalf("items = %#v", items)
	}
}

func TestBlockedOrDeferredWorkListing(t *testing.T) {
	t.Parallel()
	svc := seededService(t)
	items, err := svc.GetBlockedOrDeferredWork(context.Background())
	if err != nil {
		t.Fatalf("GetBlockedOrDeferredWork error = %v", err)
	}
	if len(items) != 1 || items[0].WorkItemID != "work-1" {
		t.Fatalf("items = %#v", items)
	}
}

func TestSummaryIncludesQueuePressure(t *testing.T) {
	t.Parallel()
	svc := seededService(t)

	summary, err := svc.GetSummary(context.Background())
	if err != nil {
		t.Fatalf("GetSummary error = %v", err)
	}
	if len(summary.QueuePressure) != 1 {
		t.Fatalf("queue pressure = %#v", summary.QueuePressure)
	}
	if summary.QueuePressure[0].DepartmentID != "ops" || summary.QueuePressure[0].WorkItemsCount != 1 {
		t.Fatalf("queue pressure = %#v", summary.QueuePressure)
	}
}

func seededService(t *testing.T) Service {
	t.Helper()
	ctx := context.Background()
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	queueRepo := workplan.NewInMemoryQueueRepository()
	coordRepo := workplan.NewInMemoryCoordinationRepository()
	policyRepo := policy.NewInMemoryRepository()
	proposalRepo := proposal.NewInMemoryRepository()
	directory := employee.NewInMemoryDirectory()
	trustRepo := trust.NewInMemoryRepository()
	profileRepo := profile.NewInMemoryRepository()
	capRepo := capability.NewInMemoryRepository()
	execRepo := executionruntime.NewInMemoryExecutionRepository()
	wal := executionruntime.NewInMemoryWAL()

	base := time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC)
	must(t, caseRepo.Save(ctx, caseruntime.Case{ID: "case-1", Kind: "workflow.action", Status: "open", CorrelationID: "corr-1", SubjectRef: "subject-1", OpenedAt: base, UpdatedAt: base.Add(10 * time.Minute)}))
	must(t, queueRepo.SaveQueue(ctx, workplan.WorkQueue{ID: "queue-1", Name: "Ops", Department: "ops"}))
	must(t, queueRepo.SaveWorkItem(ctx, workplan.WorkItem{ID: "work-1", CaseID: "case-1", QueueID: "queue-1", Type: "workflow.action", Status: "open", Priority: "high", AssignedEmployeeID: "actor-1", PlanID: "plan-1", CreatedAt: base.Add(1 * time.Minute), UpdatedAt: base.Add(12 * time.Minute)}))
	must(t, coordRepo.SaveDecision(ctx, workplan.CoordinationDecision{ID: "coord-1", WorkItemID: "work-1", CaseID: "case-1", QueueID: "queue-1", DecisionType: workplan.CoordinationExecuteNow, Priority: 3, Reason: "initial", CreatedAt: base.Add(2 * time.Minute)}))
	must(t, coordRepo.SaveDecision(ctx, workplan.CoordinationDecision{ID: "coord-2", WorkItemID: "work-1", CaseID: "case-1", QueueID: "queue-1", DecisionType: workplan.CoordinationDefer, Priority: 2, Reason: "waiting for approval", CreatedAt: base.Add(3 * time.Minute)}))
	must(t, policyRepo.SaveDecision(ctx, policy.PolicyDecision{ID: "policy-1", CoordinationDecisionID: "coord-2", CaseID: "case-1", WorkItemID: "work-1", QueueID: "queue-1", Outcome: policy.PolicyRequireApproval, Reason: "high risk", CreatedAt: base.Add(4 * time.Minute)}))
	must(t, policyRepo.SaveApprovalRequest(ctx, policy.ApprovalRequest{ID: "approval-1", CoordinationDecisionID: "coord-2", PolicyDecisionID: "policy-1", CaseID: "case-1", WorkItemID: "work-1", QueueID: "queue-1", Status: policy.ApprovalPending, RequestedFromRole: "manager", CreatedAt: base.Add(5 * time.Minute)}))
	must(t, proposalRepo.Save(ctx, proposal.Proposal{ID: "proposal-1", Type: proposal.ProposalTypeActionIntent, Status: proposal.ProposalValidated, ActorID: "actor-1", CaseID: "case-1", WorkItemID: "work-1", Justification: "first", CreatedAt: base.Add(6 * time.Minute), UpdatedAt: base.Add(6 * time.Minute)}))
	must(t, proposalRepo.Save(ctx, proposal.Proposal{ID: "proposal-2", Type: proposal.ProposalTypeActionIntent, Status: proposal.ProposalCompiled, ActorID: "actor-1", CaseID: "case-1", WorkItemID: "work-1", Justification: "latest", ActionPlanID: "plan-compiled", CreatedAt: base.Add(7 * time.Minute), UpdatedAt: base.Add(8 * time.Minute)}))
	must(t, directory.SaveEmployee(ctx, employee.DigitalEmployee{ID: "actor-1", Role: "operator", Enabled: true, QueueMemberships: []string{"queue-1"}}))
	must(t, trustRepo.Save(ctx, trust.TrustProfile{ActorID: "actor-1", TrustLevel: trust.TrustHigh, AutonomyTier: trust.AutonomyStandard, UpdatedAt: base.Add(9 * time.Minute)}))
	must(t, profileRepo.SaveProfile(ctx, profile.CompetencyProfile{ID: "profile-1", ActorID: "actor-1", Name: "Primary Operator", ExecutionStyle: profile.ExecutionStyleBalanced, MaxComplexity: 5, PreferredWorkKinds: []string{"workflow.action"}}))
	must(t, capRepo.SaveCapability(ctx, capability.Capability{ID: "cap-1", Code: "workflow.execute", Level: 2}))
	must(t, capRepo.AssignCapability(ctx, capability.ActorCapability{ActorID: "actor-1", CapabilityID: "cap-1", Level: 2}))
	must(t, execRepo.SaveSession(ctx, executionruntime.ExecutionSession{ID: "exec-1", WorkItemID: "work-1", Status: executionruntime.ExecutionSessionSucceeded, CurrentStepIndex: 1, CreatedAt: base.Add(9 * time.Minute), UpdatedAt: base.Add(10 * time.Minute)}))
	must(t, execRepo.SaveSession(ctx, executionruntime.ExecutionSession{ID: "exec-2", WorkItemID: "work-1", Status: executionruntime.ExecutionSessionFailed, CurrentStepIndex: 2, FailureReason: "operator waiting", CreatedAt: base.Add(11 * time.Minute), UpdatedAt: base.Add(12 * time.Minute)}))
	must(t, execRepo.SaveStep(ctx, executionruntime.StepExecution{ID: "step-1", ExecutionSessionID: "exec-2", ActionID: "action-1", StepIndex: 0, Status: executionruntime.StepSucceeded}))
	must(t, execRepo.SaveStep(ctx, executionruntime.StepExecution{ID: "step-2", ExecutionSessionID: "exec-2", ActionID: "action-2", StepIndex: 1, Status: executionruntime.StepFailed, FailureReason: "operator waiting"}))
	must(t, wal.Append(ctx, executionruntime.WALRecord{ID: "wal-1", ExecutionSessionID: "exec-2", ActionID: "action-1", Type: executionruntime.WALStepIntent, CreatedAt: base.Add(11 * time.Minute)}))
	must(t, wal.Append(ctx, executionruntime.WALRecord{ID: "wal-2", ExecutionSessionID: "exec-2", ActionID: "action-2", Type: executionruntime.WALStepResult, CreatedAt: base.Add(12 * time.Minute)}))

	return NewService(caseRepo, queueRepo, coordRepo, policyRepo, proposalRepo, directory, trustRepo, profileRepo, capRepo, execRepo, wal, eventcore.NewInMemoryEventLog())
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
