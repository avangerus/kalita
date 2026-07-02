package engine

import (
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/eventstore"
)

// TestRewindCrashConsistency is acceptance test 3 (LLD-01 §7): a rewind that
// must compensate BOTH a field diff and a workflow-state move is a two-event
// operation (LLD-01 §2, "rewind с >1 компенсацией") and gets an op-bracket.
// Killing the process between the two compensating events must not leave a
// silent half-rewound record — the next startup marks it Inconsistent.
func TestRewindCrashConsistency(t *testing.T) {
	e := automationEngine(t)
	contract, err := e.Create(ctx, anna, "Contract",
		map[string]any{"company": "Vector LLC", "due_date": "2026-06-09", "amount": 50000.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	debtor, err := e.Create(ctx, anna, "Debtor",
		map[string]any{"company": "Vector LLC", "contract": contract.ID, "debt": 50000.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	// creation auto-transitions Overdue -> capture the seq right here: rewinding
	// to it later must undo both the debt edit AND the send_claim transition
	// below, i.e. exactly the two-event compensation case.
	head, _, err := e.store.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	toSeq := head

	if _, err := e.Update(ctx, anna, "Debtor", debtor.ID, map[string]any{"debt": 75000.0}, basis, ""); err != nil {
		t.Fatal(err)
	}
	open := e.Tasks("Collector", TaskOpen)
	var sendClaim *Task
	for _, tk := range open {
		if tk.Action == "send_claim" {
			sendClaim = tk
		}
	}
	if sendClaim == nil {
		t.Fatalf("send_claim task expected after creation, got %+v", open)
	}
	if _, err := e.TakeTask(ctx, collector, sendClaim.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Act(ctx, collector, "Debtor", debtor.ID, "send_claim", basis, ""); err != nil {
		t.Fatal(err)
	}
	rec, err := e.Get(ctx, anna, "Debtor", debtor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Values["status"] != "Claim" {
		t.Fatalf("want Claim before rewind, got %v", rec.Values["status"])
	}

	// wrap the underlying store: allow everything so far, then let exactly ONE
	// more Append through (the field-diff compensation) before the state-move
	// compensation fails — simulating a kill between the two rewind events.
	mem := e.store.(*eventstore.MemStore)
	fs := &faultyStore{Store: mem, failAfter: 1 << 30}
	e2raw, err := New(ctx, e.Model(), fs, WithClock(func() time.Time { return *e.clock }))
	if err != nil {
		t.Fatal(err)
	}
	fs.failAfter = fs.calls + 1 // field-diff append allowed; state-move append fails

	if _, err := e2raw.RewindTo(ctx, anna, "Debtor", debtor.ID, toSeq, basis, ""); err == nil {
		t.Fatal("RewindTo must surface the simulated crash as an error")
	}

	// confirm the op indeed opened and did NOT close (matches "killed between
	// compensations", not "never started").
	if lastEventOfKind(t, e2raw, eventstore.OpBegin, "Debtor", debtor.ID) == nil {
		t.Fatal("op.begin must have been journaled before the simulated crash")
	}
	if got := countEventsOfKind(t, e2raw, eventstore.OpEnd); got != 0 {
		t.Fatalf("op.end must not have been journaled: got %d", got)
	}

	// "restart": fresh engine straight off the durable store.
	e3, err := New(ctx, e.Model(), mem, WithClock(func() time.Time { return *e.clock }))
	if err != nil {
		t.Fatal(err)
	}
	if lastEventOfKind(t, e3, eventstore.OpAborted, "Debtor", debtor.ID) == nil {
		t.Fatal("startup recovery must journal op.aborted for the unclosed rewind bracket")
	}
	rec3, err := e3.Get(ctx, anna, "Debtor", debtor.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !rec3.Inconsistent {
		t.Fatal("a record left mid-rewind by a crash must be marked Inconsistent, not silently desynced")
	}
	esc := e3.Tasks(e3.defApprover, TaskOpen)
	found := false
	for _, tk := range esc {
		if tk.Kind == TaskEscalation && tk.RecordID == debtor.ID && tk.Action == "inconsistent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("recovery must escalate the inconsistent rewind to the approver role %s, got: %+v", e3.defApprover, esc)
	}

	// and mutation is refused until the supervisor acts.
	_, actErr := e3.Act(ctx, collector, "Debtor", debtor.ID, "send_claim", basis, "")
	wantCode(t, actErr, CodeRecordInconsistent)
}
