package dsl

import (
	"os"
	"path/filepath"
	"testing"
)

func compilePackDir(t *testing.T, dir string) (*Model, []*Error) {
	t.Helper()
	files := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".kal" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = string(raw)
	}
	return Compile(files)
}

// BACKLOG-MVP week 2 DoD: entities and permissions of examples/collections compile.
func TestCollectionsPackCompiles(t *testing.T) {
	m, errs := compilePackDir(t, "../../examples/collections")
	for _, e := range errs {
		t.Errorf("unexpected: %v", e)
	}
	if t.Failed() {
		t.FailNow()
	}

	if m.Manifest == nil || m.Manifest.Name != "collections" {
		t.Fatal("manifest must parse: pack collections")
	}
	debtor, ok := m.Entities["Debtor"]
	if !ok {
		t.Fatal("entity Debtor must exist")
	}
	if len(debtor.Fields) != 6 {
		t.Fatalf("Debtor must have 6 fields, got %d", len(debtor.Fields))
	}

	byName := map[string]*FieldDecl{}
	for _, f := range debtor.Fields {
		byName[f.Name] = f
	}
	if f := byName["contract"]; f.Type.Kind != TyRef || f.Type.RefTarget != "Contract" || f.OnDelete != "restrict" {
		t.Fatalf("contract field parsed wrong: %+v", f)
	}
	if f := byName["status"]; f.Type.Kind != TyEnum || len(f.Type.EnumValues) != 5 || f.Default != "OnTime" {
		t.Fatalf("status field parsed wrong: %+v", f)
	}
	if f := byName["overdue_days"]; f.Computed == "" {
		t.Fatalf("overdue_days must be computed: %+v", f)
	}
	if f := byName["manager"]; f.Type.RefTarget != "core.User" || f.Default != "$me" {
		t.Fatalf("manager field parsed wrong: %+v", f)
	}

	if len(debtor.Constraints) != 1 || len(debtor.Constraints[0].Fields) != 2 {
		t.Fatalf("Debtor must carry unique(company, contract): %+v", debtor.Constraints)
	}

	collector, ok := m.Roles["Collector"]
	if !ok || !collector.IsAgent {
		t.Fatal("Collector must be an agent role")
	}
	perms := m.Perms["Collector"]
	if perms == nil || len(perms.Rules) != 3 {
		t.Fatalf("Collector must have 3 permission rules, got %+v", perms)
	}
	var deny *PermRule
	for i := range perms.Rules {
		if perms.Rules[i].Verb == "deny" {
			deny = &perms.Rules[i]
		}
	}
	if deny == nil || len(deny.Items) != 3 {
		t.Fatalf("Collector deny must have 3 items: %+v", deny)
	}
	if deny.Items[0].Verb != "update" || deny.Items[0].Entity != "Debtor" || deny.Items[0].Field != "debt" {
		t.Fatalf("deny[0] must be update Debtor.debt: %+v", deny.Items[0])
	}
	if deny.Items[1].Verb != "delete" || !deny.Items[1].All {
		t.Fatalf("deny[1] must be delete *: %+v", deny.Items[1])
	}
	if deny.Items[2].Verb != "read" || deny.Items[2].Entity != "Contract" || deny.Items[2].Where == "" {
		t.Fatalf("deny[2] must be read Contract where ...: %+v", deny.Items[2])
	}

	// workflow / automation / ui survive as raw blocks for week 4
	kinds := map[string]bool{}
	for _, rb := range m.Raw {
		kinds[rb.Kind] = true
	}
	if !kinds["workflow"] || !kinds["automation"] || !kinds["ui"] {
		t.Fatalf("raw blocks must be preserved, got %v", kinds)
	}
}

// The second acceptance pack must compile too (gate of week 4 starts passing
// its entity/permission half now).
func TestDevDepartmentPackCompiles(t *testing.T) {
	m, errs := compilePackDir(t, "../../examples/dev_department")
	for _, e := range errs {
		t.Errorf("unexpected: %v", e)
	}
	if t.Failed() {
		t.FailNow()
	}
	for _, want := range []string{"ADR", "Task", "Defect"} {
		if _, ok := m.Entities[want]; !ok {
			t.Fatalf("entity %s must exist", want)
		}
	}
	agentRoles := 0
	for _, r := range m.Roles {
		if r.IsAgent {
			agentRoles++
		}
	}
	if agentRoles != 5 {
		t.Fatalf("dev_department declares 5 agent roles, got %d", agentRoles)
	}
}
