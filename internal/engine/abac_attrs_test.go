package engine

import (
	"context"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Attribute-based row scoping (the "A" in ABAC): a reference directory is
// filtered by the actor's own attribute, declared in one concise line —
// `read Counterparty where region = $me.region`. One viewer sees every
// counterparty in their region, another sees a different region's; the kernel
// resolves $me.region and applies the filter. This is the founder's exact case
// (a counterparty directory where one user sees all of a region, another only
// theirs) and the "list only what I'm allowed" query that ABAC implementations
// stumble on.
func TestActorAttributeRowScope(t *testing.T) {
	src := `pack t
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1
entity Counterparty:
    name: string required
    region: string
roles:
    Admin
    Viewer
permissions:
    Admin:
        full [Counterparty]
    Viewer:
        read Counterparty where region = $me.region
`
	model, errs := dsl.Compile(map[string]string{"t.dsl": src})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	ctx := context.Background()
	eng, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	admin := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Admin"}
	basis := &eventstore.Basis{Type: "human", ID: "root"}
	for _, c := range []struct{ name, region string }{
		{"Volga Trade", "Volga"}, {"Volga Steel", "Volga"}, {"Ural Mining", "Ural"},
	} {
		if _, err := eng.Create(ctx, admin, "Counterparty",
			map[string]any{"name": c.name, "region": c.region}, basis, ""); err != nil {
			t.Fatalf("create %s: %v", c.name, err)
		}
	}

	// a Volga viewer sees only Volga counterparties
	volga := eventstore.Actor{Type: eventstore.ActorHuman, ID: "u1", Role: "Viewer",
		Attrs: map[string]any{"region": "Volga"}}
	rows, err := eng.Query(ctx, volga, "Counterparty", QueryOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Errorf("Volga viewer sees %d, want 2", len(rows))
	}
	for _, r := range rows {
		if r.Values["region"] != "Volga" {
			t.Errorf("leaked a %v counterparty to the Volga viewer", r.Values["region"])
		}
	}

	// a different attribute yields a different permitted set
	ural := eventstore.Actor{Type: eventstore.ActorHuman, ID: "u2", Role: "Viewer",
		Attrs: map[string]any{"region": "Ural"}}
	rows, _ = eng.Query(ctx, ural, "Counterparty", QueryOpts{})
	if len(rows) != 1 || rows[0].Values["region"] != "Ural" {
		t.Errorf("Ural viewer should see exactly 1 Ural counterparty, got %v", rows)
	}

	// a viewer with no region attribute sees nothing (fail-closed)
	none := eventstore.Actor{Type: eventstore.ActorHuman, ID: "u3", Role: "Viewer"}
	rows, _ = eng.Query(ctx, none, "Counterparty", QueryOpts{})
	if len(rows) != 0 {
		t.Errorf("a viewer with no region must see nothing, got %d", len(rows))
	}
}
