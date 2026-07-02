package engine

import (
	"context"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// "Откат любого действия" — undo a field edit AND a workflow transition, by
// appending compensating events (the journal is never rewritten), with the undo
// itself reversible (redo). A supervisory capability: an agent without `full`
// cannot do it, and reverting a bare creation has nothing to undo.
func TestRevertUndoesEditAndTransition(t *testing.T) {
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

	rec, err := eng.Create(ctx, sup, "Ticket", map[string]any{"title": "A", "note": "first"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	id := rec.ID

	// reverting a bare creation has nothing to undo
	if _, err := eng.Revert(ctx, sup, "Ticket", id, basis, ""); err == nil {
		t.Fatal("reverting a freshly created record must fail (nothing to undo)")
	}

	// --- undo a field edit ---
	if _, err := eng.Update(ctx, sup, "Ticket", id, map[string]any{"note": "second"}, basis, ""); err != nil {
		t.Fatal(err)
	}
	headBefore, _, _ := store.Head(ctx)
	rev, err := eng.Revert(ctx, sup, "Ticket", id, basis, "")
	if err != nil {
		t.Fatalf("revert edit: %v", err)
	}
	if rev.Values["note"] != "first" {
		t.Fatalf("after revert note = %v, want \"first\"", rev.Values["note"])
	}
	// the undo is an APPENDED event — history grew, it was not erased
	headAfter, _, _ := store.Head(ctx)
	if headAfter != headBefore+1 {
		t.Fatalf("revert must append one compensating event, seq went %d -> %d", headBefore, headAfter)
	}

	// --- undo a workflow transition (the move a plain update cannot make) ---
	if _, err := eng.Act(ctx, sup, "Ticket", id, "start", basis, ""); err != nil {
		t.Fatal(err)
	}
	if g, _ := eng.Get(ctx, sup, "Ticket", id); g.Values["status"] != "Working" {
		t.Fatalf("after start, status = %v, want Working", g.Values["status"])
	}
	back, err := eng.Revert(ctx, sup, "Ticket", id, basis, "")
	if err != nil {
		t.Fatalf("revert transition: %v", err)
	}
	if back.Values["status"] != "Open" {
		t.Fatalf("revert of start -> status = %v, want Open", back.Values["status"])
	}

	// --- redo: reverting the revert moves it forward again ---
	redo, err := eng.Revert(ctx, sup, "Ticket", id, basis, "")
	if err != nil {
		t.Fatalf("revert of revert: %v", err)
	}
	if redo.Values["status"] != "Working" {
		t.Fatalf("revert of revert -> status = %v, want Working (redo)", redo.Values["status"])
	}

	// --- an agent without `full` cannot revert ---
	wrk := eventstore.Actor{Type: eventstore.ActorAgent, ID: "bot", Role: "Worker"}
	if _, err := eng.Revert(ctx, wrk, "Ticket", id, basis, ""); err == nil {
		t.Fatal("an agent without full permission must not revert")
	} else if e, ok := err.(*Err); !ok || e.Code != CodePermissionDenied {
		t.Fatalf("want PERMISSION_DENIED, got %v", err)
	}

	// the hash chain is intact through all the reverts — history is tamper-evident,
	// and undo never broke it
	if err := store.Verify(ctx); err != nil {
		t.Fatalf("journal must verify after reverts: %v", err)
	}
}
