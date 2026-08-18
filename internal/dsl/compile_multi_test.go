package dsl

import "testing"

// TestCompileMultiGroupsByHeader proves the product-mission compile shape: a
// flat sources map holding SEVERAL packs groups by each file's `pack X`
// header, compiles per group, and merges with the same collision loudness as
// the boot loader — while a single-group map is byte-identical to Compile.
func TestCompileMultiGroupsByHeader(t *testing.T) {
	files := map[string]string{
		"packs/one/entities.dsl": "pack one\nversion 0.1.0\n\nentity Alpha:\n    title: string\n",
		"packs/two/entities.dsl": "pack two\nversion 0.1.0\n\nentity Beta:\n    title: string\n",
	}
	m, errs := CompileMulti(files)
	if len(errs) > 0 {
		t.Fatalf("CompileMulti must merge two packs: %v", errs[0])
	}
	if m.Entities["Alpha"] == nil || m.Entities["Beta"] == nil {
		t.Fatalf("both packs' entities must exist: %+v", m.Entities)
	}
}

// TestCompileMultiSingleGroupIsCompile pins I-1: one group must behave exactly
// like the plain single-pack Compile (proposals and journals unchanged).
func TestCompileMultiSingleGroupIsCompile(t *testing.T) {
	files := map[string]string{"t.dsl": "pack t\nversion 0.1.0\n\nentity A:\n    x: int\n"}
	multi, errs := CompileMulti(files)
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	single, errs := Compile(files)
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	if multi.Manifest.Name != single.Manifest.Name ||
		len(multi.Entities) != len(single.Entities) {
		t.Fatalf("single-group CompileMulti must match Compile: %q vs %q",
			multi.Manifest.Name, single.Manifest.Name)
	}
}

// TestCompileMultiCollisionIsLoud: two packs declaring the same entity name
// must fail loudly (the same E009 the boot loader produces), never silently
// last-wins.
func TestCompileMultiCollisionIsLoud(t *testing.T) {
	files := map[string]string{
		"one.dsl": "pack one\nversion 0.1.0\n\nentity Dupe:\n    title: string\n",
		"two.dsl": "pack two\nversion 0.1.0\n\nentity Dupe:\n    title: string\n",
	}
	_, errs := CompileMulti(files)
	if len(errs) == 0 {
		t.Fatal("cross-pack entity collision must fail loudly")
	}
	if errs[0].Code != EDupEntity {
		t.Fatalf("want E009_DUPLICATE_ENTITY, got %s", errs[0].Code)
	}
}

// TestPackHeaderOf: the header is the file's own identity line, not the path.
func TestPackHeaderOf(t *testing.T) {
	if got := packHeaderOf("pack ap\nversion 0.1.0\n"); got != "ap" {
		t.Fatalf("packHeaderOf = %q, want ap", got)
	}
	if got := packHeaderOf("  pack my-pack  \nentity A:\n"); got != "my-pack" {
		t.Fatalf("packHeaderOf = %q, want my-pack", got)
	}
	if got := packHeaderOf("entity A:\n    x: int\n"); got != "" {
		t.Fatalf("packHeaderOf = %q, want empty", got)
	}
}
