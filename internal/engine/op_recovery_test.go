package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// faultyStore wraps a Store and, after failAfter successful Appends, fails
// every subsequent one — simulating a process crash: nothing more is ever
// durably written. The underlying store keeps whatever made it through, so a
// fresh Engine built straight off it (bypassing the fault injector) is
// exactly "restart reads the durable journal".
type faultyStore struct {
	eventstore.Store
	failAfter int
	calls     int
}

func (f *faultyStore) Append(ctx context.Context, in eventstore.AppendInput) (*eventstore.Event, error) {
	f.calls++
	if f.calls > f.failAfter {
		return nil, errors.New("faultyStore: simulated crash")
	}
	return f.Store.Append(ctx, in)
}

// TestOpBracketsRecovery is acceptance test 2 (LLD-01 §7): a store failure
// injected right after op.begin (simulating kill -9 mid-cascade) must be
// healed by the next startup — op.aborted journaled, the record marked
// Inconsistent, an escalation task created for the approver role — and until
// confirm_consistent, ordinary mutations on that record are rejected with
// RECORD_INCONSISTENT.
func TestOpBracketsRecovery(t *testing.T) {
	model, errs := dsl.Compile(map[string]string{"test.dsl": testPack})
	if len(errs) > 0 {
		t.Fatalf("compile: %v", errs[0])
	}
	mem := eventstore.NewMemStore(nil)
	fs := &faultyStore{Store: mem, failAfter: 1 << 30} // effectively unlimited until armed below
	e, err := New(ctx, model, fs)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := e.Create(ctx, admin, "Doc", map[string]any{"title": "x"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}

	// allow exactly one more successful append (op.begin) — everything after
	// that fails, as if the process died right after the bracket opened.
	fs.failAfter = fs.calls + 1

	// Two actions so the cascade is op-bracketed (LLD-01 §2: single-action
	// rules are one atomic event and deliberately get no bracket).
	rule := &dsl.AutomationRule{File: "test2", Line: 1, Actions: []dsl.AutomationAction{
		{Kind: "escalate", Role: "Admin"},
		{Kind: "notify", Args: `email(ok)`},
	}}
	e.fireActions(ctx, rule, "Doc", doc.ID, "") // op.begin ok; every later Append (actions, op.end) fails

	if got := countEventsOfKind(t, e, eventstore.OpEnd); got != 0 {
		t.Fatalf("op.end must NOT have been journaled (simulated crash before it): got %d", got)
	}
	if lastEventOfKind(t, e, eventstore.OpBegin, "Doc", doc.ID) == nil {
		t.Fatal("op.begin must have been journaled before the simulated crash")
	}

	// "restart": a fresh engine straight off the durable store (mem), not the
	// fault injector — the crash is over, this is what the next boot reads.
	e2, err := New(ctx, model, mem)
	if err != nil {
		t.Fatal(err)
	}

	aborted := lastEventOfKind(t, e2, eventstore.OpAborted, "Doc", doc.ID)
	if aborted == nil {
		t.Fatal("startup recovery must journal op.aborted for the unclosed bracket")
	}

	rec, err := e2.Get(ctx, admin, "Doc", doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Inconsistent {
		t.Fatal("record must be marked Inconsistent after recovery")
	}

	esc := e2.Tasks(e2.defApprover, TaskOpen)
	var found *Task
	for _, tk := range esc {
		if tk.Kind == TaskEscalation && tk.RecordID == doc.ID && tk.Action == "inconsistent" {
			found = tk
		}
	}
	if found == nil {
		t.Fatalf("recovery must escalate to the approver role %s, got: %+v", e2.defApprover, esc)
	}

	// ordinary mutations are rejected until confirmed or rewound.
	_, updErr := e2.Update(ctx, admin, "Doc", doc.ID, map[string]any{"title": "y"}, basis, "")
	wantCode(t, updErr, CodeRecordInconsistent)

	// confirm_consistent by a non-approver is denied.
	if _, err := e2.ConfirmConsistent(ctx, editor, "Doc", doc.ID, basis); err == nil {
		t.Fatal("confirm_consistent must be denied to a non-approver role")
	}

	// confirm_consistent by the approver (Admin, the default "Owner" is not a
	// role in testPack, so wire the approver to a role that exists) clears it.
	e2.defApprover = "Admin"
	// re-run recovery's own escalation target check would need a fresh boot to
	// pick up the new approver; confirm_consistent itself only checks the
	// CURRENT defApprover at call time, which is what we just set.
	confirmActor := eventstore.Actor{Type: eventstore.ActorHuman, ID: "alice", Role: "Admin"}
	confirmed, err := e2.ConfirmConsistent(ctx, confirmActor, "Doc", doc.ID, basis)
	if err != nil {
		t.Fatalf("approver must be able to confirm_consistent: %v", err)
	}
	if confirmed.Inconsistent {
		t.Fatal("confirm_consistent must clear Inconsistent")
	}

	// mutations now succeed again.
	if _, err := e2.Update(ctx, admin, "Doc", doc.ID, map[string]any{"title": "z"}, basis, ""); err != nil {
		t.Fatalf("mutation must succeed once consistency is confirmed: %v", err)
	}

	// and a THIRD boot must not re-flag it (op.aborted already closed the op).
	e3, err := New(ctx, model, mem)
	if err != nil {
		t.Fatal(err)
	}
	rec3, err := e3.Get(ctx, admin, "Doc", doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec3.Inconsistent {
		t.Fatal("a confirmed record must stay consistent across a further restart")
	}
}
