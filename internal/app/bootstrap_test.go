package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"kalita/internal/actionplan"
	"kalita/internal/caseruntime"
	"kalita/internal/eventcore"
	"kalita/internal/policy"
	"kalita/internal/trust"
	"kalita/internal/workplan"
)

func TestBootstrapProvidesEventCenterCaseRuntimeWorkplanPolicyExecutionControlAndEmployeeLayer(t *testing.T) {
	cfg := `{
  "port": "8080",
  "dslDir": "../../dsl",
  "enumsDir": "../../reference/enums",
  "dbUrl": "",
  "autoMigrate": false,
  "blobDriver": "local",
  "filesRoot": "../../uploads"
}`
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	result, err := Bootstrap(cfgPath)
	if err != nil {
		t.Fatalf("Bootstrap error = %v", err)
	}
	if result == nil {
		t.Fatal("Bootstrap result is nil")
	}
	if result.Storage == nil {
		t.Fatal("Storage is nil")
	}
	if result.EventLog == nil {
		t.Fatal("EventLog is nil")
	}
	if result.CommandBus == nil {
		t.Fatal("CommandBus is nil")
	}
	if result.CaseRepo == nil {
		t.Fatal("CaseRepo is nil")
	}
	if result.CaseResolver == nil {
		t.Fatal("CaseResolver is nil")
	}
	if result.CaseService == nil {
		t.Fatal("CaseService is nil")
	}
	if result.QueueRepo == nil {
		t.Fatal("QueueRepo is nil")
	}
	if result.PlanRepo == nil {
		t.Fatal("PlanRepo is nil")
	}
	if result.CoordinationRepo == nil {
		t.Fatal("CoordinationRepo is nil")
	}
	if result.AssignmentRouter == nil {
		t.Fatal("AssignmentRouter is nil")
	}
	if result.Planner == nil {
		t.Fatal("Planner is nil")
	}
	if result.Coordinator == nil {
		t.Fatal("Coordinator is nil")
	}
	if result.WorkService == nil {
		t.Fatal("WorkService is nil")
	}
	if result.PolicyRepo == nil {
		t.Fatal("PolicyRepo is nil")
	}
	if result.PolicyEvaluator == nil {
		t.Fatal("PolicyEvaluator is nil")
	}
	if result.PolicyService == nil {
		t.Fatal("PolicyService is nil")
	}
	if result.ConstraintsRepo == nil {
		t.Fatal("ConstraintsRepo is nil")
	}
	if result.ConstraintsPlanner == nil {
		t.Fatal("ConstraintsPlanner is nil")
	}
	if result.ConstraintsService == nil {
		t.Fatal("ConstraintsService is nil")
	}
	if result.EmployeeDirectory == nil {
		t.Fatal("EmployeeDirectory is nil")
	}
	if result.AssignmentRepo == nil {
		t.Fatal("AssignmentRepo is nil")
	}
	if result.EmployeeSelector == nil {
		t.Fatal("EmployeeSelector is nil")
	}
	if result.EmployeeService == nil {
		t.Fatal("EmployeeService is nil")
	}
	if result.TrustRepo == nil {
		t.Fatal("TrustRepo is nil")
	}
	if result.TrustScorer == nil {
		t.Fatal("TrustScorer is nil")
	}
	if result.TrustService == nil {
		t.Fatal("TrustService is nil")
	}
	if result.ExecutionRepo == nil {
		t.Fatal("ExecutionRepo is nil")
	}
	if result.ExecutionWAL == nil {
		t.Fatal("ExecutionWAL is nil")
	}
	if result.ActionExecutor == nil {
		t.Fatal("ActionExecutor is nil")
	}
	if result.ProposalRepo == nil {
		t.Fatal("ProposalRepo is nil")
	}
	if result.ProposalValidator == nil {
		t.Fatal("ProposalValidator is nil")
	}
	if result.ProposalCompiler == nil {
		t.Fatal("ProposalCompiler is nil")
	}
	if result.ProposalService == nil {
		t.Fatal("ProposalService is nil")
	}
	if result.ExecutionRunner == nil {
		t.Fatal("ExecutionRunner is nil")
	}
	if result.ExecutionRuntime == nil {
		t.Fatal("ExecutionRuntime is nil")
	}
	queues, err := result.QueueRepo.ListQueues(context.Background())
	if err != nil {
		t.Fatalf("ListQueues error = %v", err)
	}
	if len(queues) == 0 || queues[0].ID != "default-intake" {
		t.Fatalf("queues = %#v", queues)
	}
	employees, err := result.EmployeeDirectory.ListEmployees(context.Background())
	if err != nil {
		t.Fatalf("ListEmployees error = %v", err)
	}
	if len(employees) == 0 || employees[0].Role != "legacy_operator" {
		t.Fatalf("employees = %#v", employees)
	}
	selected, reason, err := result.EmployeeSelector.SelectForWorkItem(context.Background(), workplan.WorkItem{ID: "work-1", QueueID: "default-intake"}, actionplan.ActionPlan{ID: "plan-1", Actions: []actionplan.Action{{ID: "action-1", Type: "legacy_workflow_action"}}})
	if err != nil {
		t.Fatalf("SelectForWorkItem error = %v", err)
	}
	if selected.ID != employees[0].ID || reason == "" {
		t.Fatalf("selected = %#v reason=%q", selected, reason)
	}
}

