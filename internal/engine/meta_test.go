package engine

import (
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// LLD-05 §3/§4 test 1 (second half): TestProductManifestParses' /api/meta
// availability — brand defaults declared in a pack's product{} block surface
// through MetaFor (the Go function backing GET /api/meta) for any actor.
func TestProductManifestExposedInMeta(t *testing.T) {
	src := `pack knowvault
version 1

product:
    name "KnowVault"
    logo "https://cdn.example.com/logo.svg"
    color accent "#4da3ff"
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
	model, errs := dsl.Compile(map[string]string{"p.dsl": src})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	m := e.MetaFor("mike", "Owner")
	if m.Product == nil {
		t.Fatal("meta.product must be present when the pack declares product{}")
	}
	if m.Product.Name != "KnowVault" {
		t.Errorf("product.name = %q, want KnowVault", m.Product.Name)
	}
	if m.Product.Colors["accent"] != "#4da3ff" {
		t.Errorf("product.colors[accent] = %q, want #4da3ff", m.Product.Colors["accent"])
	}
	if m.Product.Home != "SearchFace" {
		t.Errorf("product.home = %q, want SearchFace", m.Product.Home)
	}
	if m.Product.Auth != "password" {
		t.Errorf("product.auth = %q, want password", m.Product.Auth)
	}
}

// TestProductManifestAbsent: a pack without product{} exposes no product
// block at all (nil, not a zero-valued struct) — additive, I-1.
func TestProductManifestAbsent(t *testing.T) {
	src := `
entity Doc:
    title: string required

roles:
    Owner

permissions:
    Owner:
        full [Doc]
`
	model, errs := dsl.Compile(map[string]string{"p.dsl": src})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	m := e.MetaFor("mike", "Owner")
	if m.Product != nil {
		t.Fatalf("meta.product must be nil without a product{} block, got %+v", m.Product)
	}
}

// TestProductManifestNameFallsBackToPack: product.name defaults to the pack's
// technical name when the product block omits name (still usable branding).
func TestProductManifestNameFallsBackToPack(t *testing.T) {
	src := `pack legalpack
version 1

product:
    auth invite

entity Doc:
    title: string required

roles:
    Owner

permissions:
    Owner:
        full [Doc]
`
	model, errs := dsl.Compile(map[string]string{"p.dsl": src})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	m := e.MetaFor("mike", "Owner")
	if m.Product == nil || m.Product.Name != "legalpack" {
		t.Fatalf("product.name must fall back to the pack name, got %+v", m.Product)
	}
}
