package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBootstrapProvidesEventCenterAndCaseRuntime(t *testing.T) {
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
}
