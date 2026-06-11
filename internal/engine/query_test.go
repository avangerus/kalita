package engine

import (
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Query v2: rich where, multi-field sort, full-text search.
func TestQueryV2(t *testing.T) {
	src := `
entity Deal:
    title: string required
    amount: int
    stage: enum[Lead, Won, Lost] default=Lead

roles:
    Owner

permissions:
    Owner:
        full [Deal]
`
	model, errs := dsl.Compile(map[string]string{"t.kal": src})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	o := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	mk := func(title string, amt float64, stage string) {
		_, err := e.Create(ctx, o, "Deal", map[string]any{"title": title, "amount": amt, "stage": stage}, basis, "")
		if err != nil {
			t.Fatal(err)
		}
	}
	mk("Acme contract", 5000, "Lead")
	mk("Globex deal", 12000, "Won")
	mk("Initech renewal", 8000, "Lead")
	mk("Umbrella loss", 3000, "Lost")

	// where: open deals over 6000
	rows, _ := e.Query(ctx, o, "Deal", QueryOpts{Where: "stage != Lost and amount > 6000"})
	if len(rows) != 2 {
		t.Fatalf("where: want 2 (Globex, Initech), got %d", len(rows))
	}

	// sort descending by amount
	rows, _ = e.Query(ctx, o, "Deal", QueryOpts{Sort: []string{"-amount"}})
	if rows[0].Values["title"] != "Globex deal" || rows[3].Values["title"] != "Umbrella loss" {
		t.Fatalf("sort -amount wrong: %v ... %v", rows[0].Values["title"], rows[3].Values["title"])
	}

	// full-text search
	rows, _ = e.Query(ctx, o, "Deal", QueryOpts{Search: "renewal"})
	if len(rows) != 1 || rows[0].Values["title"] != "Initech renewal" {
		t.Fatalf("search: want Initech renewal, got %v", rows)
	}

	// combined: open deals, sorted, limited
	rows, _ = e.Query(ctx, o, "Deal", QueryOpts{Where: "stage = Lead", Sort: []string{"-amount"}, Limit: 1})
	if len(rows) != 1 || rows[0].Values["title"] != "Initech renewal" {
		t.Fatalf("combined: want top Lead by amount = Initech, got %v", rows)
	}
}
