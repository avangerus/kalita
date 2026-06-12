package engine

import (
	"context"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Multi-step rewind: after several edits and transitions, restore the record to
// an earlier point in one move — "rewind to before the agent touched it." The
// past state is reconstructed from history; compensating events walk current ->
// target; the journal is preserved and still verifies.
func TestRewindToEarlierState(t *testing.T) {
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
    Worker agent
permissions:
    Supervisor:
        full [Ticket]
        act  [start, finish]
    Worker:
        read   [Ticket]
        update [Ticket]
        act    [start, finish]
        deny   [delete *, update Ticket.status]
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
	createSeq, _, _ := store.Head(ctx) // the state we will rewind back to

	// a flurry of changes — the kind an agent might make
	mustUpdate(t, ctx, eng, sup, id, map[string]any{"note": "n1"}, basis)
	mustUpdate(t, ctx, eng, sup, id, map[string]any{"note": "n2", "title": "A-edited"}, basis)
	mustAct(t, ctx, eng, sup, id, "start", basis)  // Open -> Working
	mustAct(t, ctx, eng, sup, id, "finish", basis) // Working -> Done
	if g, _ := eng.Get(ctx, sup, "Ticket", id); g.Values["status"] != "Done" || g.Values["note"] != "n2" {
		t.Fatalf("pre-rewind state wrong: %v", g.Values)
	}

	headBefore, _, _ := store.Head(ctx)
	got, err := eng.RewindTo(ctx, sup, "Ticket", id, createSeq, basis)
	if err != nil {
		t.Fatalf("rewind: %v", err)
	}
	// every field and the workflow state are back to creation, in one rewind
	if got.Values["note"] != "n0" || got.Values["title"] != "A" || got.Values["status"] != "Open" {
		t.Fatalf("after rewind = %v, want note n0 / title A / status Open", got.Values)
	}
	// history grew (a field-undo event + a state-undo event), it was not rewritten
	headAfter, _, _ := store.Head(ctx)
	if headAfter <= headBefore {
		t.Fatalf("rewind must append compensating events, seq %d -> %d", headBefore, headAfter)
	}
	if err := store.Verify(ctx); err != nil {
		t.Fatalf("journal must verify after rewind: %v", err)
	}

	// rewinding before the record existed is refused
	if _, err := eng.RewindTo(ctx, sup, "Ticket", id, createSeq-1, basis); err == nil {
		t.Fatal("rewind to before creation must fail")
	}
	// an agent without `full` cannot rewind
	wrk := eventstore.Actor{Type: eventstore.ActorAgent, ID: "bot", Role: "Worker"}
	if _, err := eng.RewindTo(ctx, wrk, "Ticket", id, createSeq, basis); err == nil {
		t.Fatal("an agent without full permission must not rewind")
	}
}

func mustUpdate(t *testing.T, ctx context.Context, eng *Engine, a eventstore.Actor, id string, v map[string]any, b *eventstore.Basis) {
	t.Helper()
	if _, err := eng.Update(ctx, a, "Ticket", id, v, b, ""); err != nil {
		t.Fatal(err)
	}
}

func mustAct(t *testing.T, ctx context.Context, eng *Engine, a eventstore.Actor, id, action string, b *eventstore.Basis) {
	t.Helper()
	if _, err := eng.Act(ctx, a, "Ticket", id, action, b, ""); err != nil {
		t.Fatal(err)
	}
}
