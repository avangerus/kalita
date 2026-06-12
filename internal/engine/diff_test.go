package engine

import (
	"context"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Diff is the NET change between two journal points, not the event log: a field
// edited away and back shows nothing; a field genuinely changed shows old→new;
// a workflow move is reported separately. "What did the agent touch?" answered
// in one read.
func TestDiffNetChange(t *testing.T) {
	ctx := context.Background()
	src := `pack t
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1
entity Ticket:
    title:  string
    note:   string
    status: enum[Open, Working, Done] default=Open
workflow Ticket on status:
    Open    -> Working: start
    Working -> Done:    finish
roles:
    Supervisor
permissions:
    Supervisor:
        full [Ticket]
        act  [start, finish]
`
	model, errs := dsl.Compile(map[string]string{"t.dsl": src})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	eng, err := New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	sup := eventstore.Actor{Type: eventstore.ActorHuman, ID: "sup", Role: "Supervisor"}
	basis := &eventstore.Basis{Type: "human", ID: "sup"}

	rec, err := eng.Create(ctx, sup, "Ticket", map[string]any{"title": "A", "note": "n0"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	id := rec.ID
	createSeq, _, _ := store.Head(ctx)

	mustUpdate(t, ctx, eng, sup, id, map[string]any{"note": "n1"}, basis)
	mustUpdate(t, ctx, eng, sup, id, map[string]any{"note": "n2"}, basis)
	mustUpdate(t, ctx, eng, sup, id, map[string]any{"note": "n0"}, basis)        // back to start: net zero
	mustUpdate(t, ctx, eng, sup, id, map[string]any{"title": "A-edited"}, basis) // genuine change
	mustAct(t, ctx, eng, sup, id, "start", basis)                                // Open -> Working

	d, err := eng.Diff(ctx, sup, "Ticket", id, createSeq, 0) // 0 = now
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Fields) != 1 || d.Fields[0].Field != "title" ||
		d.Fields[0].Old != "A" || d.Fields[0].New != "A-edited" {
		t.Fatalf("net field diff = %+v, want only title A→A-edited (note churned back to n0)", d.Fields)
	}
	if d.StateFrom != "Open" || d.StateTo != "Working" {
		t.Fatalf("state diff = %s→%s, want Open→Working", d.StateFrom, d.StateTo)
	}

	// diffing a point against itself is empty
	head, _, _ := store.Head(ctx)
	same, _ := eng.Diff(ctx, sup, "Ticket", id, head, head)
	if len(same.Fields) != 0 || same.StateFrom != "" {
		t.Fatalf("diff of a point against itself must be empty, got %+v", same)
	}
}
