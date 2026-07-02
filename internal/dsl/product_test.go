package dsl

import "testing"

// LLD-05 §3/§4 test 1: TestProductManifestParses — the product{} section
// compiles; brand defaults are readable off the model (engine/meta_test.go
// covers the second half of the acceptance criterion: availability through
// /api/meta).

func TestProductManifestParses(t *testing.T) {
	src := `pack knowvault
version 1

product:
    name "KnowVault"
    logo "https://cdn.example.com/logo.svg"
    color accent "#4da3ff"
    color bg "#0f1419"
    home SearchFace
    auth password

entity Doc:
    title: string required

roles:
    Owner

permissions:
    Owner:
        full [Doc]
`
	m, errs := Compile(map[string]string{"p.dsl": src})
	for _, e := range errs {
		t.Errorf("unexpected: %v", e)
	}
	if t.Failed() {
		t.FailNow()
	}
	if m.Product == nil {
		t.Fatal("product block must parse")
	}
	if m.Product.Name != "KnowVault" {
		t.Errorf("name = %q, want KnowVault", m.Product.Name)
	}
	if m.Product.Logo != "https://cdn.example.com/logo.svg" {
		t.Errorf("logo = %q", m.Product.Logo)
	}
	if m.Product.Colors["accent"] != "#4da3ff" || m.Product.Colors["bg"] != "#0f1419" {
		t.Errorf("colors = %+v", m.Product.Colors)
	}
	if m.Product.Home != "SearchFace" {
		t.Errorf("home = %q, want SearchFace", m.Product.Home)
	}
	if m.Product.Auth != "password" {
		t.Errorf("auth = %q, want password", m.Product.Auth)
	}
}

// TestProductBlockOptional: a pack without product{} compiles exactly as
// before (I-1 — additive grammar, zero impact on packs that don't use it).
func TestProductBlockOptional(t *testing.T) {
	src := `pack plain
version 1

entity Doc:
    title: string required

roles:
    Owner

permissions:
    Owner:
        full [Doc]
`
	m, errs := Compile(map[string]string{"p.dsl": src})
	for _, e := range errs {
		t.Errorf("unexpected: %v", e)
	}
	if m.Product != nil {
		t.Fatal("product must be nil when the pack declares no product block")
	}
}

// TestProductBadAuth: an unrecognized auth mode is a compile error with a
// fix hint, not a silent acceptance (I-3 — errors are events, not silence).
func TestProductBadAuth(t *testing.T) {
	src := `pack p
version 1

product:
    name "P"
    auth oauth

entity Doc:
    title: string required

roles:
    Owner

permissions:
    Owner:
        full [Doc]
`
	_, errs := Compile(map[string]string{"p.dsl": src})
	found := false
	for _, e := range errs {
		if e.Code == EBadProduct {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected E050_BAD_PRODUCT for auth oauth, got %v", errs)
	}
}

// TestProductDeclaredTwice: a second product{} block is rejected, not merged
// silently (one manifest, one meaning).
func TestProductDeclaredTwice(t *testing.T) {
	src := `pack p
version 1

product:
    name "One"

product:
    name "Two"

entity Doc:
    title: string required

roles:
    Owner

permissions:
    Owner:
        full [Doc]
`
	_, errs := Compile(map[string]string{"p.dsl": src})
	found := false
	for _, e := range errs {
		if e.Code == EBadProduct {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected E050_BAD_PRODUCT for a duplicate product block, got %v", errs)
	}
}
