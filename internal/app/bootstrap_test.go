package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"kalita/internal/actionplan"
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
