package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapProvidesEventCenterCaseRuntimeWorkplanAndPolicy(t *testing.T) {
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
	queues, err := result.QueueRepo.ListQueues(context.Background())
	if err != nil {
		t.Fatalf("ListQueues error = %v", err)
	}
	if len(queues) == 0 || queues[0].ID != "default-intake" {
		t.Fatalf("queues = %#v", queues)
	}
}
