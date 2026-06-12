package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// The permitted-set query must NOT scan every row. With 900 counterparties
// across 3 regions and `read where region = $me.region`, a regional viewer's
// query is served from one region's index bucket (~300 candidates), not a scan
// of all 900 — and still returns exactly their region. can() stays the
// authority, so the narrowing can never leak another region.
func TestPermittedSetUsesIndex(t *testing.T) {
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
	regions := []string{"Volga", "Ural", "Siberia"}
	for i := 0; i < 900; i++ {
		if _, err := eng.Create(ctx, admin, "Counterparty", map[string]any{
			"name": fmt.Sprintf("CP-%d", i), "region": regions[i%3]}, basis, ""); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	viewer := eventstore.Actor{Type: eventstore.ActorHuman, ID: "u1", Role: "Viewer",
		Attrs: map[string]any{"region": "Volga"}}

	// the index narrows to ~300 candidates, not 900
	cand, narrowed := eng.candidateIDs("Counterparty", viewer)
	if !narrowed {
		t.Fatal("region-scoped read should be index-narrowed, not a full scan")
	}
	if len(cand) != 300 {
		t.Errorf("candidate set = %d, want 300 (one region's bucket, not all 900)", len(cand))
	}

	// and the actual query returns exactly the Volga set
	rows, err := eng.Query(ctx, viewer, "Counterparty", QueryOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 300 {
		t.Errorf("query returned %d, want 300", len(rows))
	}
	for _, r := range rows {
		if r.Values["region"] != "Volga" {
			t.Fatalf("index narrowing leaked a %v row", r.Values["region"])
		}
	}

	// a full-access role is NOT narrowed (unconditional allow → scan all)
	if _, narrowed := eng.candidateIDs("Counterparty", admin); narrowed {
		t.Error("an unconditional allow must fall back to a full scan, not narrow")
	}

	// staleness backstop: adding a Volga row is reflected after the write
	if _, err := eng.Create(ctx, admin, "Counterparty", map[string]any{"name": "CP-new", "region": "Volga"}, basis, ""); err != nil {
		t.Fatal(err)
	}
	if cand, _ := eng.candidateIDs("Counterparty", viewer); len(cand) != 301 {
		t.Errorf("after adding a Volga row, candidates = %d, want 301 (index rebuilt)", len(cand))
	}
}
