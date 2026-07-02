package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
)

// TestLoadPacksSingleDirIsLoadPack: the multi-pack loader's single-directory
// fast path must be identical to the pre-multi-pack loadPack (I-1 regression
// guard for the CLI layer — the golden-replay gate under tests/ covers the
// dsl/engine layer for the same invariant).
func TestLoadPacksSingleDirIsLoadPack(t *testing.T) {
	repoRoot := repoRootForTest(t)
	dir := filepath.Join(repoRoot, "packs", "contracts")

	want, wantErrs := loadPack(dir)
	if len(wantErrs) > 0 {
		t.Fatalf("packs/contracts must compile clean: %v", wantErrs)
	}
	got, gotErrs := loadPacks([]string{dir})
	if len(gotErrs) > 0 {
		t.Fatalf("loadPacks([single dir]) must compile clean: %v", gotErrs)
	}
	if got.Manifest.Name != want.Manifest.Name || len(got.Entities) != len(want.Entities) || len(got.Roles) != len(want.Roles) {
		t.Fatalf("loadPacks([single dir]) diverges from loadPack: got pack=%s entities=%d roles=%d, want pack=%s entities=%d roles=%d",
			got.Manifest.Name, len(got.Entities), len(got.Roles),
			want.Manifest.Name, len(want.Entities), len(want.Roles))
	}
}

// TestLoadPacksContractsAndAssistantCoexist is the task's proof obligation:
// packs/contracts + packs/assistant load together on one node, both models'
// entities are present, and the merged model boots a real engine (not just
// a clean dsl.Compile — the thing serve() actually runs).
func TestLoadPacksContractsAndAssistantCoexist(t *testing.T) {
	repoRoot := repoRootForTest(t)
	dirs := []string{
		filepath.Join(repoRoot, "packs", "contracts"),
		filepath.Join(repoRoot, "packs", "assistant"),
	}
	for _, d := range dirs {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			t.Skipf("%s not found (repo layout changed?): %v", d, err)
		}
	}

	model, errs := loadPacks(dirs)
	if len(errs) > 0 {
		t.Fatalf("packs/contracts + packs/assistant must merge clean: %v", errs)
	}

	// contracts' entities
	for _, name := range []string{"Contract", "Counterparty", "Playbook", "Finding"} {
		if _, ok := model.Entities[name]; !ok {
			t.Errorf("merged model missing contracts entity %s", name)
		}
	}
	// assistant's entities
	for _, name := range []string{"Session", "Message"} {
		if _, ok := model.Entities[name]; !ok {
			t.Errorf("merged model missing assistant entity %s", name)
		}
	}
	// roles from both packs
	for _, name := range []string{"Lawyer", "Legal", "ContractManager", "CommercialDirector", "User", "Admin"} {
		if _, ok := model.Roles[name]; !ok {
			t.Errorf("merged model missing role %s", name)
		}
	}

	// the merged model must actually boot the engine serve() runs — proving
	// coexistence at the dsl.Model level is necessary but not sufficient.
	ctx := context.Background()
	store := eventstore.NewMemStore(nil)
	if _, err := engine.New(ctx, model, store); err != nil {
		t.Fatalf("engine.New on the merged model: %v", err)
	}
}

// TestLoadPacksEntityCollisionIsLoud: two packs declaring the same entity
// name must fail loudly, naming both packs — never a silent last-wins
// overwrite (the task's explicit collision contract).
func TestLoadPacksEntityCollisionIsLoud(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "alpha")
	b := filepath.Join(root, "beta")
	mustWritePack(t, a, "alpha", "entity Contract:\n    title: string required\n")
	mustWritePack(t, b, "beta", "entity Contract:\n    number: string required\n")

	_, errs := loadPacks([]string{a, b})
	if len(errs) == 0 {
		t.Fatal("expected a collision error, got a clean merge")
	}
	found := false
	for _, e := range errs {
		if e.Code == dsl.EDupEntity && strings.Contains(e.Message, "alpha") && strings.Contains(e.Message, "beta") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an E009_DUPLICATE_ENTITY error naming both packs, got: %v", errs)
	}
}

// TestLoadPacksRoleCollisionIsLoud mirrors the entity case for roles.
func TestLoadPacksRoleCollisionIsLoud(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "alpha")
	b := filepath.Join(root, "beta")
	mustWritePack(t, a, "alpha", "entity Widget:\n    name: string required\n\nroles:\n    Owner\n\npermissions:\n    Owner:\n        full [Widget]\n")
	mustWritePack(t, b, "beta", "entity Gadget:\n    name: string required\n\nroles:\n    Owner\n\npermissions:\n    Owner:\n        full [Gadget]\n")

	_, errs := loadPacks([]string{a, b})
	if len(errs) == 0 {
		t.Fatal("expected a role collision error, got a clean merge")
	}
	found := false
	for _, e := range errs {
		if e.Code == dsl.EDupRole && strings.Contains(e.Message, "alpha") && strings.Contains(e.Message, "beta") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an E014_DUPLICATE_ROLE error naming both packs, got: %v", errs)
	}
}

func mustWritePack(t *testing.T, dir, packName, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "pack " + packName + "\nversion 0.1.0\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, "pack.dsl"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}