func TestBootstrapExposesTrustService(t *testing.T) {
	cfg := `{
  "port": "8080",
  "dslDir": "../../dsl",
  "enumsDir": "../../reference/enums",
  "dbUrl": "",
  "autoMigrate": false,
  "blobDriver": "local",
  "filesRoot": "../../uploads"
}`
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	result, err := Bootstrap(cfgPath)
	if err != nil {
		t.Fatalf("Bootstrap error = %v", err)
	}
	profile, err := result.TrustService.RecordOutcome(context.Background(), trust.ExecutionOutcome{ActorID: "employee-legacy-operator", ExecutionID: "exec-1", Succeeded: true})
	if err != nil {
		t.Fatalf("RecordOutcome error = %v", err)
	}
	if profile.ActorID != "employee-legacy-operator" || profile.CompletedExecutions != 1 {
		t.Fatalf("profile = %#v", profile)
	}
	got, ok, err := result.TrustService.GetTrustProfile(context.Background(), "employee-legacy-operator")
	if err != nil {
		t.Fatalf("GetTrustProfile error = %v", err)
	}
	if !ok || got != profile {
		t.Fatalf("GetTrustProfile = %#v, %v", got, ok)
	}
}

func TestBootstrapRestoresPersistentStateAcrossRestart(t *testing.T) {
	t.Parallel()
	persistDir := filepath.Join(t.TempDir(), "persist")
	cfg := `{
  "port": "8080",
  "dslDir": "../../dsl",
  "enumsDir": "../../reference/enums",
  "dbUrl": "",
  "autoMigrate": false,
  "persistenceEnabled": true,
  "persistenceDir": "` + persistDir + `",
  "snapshotEvery": 1,
  "blobDriver": "local",
  "filesRoot": "../../uploads"
}`
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	first, err := Bootstrap(cfgPath)
	if err != nil {
		t.Fatalf("Bootstrap(first) error = %v", err)
	}
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	caseRecord := caseruntime.Case{ID: "case-persist-1", Kind: "missed_container_pickup_review", Status: string(caseruntime.CaseOpen), Title: "Persistent case", SubjectRef: "route:R-42/container:SITE-42", CorrelationID: "corr-persist-1", OpenedAt: now, UpdatedAt: now, OwnerQueueID: "default-intake"}
	if err := first.CaseRepo.Save(context.Background(), caseRecord); err != nil {
		t.Fatalf("CaseRepo.Save error = %v", err)
	}
	workItem := workplan.WorkItem{ID: "work-persist-1", CaseID: caseRecord.ID, QueueID: "default-intake", Type: "missed_container_pickup_review", Status: string(workplan.WorkItemOpen), Priority: "high", Reason: "persist me", CreatedAt: now, UpdatedAt: now}
	if err := first.QueueRepo.SaveWorkItem(context.Background(), workItem); err != nil {
		t.Fatalf("SaveWorkItem error = %v", err)
	}
	decision := workplan.CoordinationDecision{ID: "coord-persist-1", WorkItemID: workItem.ID, CaseID: caseRecord.ID, QueueID: "default-intake", DecisionType: workplan.CoordinationDefer, Reason: "need approval", Priority: workplan.CoordinationPriorityDefer, CreatedAt: now}
	if err := first.CoordinationRepo.SaveDecision(context.Background(), decision); err != nil {
		t.Fatalf("SaveDecision error = %v", err)
	}
	policyDecision := policy.PolicyDecision{ID: "policy-persist-1", CoordinationDecisionID: decision.ID, CaseID: caseRecord.ID, WorkItemID: workItem.ID, QueueID: "default-intake", Outcome: policy.PolicyRequireApproval, Reason: "trust low", CreatedAt: now.Add(time.Minute)}
	if err := first.PolicyRepo.SaveDecision(context.Background(), policyDecision); err != nil {
		t.Fatalf("SaveDecision(policy) error = %v", err)
	}
	approval := policy.ApprovalRequest{ID: "approval-persist-1", CoordinationDecisionID: decision.ID, PolicyDecisionID: policyDecision.ID, CaseID: caseRecord.ID, WorkItemID: workItem.ID, QueueID: "default-intake", Status: policy.ApprovalPending, RequestedFromRole: "supervisor", CreatedAt: now.Add(2 * time.Minute)}
	if err := first.PolicyRepo.SaveApprovalRequest(context.Background(), approval); err != nil {
		t.Fatalf("SaveApprovalRequest error = %v", err)
	}
	trustProfile := trust.TrustProfile{ActorID: "employee-legacy-operator", Metrics: trust.TrustMetrics{SuccessCount: 2, FailureCount: 1}, CompletedExecutions: 3, FailedExecutions: 1, TrustLevel: trust.TrustLow, AutonomyTier: trust.AutonomyRestricted, UpdatedAt: now.Add(3 * time.Minute)}
	if err := first.TrustRepo.Save(context.Background(), trustProfile); err != nil {
		t.Fatalf("TrustRepo.Save error = %v", err)
	}
	for _, execEvent := range []eventcore.ExecutionEvent{
		{ID: "exec-persist-1", ExecutionID: "approval:approval-persist-1", CaseID: caseRecord.ID, Step: "approval_request_created", Status: "pending", OccurredAt: now.Add(2 * time.Minute), CorrelationID: caseRecord.CorrelationID, CausationID: approval.ID, Payload: map[string]any{"approval_request_id": approval.ID}},
		{ID: "exec-persist-2", ExecutionID: "approval:approval-persist-1", CaseID: caseRecord.ID, Step: "coordination_decision_made", Status: string(workplan.CoordinationDefer), OccurredAt: now.Add(4 * time.Minute), CorrelationID: caseRecord.CorrelationID, CausationID: decision.ID, Payload: map[string]any{"coordination_decision_id": decision.ID}},
		{ID: "exec-persist-3", ExecutionID: "exec-persist-actor", CaseID: caseRecord.ID, Step: "trust_updated", Status: string(trust.TrustLow), OccurredAt: now.Add(5 * time.Minute), CorrelationID: caseRecord.CorrelationID, CausationID: "exec-persist-actor", Payload: map[string]any{"actor_id": trustProfile.ActorID, "trust_level": trustProfile.TrustLevel}},
	} {
		if err := first.EventLog.AppendExecutionEvent(context.Background(), execEvent); err != nil {
			t.Fatalf("AppendExecutionEvent(%s) error = %v", execEvent.ID, err)
		}
	}

	second, err := Bootstrap(cfgPath)
	if err != nil {
		t.Fatalf("Bootstrap(second) error = %v", err)
	}

	caseOverview, err := second.ControlPlane.GetCaseOverview(context.Background(), caseRecord.ID)
	if err != nil {
		t.Fatalf("GetCaseOverview error = %v", err)
	}
	if caseOverview.CorrelationID != caseRecord.CorrelationID || caseOverview.SubjectRef != caseRecord.SubjectRef {
		t.Fatalf("caseOverview = %#v", caseOverview)
	}
	workOverview, err := second.ControlPlane.GetWorkItemOverview(context.Background(), workItem.ID)
	if err != nil {
		t.Fatalf("GetWorkItemOverview error = %v", err)
	}
	if workOverview.Coordination.DecisionType != string(workplan.CoordinationDefer) || workOverview.PolicyApproval.Outcome != string(policy.PolicyRequireApproval) || workOverview.PolicyApproval.ApprovalRequestID != approval.ID {
		t.Fatalf("workOverview = %#v", workOverview)
	}
	gotTrust, ok, err := second.TrustService.GetTrustProfile(context.Background(), trustProfile.ActorID)
	if err != nil {
		t.Fatalf("GetTrustProfile error = %v", err)
	}
	if !ok || !reflect.DeepEqual(gotTrust, trustProfile) {
		t.Fatalf("trust profile = %#v, ok=%v want %#v", gotTrust, ok, trustProfile)
	}
	timeline, err := second.ControlPlane.GetCaseTimeline(context.Background(), caseRecord.ID)
	if err != nil {
		t.Fatalf("GetCaseTimeline error = %v", err)
	}
	steps := make([]string, 0, len(timeline))
	for _, entry := range timeline {
		steps = append(steps, entry.Step)
	}
	wantSteps := []string{"approval_requested", "coordination_decided", "trust_updated"}
	if !reflect.DeepEqual(steps, wantSteps) {
		t.Fatalf("timeline steps = %#v, want %#v", steps, wantSteps)
	}
}
