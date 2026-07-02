package dsl

import (
	"strings"
	"testing"
)

func compileOnePack(t *testing.T, src string) *Model {
	t.Helper()
	m, errs := Compile(map[string]string{"pack.dsl": src})
	if len(errs) > 0 {
		t.Fatalf("pack must compile clean: %v", errs)
	}
	return m
}

// TestMergeModelsCoexist proves the core multi-pack contract: two packs with
// disjoint entities/roles compile together into one model that carries both.
func TestMergeModelsCoexist(t *testing.T) {
	a := compileOnePack(t, `pack alpha
version 0.1.0

entity Widget:
    name: string required

roles:
    Owner

permissions:
    Owner:
        full [Widget]
`)
	b := compileOnePack(t, `pack beta
version 0.1.0

entity Gadget:
    name: string required

roles:
    Operator

permissions:
    Operator:
        full [Gadget]
`)

	merged, errs := MergeModels([]*Model{a, b})
	if len(errs) != 0 {
		t.Fatalf("expected a clean merge, got errors: %v", errs)
	}
	if _, ok := merged.Entities["Widget"]; !ok {
		t.Error("merged model must carry pack alpha's Widget entity")
	}
	if _, ok := merged.Entities["Gadget"]; !ok {
		t.Error("merged model must carry pack beta's Gadget entity")
	}
	if _, ok := merged.Roles["Owner"]; !ok {
		t.Error("merged model must carry pack alpha's Owner role")
	}
	if _, ok := merged.Roles["Operator"]; !ok {
		t.Error("merged model must carry pack beta's Operator role")
	}
	if _, ok := merged.Perms["Owner"]; !ok {
		t.Error("merged model must carry pack alpha's Owner permissions")
	}
	if _, ok := merged.Perms["Operator"]; !ok {
		t.Error("merged model must carry pack beta's Operator permissions")
	}
}

// TestMergeModelsEntityCollision: two packs declaring the same entity name
// is a loud compile error naming BOTH packs, never a silent last-wins merge.
func TestMergeModelsEntityCollision(t *testing.T) {
	a := compileOnePack(t, `pack alpha
version 0.1.0

entity Contract:
    title: string required
`)
	b := compileOnePack(t, `pack beta
version 0.1.0

entity Contract:
    number: string required
`)

	_, errs := MergeModels([]*Model{a, b})
	if len(errs) == 0 {
		t.Fatal("expected a collision error, got a clean merge")
	}
	found := false
	for _, e := range errs {
		if e.Code == EDupEntity {
			found = true
			if !strings.Contains(e.Message, "alpha") || !strings.Contains(e.Message, "beta") {
				t.Errorf("collision message must name both packs, got: %s", e.Message)
			}
			if e.FixHint == "" {
				t.Error("collision error must carry a fix hint")
			}
		}
	}
	if !found {
		t.Fatalf("expected EDupEntity among errors, got: %v", errs)
	}
}

// TestMergeModelsRoleCollision mirrors the entity case for roles — the other
// symbol kind the task explicitly calls out as a naming collision.
func TestMergeModelsRoleCollision(t *testing.T) {
	a := compileOnePack(t, `pack alpha
version 0.1.0

entity Widget:
    name: string required

roles:
    Owner

permissions:
    Owner:
        full [Widget]
`)
	b := compileOnePack(t, `pack beta
version 0.1.0

entity Gadget:
    name: string required

roles:
    Owner

permissions:
    Owner:
        full [Gadget]
`)

	_, errs := MergeModels([]*Model{a, b})
	if len(errs) == 0 {
		t.Fatal("expected a role collision error, got a clean merge")
	}
	found := false
	for _, e := range errs {
		if e.Code == EDupRole {
			found = true
			if !strings.Contains(e.Message, "alpha") || !strings.Contains(e.Message, "beta") {
				t.Errorf("collision message must name both packs, got: %s", e.Message)
			}
		}
	}
	if !found {
		t.Fatalf("expected EDupRole among errors, got: %v", errs)
	}
}

// TestMergeModelsCoreEntitiesUnion proves that core.* kernel built-ins
// (injected independently into every pack's own solo compile) are unioned,
// not flagged as a fake authoring collision, even though both packs
// declare an mdg[Region] field that provisions the SAME core.Region
// dictionary entity independently.
func TestMergeModelsCoreEntitiesUnion(t *testing.T) {
	a := compileOnePack(t, `pack alpha
version 0.1.0

entity Widget:
    name:   string required
    region: ref[core.Region]
`)
	b := compileOnePack(t, `pack beta
version 0.1.0

entity Gadget:
    name:   string required
    region: ref[core.Region]
`)

	merged, errs := MergeModels([]*Model{a, b})
	if len(errs) != 0 {
		t.Fatalf("expected a clean merge (core.* entities union, not collide), got: %v", errs)
	}
	if _, ok := merged.Entities["core.Region"]; !ok {
		t.Error("merged model must carry the shared core.Region dictionary")
	}
	if _, ok := merged.Entities["core.Calendar"]; !ok {
		t.Error("merged model must carry the kernel-injected core.Calendar entity")
	}
}

// TestMergeModelsSinglePassthrough: MergeModels with exactly one model is
// the identity function — no synthetic manifest, no relabeling.
func TestMergeModelsSinglePassthrough(t *testing.T) {
	a := compileOnePack(t, `pack alpha
version 0.1.0

entity Widget:
    name: string required
`)
	merged, errs := MergeModels([]*Model{a})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if merged != a {
		t.Error("a single model must pass through unchanged")
	}
}
