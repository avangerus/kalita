package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEntitiesParsesWorkflowMetadata(t *testing.T) {
	t.Parallel()

	dsl := `
module test

entity Ticket:
  title: string required
  status: enum[Draft, InApproval, Approved] required default=Draft
  workflow:
    status_field: status
    actions:
      submit:
        from: [Draft]
        to: InApproval
      approve:
        from: [InApproval]
        to: Approved
`
	path := writeDSLFile(t, dsl)

	entities, err := LoadEntities(path)
	if err != nil {
		t.Fatalf("LoadEntities() error = %v", err)
	}
	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}
	got := entities[0].Workflow
	if got == nil {
		t.Fatalf("expected workflow metadata to be parsed")
	}
	if got.StatusField != "status" {
		t.Fatalf("status_field = %q, want status", got.StatusField)
	}
	if got.Actions["submit"].To != "InApproval" {
		t.Fatalf("submit.to = %q", got.Actions["submit"].To)
	}
	if len(got.Actions["approve"].From) != 1 || got.Actions["approve"].From[0] != "InApproval" {
		t.Fatalf("approve.from = %#v", got.Actions["approve"].From)
	}
}

func TestLoadAllEntitiesExistingDSLStillValid(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	src, err := os.ReadFile(filepath.Join("..", "..", "dsl", "core", "entities.dsl"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "core"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "core", "entities.dsl"), src, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	entities, err := LoadAllEntities(root)
	if err != nil {
		t.Fatalf("LoadAllEntities() error = %v", err)
	}
	if _, ok := entities["core.Project"]; !ok {
		t.Fatalf("expected existing core.Project to still load")
	}
	if _, ok := entities["test.WorkflowTask"]; !ok {
		t.Fatalf("expected WorkflowTask example to load")
	}
}

func TestLoadEntitiesRejectsDuplicateWorkflowAction(t *testing.T) {
	t.Parallel()

	dsl := `
module test

entity Ticket:
  status: enum[Draft, InApproval] required
  workflow:
    status_field: status
    actions:
      submit:
        from: [Draft]
        to: InApproval
      submit:
        from: [InApproval]
        to: Draft
`
	_, err := LoadEntities(writeDSLFile(t, dsl))
	if err == nil {
		t.Fatalf("expected duplicate workflow action error")
	}
}

func TestLintWorkflowRejectsUnknownStatusFieldAndStates(t *testing.T) {
	t.Parallel()

	entity := &Entity{
		Name:   "Ticket",
		Module: "test",
		Fields: []Field{
			{Name: "status", Type: "enum", Enum: []string{"Draft", "Approved"}},
		},
		Workflow: &Workflow{
			StatusField: "lifecycle",
			Actions: map[string]WorkflowAction{
				"submit": {From: []string{"Draft"}, To: "Approved"},
			},
		},
	}
	issues := Lint(map[string]*Entity{"test.Ticket": entity})
	if len(issues) == 0 {
		t.Fatalf("expected lint issues for unknown status field")
	}

	entity.Workflow.StatusField = "status"
	entity.Workflow.Actions["submit"] = WorkflowAction{From: []string{"Unknown"}, To: "Missing"}
	issues = Lint(map[string]*Entity{"test.Ticket": entity})
	if len(issues) < 2 {
		t.Fatalf("expected issues for unknown workflow states, got %#v", issues)
	}
}

func writeDSLFile(t *testing.T, dsl string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "entities.dsl")
	if err := os.WriteFile(path, []byte(dsl), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
