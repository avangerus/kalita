package engine

import (
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Cycle-003 (G1 / D-010) regressions: Decide re-validates the transition guard
// against the CURRENT world. Between Act (park) and Decide the record fields,
// linked records, computed aggregates and the clock can all change — the signed
// decision must never apply a transition whose guard no longer holds. The
// re-evaluation uses the REQUESTER as the $me context and the current clock.

const decideGuardPack = `
entity Job:
    score: int
    status: enum[New, Done] default=New

roles:
    Manager
    Owner

permissions:
    Owner:
        full [Job]
        act  [finish]
    Manager:
        update  [Job]
        approve [finish]

workflow Job on status:
    New -> Done: finish requires approval(Manager) when score >= 100
`

func decideGuardEngine(t *testing.T, pack string) (*Engine, eventstore.Store) {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"dg.dsl": pack})
	if len(errs) > 0 {
		t.Fatalf("decide-guard pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	e, err := New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	return e, store
}

var (
	dgOwner = eventstore.Actor{Type: eventstore.ActorHuman, ID: "owner-1", Role: "Owner"}
	dgMgr   = eventstore.Actor{Type: eventstore.ActorHuman, ID: "mgr-1", Role: "Manager"}
)

// TestDecideRevalidatesGuardOwnField: a guard over the record's own field.
// Park with score=150, drop score to 50 while the signature is pending →
// Decide must fail GUARD_FAILED, journal nothing, and keep the approval
// pending; restoring the data lets the SAME approval be decided afterwards.
func TestDecideRevalidatesGuardOwnField(t *testing.T) {
	e, store := decideGuardEngine(t, decideGuardPack)

	job, err := e.Create(ctx, dgOwner, "Job", map[string]any{"score": 150.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Act(ctx, dgOwner, "Job", job.ID, "finish", basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "pending_approval" || res.ApprovalID == "" {
		t.Fatalf("finish must park for approval: %+v", res)
	}

	// the world changes while the signature is pending
	if _, err := e.Update(ctx, dgMgr, "Job", job.ID, map[string]any{"score": 50.0}, basis, ""); err != nil {
		t.Fatal(err)
	}

	// decide on the stale world: rejected before anything is journaled
	_, err = e.Decide(ctx, dgMgr, res.ApprovalID, true, nil, basis)
	ge := wantCode(t, err, CodeGuardFailed)
	if ge.FixHint == "" {
		t.Fatal("guard failures must explain themselves")
	}
	if got := e.PendingApprovals("Manager"); len(got) != 1 {
		t.Fatalf("failed decide must leave the approval pending, got %d", len(got))
	}
	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == eventstore.ApprovalGranted || ev.Kind == eventstore.ApprovalRejected {
			t.Fatalf("a failed decide must journal no decision, got %s", ev.Kind)
		}
	}
	if err := eventstore.VerifyChain(events); err != nil {
		t.Fatalf("chain must stay intact after a failed decide: %v", err)
	}

	// restore the data → the same approval can now be decided
	if _, err := e.Update(ctx, dgMgr, "Job", job.ID, map[string]any{"score": 150.0}, basis, ""); err != nil {
		t.Fatal(err)
	}
	rec, err := e.Decide(ctx, dgMgr, res.ApprovalID, true, nil, basis)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Values["status"] != "Done" {
		t.Fatalf("decide on a restored world must apply, got status=%v", rec.Values["status"])
	}

	// decisions stay final
	if _, err := e.Decide(ctx, dgMgr, res.ApprovalID, true, nil, basis); err == nil {
		t.Fatal("approval decisions are final")
	}
}

// TestDecideRejectNotBlockedByStaleGuard: the reject path applies nothing and
// must never be blocked by a stale guard (D-009 asymmetry).
func TestDecideRejectNotBlockedByStaleGuard(t *testing.T) {
	e, _ := decideGuardEngine(t, decideGuardPack)

	job, err := e.Create(ctx, dgOwner, "Job", map[string]any{"score": 150.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Act(ctx, dgOwner, "Job", job.ID, "finish", basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Update(ctx, dgMgr, "Job", job.ID, map[string]any{"score": 50.0}, basis, ""); err != nil {
		t.Fatal(err)
	}

	rec, err := e.Decide(ctx, dgMgr, res.ApprovalID, false, nil, basis)
	if err != nil {
		t.Fatalf("reject must never be blocked by a stale guard: %v", err)
	}
	if rec.Values["status"] != "New" {
		t.Fatalf("reject applies nothing, got status=%v", rec.Values["status"])
	}
	if got := e.PendingApprovals("Manager"); len(got) != 0 {
		t.Fatalf("rejected approval must leave the queue, got %d", len(got))
	}
}

// TestDecideRevalidatesRefPathGuard: a guard over a LINKED record's field
// (ref-path `item.amount`). Mutating the linked record between park and
// decide must fail the decision.
func TestDecideRevalidatesRefPathGuard(t *testing.T) {
	const pack = `
entity Job:
    item: ref[Item]
    status: enum[New, Done] default=New

entity Item:
    amount: int

roles:
    Manager
    Owner

permissions:
    Owner:
        full [Job, Item]
        act  [finish]
    Manager:
        update  [Job, Item]
        approve [finish]

workflow Job on status:
    New -> Done: finish requires approval(Manager) when item.amount >= 100
`
	e, _ := decideGuardEngine(t, pack)

	item, err := e.Create(ctx, dgOwner, "Item", map[string]any{"amount": 120.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	job, err := e.Create(ctx, dgOwner, "Job", map[string]any{"item": item.ID}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Act(ctx, dgOwner, "Job", job.ID, "finish", basis, "")
	if err != nil || res.Status != "pending_approval" {
		t.Fatalf("finish must park (guard true at park): %v %+v", err, res)
	}

	// the linked record changes while the signature is pending
	if _, err := e.Update(ctx, dgMgr, "Item", item.ID, map[string]any{"amount": 10.0}, basis, ""); err != nil {
		t.Fatal(err)
	}

	_, err = e.Decide(ctx, dgMgr, res.ApprovalID, true, nil, basis)
	wantCode(t, err, CodeGuardFailed)
	if got := e.PendingApprovals("Manager"); len(got) != 1 {
		t.Fatalf("failed decide must leave the approval pending, got %d", len(got))
	}
}

// TestDecideRevalidatesAggregateGuard: a guard over a computed aggregate
// (`total = sum(Item.amount where job = $self)`). Mutating an aggregate input
// between park and decide must fail the decision.
func TestDecideRevalidatesAggregateGuard(t *testing.T) {
	const pack = `
entity Job:
    total: int computed = sum(Item.amount where job = $self)
    status: enum[New, Done] default=New

entity Item:
    job: ref[Job]
    amount: int

roles:
    Manager
    Owner

permissions:
    Owner:
        full [Job, Item]
        act  [finish]
    Manager:
        update  [Job, Item]
        approve [finish]

workflow Job on status:
    New -> Done: finish requires approval(Manager) when total >= 100
`
	e, _ := decideGuardEngine(t, pack)

	job, err := e.Create(ctx, dgOwner, "Job", map[string]any{}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	item, err := e.Create(ctx, dgOwner, "Item", map[string]any{"job": job.ID, "amount": 120.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Act(ctx, dgOwner, "Job", job.ID, "finish", basis, "")
	if err != nil || res.Status != "pending_approval" {
		t.Fatalf("finish must park (total=120 at park): %v %+v", err, res)
	}

	// an aggregate input changes while the signature is pending
	if _, err := e.Update(ctx, dgMgr, "Item", item.ID, map[string]any{"amount": 10.0}, basis, ""); err != nil {
		t.Fatal(err)
	}

	_, err = e.Decide(ctx, dgMgr, res.ApprovalID, true, nil, basis)
	wantCode(t, err, CodeGuardFailed)
	if got := e.PendingApprovals("Manager"); len(got) != 1 {
		t.Fatalf("failed decide must leave the approval pending, got %d", len(got))
	}
}

// TestDecideRevalidatesClockGuard: a time-dependent guard. The expr grammar
// has no function calls — the elapsed time is a computed FIELD
// (`age = days_since(deadline)`) referenced by the guard. Advancing the engine
// clock between park and decide must fail the decision: the guard is
// re-evaluated with the current clock at decide time.
func TestDecideRevalidatesClockGuard(t *testing.T) {
	const pack = `
entity Job:
    deadline: date
    age: int computed = days_since(deadline)
    status: enum[New, Done] default=New

roles:
    Manager
    Owner

permissions:
    Owner:
        full [Job]
        act  [finish]
    Manager:
        update  [Job]
        approve [finish]

workflow Job on status:
    New -> Done: finish requires approval(Manager) when age <= 10
`
	model, errs := dsl.Compile(map[string]string{"dg.dsl": pack})
	if len(errs) > 0 {
		t.Fatalf("decide-guard pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	clock := time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
	clockPtr := &clock
	e, err := New(ctx, model, store, WithClock(func() time.Time { return *clockPtr }))
	if err != nil {
		t.Fatal(err)
	}

	job, err := e.Create(ctx, dgOwner, "Job", map[string]any{"deadline": "2026-06-06"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Act(ctx, dgOwner, "Job", job.ID, "finish", basis, "")
	if err != nil || res.Status != "pending_approval" {
		t.Fatalf("finish must park (5 days since deadline at park): %v %+v", err, res)
	}

	// time passes while the signature is pending
	*clockPtr = clock.Add(7 * 24 * time.Hour)

	_, err = e.Decide(ctx, dgMgr, res.ApprovalID, true, nil, basis)
	wantCode(t, err, CodeGuardFailed)
	if got := e.PendingApprovals("Manager"); len(got) != 1 {
		t.Fatalf("failed decide must leave the approval pending, got %d", len(got))
	}
}

// TestDecideGuardUsesRequesterContext: `$me` inside the guard must resolve to
// the REQUESTER (journaled at park), not the approver — the decide-time
// re-evaluation asks "is the request still justified", so a guard like
// `submitted_by = $me` stays true for the approver's decision even though the
// approver is a different actor.
func TestDecideGuardUsesRequesterContext(t *testing.T) {
	const pack = `
entity Job:
    submitted_by: string
    status: enum[New, Done] default=New

roles:
    Requester
    Manager

permissions:
    Requester:
        create [Job]
        act    [finish]
    Manager:
        read    [Job]
        approve [finish]

workflow Job on status:
    New -> Done: finish requires approval(Manager) when submitted_by = $me
`
	e, _ := decideGuardEngine(t, pack)

	requester := eventstore.Actor{Type: eventstore.ActorHuman, ID: "req-1", Role: "Requester"}
	job, err := e.Create(ctx, requester, "Job", map[string]any{"submitted_by": "req-1"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Act(ctx, requester, "Job", job.ID, "finish", basis, "")
	if err != nil || res.Status != "pending_approval" {
		t.Fatalf("finish must park: %v %+v", err, res)
	}

	// dgMgr is a different actor; with the requester as the $me context the
	// guard still holds and the decision applies.
	rec, err := e.Decide(ctx, dgMgr, res.ApprovalID, true, nil, basis)
	if err != nil {
		t.Fatalf("decide must use the requester's $me context: %v", err)
	}
	if rec.Values["status"] != "Done" {
		t.Fatalf("decide must apply, got status=%v", rec.Values["status"])
	}
}

// TestDecideRefusedWhenTransitionRemoved (LIM-7 / D-020): if the transition
// was removed from the DSL between park and decide, a grant is refused before
// anything is journaled — no guard-less synthetic move — the approval stays
// pending and can still be rejected.
func TestDecideRefusedWhenTransitionRemoved(t *testing.T) {
	const parkPack = `
entity Job:
    score: int
    status: enum[New, Done] default=New

roles:
    Manager
    Owner

permissions:
    Owner:
        full [Job]
        act  [finish]
    Manager:
        update  [Job]
        approve [finish]

workflow Job on status:
    New -> Done: finish requires approval(Manager) when score >= 100
`
	const changedPack = `
entity Job:
    score: int
    status: enum[New, Done] default=New

roles:
    Manager
    Owner

permissions:
    Owner:
        full [Job]
    Manager:
        update  [Job]
        approve [finish]

workflow Job on status:
    New -> Done: alternate
    Done -> New: finish
`
	model1, errs := dsl.Compile(map[string]string{"dg.dsl": parkPack})
	if len(errs) > 0 {
		t.Fatalf("park pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	e, err := New(ctx, model1, store)
	if err != nil {
		t.Fatal(err)
	}

	job, err := e.Create(ctx, dgOwner, "Job", map[string]any{"score": 150.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Act(ctx, dgOwner, "Job", job.ID, "finish", basis, "")
	if err != nil || res.Status != "pending_approval" {
		t.Fatalf("finish must park: %v %+v", err, res)
	}

	// the model changes: `finish` is no longer declared
	model2, errs := dsl.Compile(map[string]string{"dg.dsl": changedPack})
	if len(errs) > 0 {
		t.Fatalf("changed pack must compile: %v", errs[0])
	}
	e2, err := New(ctx, model2, store)
	if err != nil {
		t.Fatal(err)
	}

	// the approval survives replay; a grant is refused and nothing is journaled
	head := len(mustAll(t, store))
	_, err = e2.Decide(ctx, dgMgr, res.ApprovalID, true, nil, basis)
	wantCode(t, err, CodeConflict)
	if got := len(mustAll(t, store)); got != head {
		t.Fatalf("a refused grant must journal nothing, journal grew %d -> %d", head, got)
	}
	if rec, _ := e2.Get(ctx, dgOwner, "Job", job.ID); rec.Values["status"] != "New" {
		t.Fatalf("the record must not move, got status=%v", rec.Values["status"])
	}
	// reject stays possible (D-009)
	if _, err := e2.Decide(ctx, dgMgr, res.ApprovalID, false, nil, basis); err != nil {
		t.Fatalf("reject must still apply: %v", err)
	}
}

func mustAll(t *testing.T, s eventstore.Store) []*eventstore.Event {
	t.Helper()
	evs, err := s.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

// TestDecideGuardFailureReplays: a failed decide journals nothing, so replaying
// the journal reproduces exactly the same world (approval pending, mutated
// data in place); the decision then applies after the data is fixed and the
// replay of the completed journal matches the live projections.
func TestDecideGuardFailureReplays(t *testing.T) {
	e, store := decideGuardEngine(t, decideGuardPack)

	job, err := e.Create(ctx, dgOwner, "Job", map[string]any{"score": 150.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Act(ctx, dgOwner, "Job", job.ID, "finish", basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Update(ctx, dgMgr, "Job", job.ID, map[string]any{"score": 50.0}, basis, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Decide(ctx, dgMgr, res.ApprovalID, true, nil, basis); err == nil {
		t.Fatal("decide on the stale world must fail")
	}

	// rebuild from the journal: the failed decide left no trace
	e2, err := New(ctx, e.Model(), store)
	if err != nil {
		t.Fatal(err)
	}
	rec2, err := e2.Get(ctx, dgOwner, "Job", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec2.Values["score"] != 50.0 || rec2.Values["status"] != "New" {
		t.Fatalf("replay must reproduce the mutated-but-unmoved world, got %+v", rec2.Values)
	}
	if got := e2.PendingApprovals("Manager"); len(got) != 1 {
		t.Fatalf("the approval must replay as pending, got %d", len(got))
	}

	// fix the data on the replayed engine and decide — the same path as live
	if _, err := e2.Update(ctx, dgMgr, "Job", job.ID, map[string]any{"score": 150.0}, basis, ""); err != nil {
		t.Fatal(err)
	}
	rec, err := e2.Decide(ctx, dgMgr, res.ApprovalID, true, nil, basis)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Values["status"] != "Done" {
		t.Fatalf("decide after restore must apply, got status=%v", rec.Values["status"])
	}

	// replay the completed journal: identical final projections
	e3, err := New(ctx, e.Model(), store)
	if err != nil {
		t.Fatal(err)
	}
	rec3, err := e3.Get(ctx, dgOwner, "Job", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rec3.Values["status"] != "Done" {
		t.Fatalf("final replay must be Done, got %v", rec3.Values["status"])
	}
	if got := e3.PendingApprovals("Manager"); len(got) != 0 {
		t.Fatalf("decided approval must not replay as pending, got %d", len(got))
	}
	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := eventstore.VerifyChain(events); err != nil {
		t.Fatalf("chain must verify: %v", err)
	}
}
