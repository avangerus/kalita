package engine

import (
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// LLD-09 §1/§4: shadow mode acceptance tests 1-3 and the I-1 golden slice of
// test 6 (an engine with no shadow events at all behaves exactly as before).

const shadowTestPack = `
entity Ticket:
    title: string required
    status: enum[Open, Closed] default=Open

workflow Ticket on status:
    Open -> Closed: close requires approval(Director)

roles:
    Owner
    Director
    Agent agent

permissions:
    Owner:
        full [Ticket]
    Director:
        read [Ticket]
        approve [close]
    Agent:
        read [Ticket]
        create [Ticket]
        act [close]
        deny [update Ticket.*, delete *]
`

func shadowEngine(t *testing.T) (*Engine, eventstore.Store) {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"shadow.dsl": shadowTestPack})
	if len(errs) > 0 {
		t.Fatalf("shadow test pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	e, err := New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	return e, store
}

var (
	shadowOwner    = eventstore.Actor{Type: eventstore.ActorHuman, ID: "owner-1", Role: "Owner"}
	shadowDirector = eventstore.Actor{Type: eventstore.ActorHuman, ID: "dir-1", Role: "Director"}
	shadowAgent    = eventstore.Actor{Type: eventstore.ActorAgent, ID: "agent-1", Role: "Agent"}
)

func lastEventKind(t *testing.T, store eventstore.Store) eventstore.Kind {
	t.Helper()
	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 {
		t.Fatal("journal is empty")
	}
	return events[len(events)-1].Kind
}

func TestShadowRedirectsToProposal(t *testing.T) {
	e, store := shadowEngine(t)

	if err := e.EnableShadow(ctx, shadowOwner, "Agent", basis); err != nil {
		t.Fatal(err)
	}
	if !e.IsShadowRole("Agent") {
		t.Fatal("Agent must be in shadow set after EnableShadow")
	}

	// shadow actor creates — must NOT land as record.created
	rec, err := e.Create(ctx, shadowAgent, "Ticket", map[string]any{"title": "t1"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if rec.ShadowProposalID == "" {
		t.Fatal("shadow create must return a ShadowProposalID")
	}
	if got := lastEventKind(t, store); got != eventstore.MutationProposed {
		t.Fatalf("shadow create must journal mutation.proposed, got %s", got)
	}
	if _, err := e.Get(ctx, shadowOwner, "Ticket", rec.ID); err == nil {
		t.Fatal("no record.created must exist for a shadow-proposed create")
	}
	pending := e.PendingMutationProposals()
	if len(pending) != 1 || pending[0].ID != rec.ShadowProposalID {
		t.Fatalf("want exactly the one pending proposal, got %v", pending)
	}

	// approve → record.created appears for real
	mp, err := e.DecideMutationProposal(ctx, shadowOwner, rec.ShadowProposalID, true, basis)
	if err != nil {
		t.Fatal(err)
	}
	if mp.Status != ProposalApplied {
		t.Fatalf("want applied, got %s", mp.Status)
	}
	if got := lastEventKind(t, store); got != eventstore.RecordCreated {
		t.Fatalf("approval must journal record.created, got %s", got)
	}
	rows, err := e.Query(ctx, shadowOwner, "Ticket", QueryOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Values["title"] != "t1" {
		t.Fatalf("the approved create must be visible, got %v", rows)
	}

	// disable shadow → next mutation applies directly
	if err := e.DisableShadow(ctx, shadowOwner, "Agent", basis); err != nil {
		t.Fatal(err)
	}
	if e.IsShadowRole("Agent") {
		t.Fatal("Agent must no longer be shadow after DisableShadow")
	}
	rec2, err := e.Create(ctx, shadowAgent, "Ticket", map[string]any{"title": "t2"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if rec2.ShadowProposalID != "" {
		t.Fatal("after DisableShadow, create must apply directly")
	}
	if got := lastEventKind(t, store); got != eventstore.RecordCreated {
		t.Fatalf("want direct record.created after disabling shadow, got %s", got)
	}
}

func TestShadowDoesNotBypassHITL(t *testing.T) {
	e, _ := shadowEngine(t)

	// a real ticket, created by a non-shadow actor
	ticket, err := e.Create(ctx, shadowOwner, "Ticket", map[string]any{"title": "t1"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}

	if err := e.EnableShadow(ctx, shadowOwner, "Agent", basis); err != nil {
		t.Fatal(err)
	}

	// shadow actor acts: close requires approval(Director) — must NOT raise a
	// real approval, must NOT move the record; only a shadow proposal.
	res, err := e.Act(ctx, shadowAgent, "Ticket", ticket.ID, "close", basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "shadow_proposed" || res.ShadowProposalID == "" {
		t.Fatalf("want shadow_proposed with a proposal id, got %+v", res)
	}
	if len(e.PendingApprovals("Director")) != 0 {
		t.Fatal("shadow act must not raise a real approval")
	}
	got, err := e.Get(ctx, shadowOwner, "Ticket", ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Values["status"] != "Open" {
		t.Fatalf("shadow act must not move the record, got status=%v", got.Values["status"])
	}

	// approve the shadow proposal: this re-runs Act for real — since `close`
	// requires approval(Director), it must PARK IN HITL again, not apply.
	mp, err := e.DecideMutationProposal(ctx, shadowOwner, res.ShadowProposalID, true, basis)
	if err != nil {
		t.Fatal(err)
	}
	if mp.Status != ProposalApplied {
		t.Fatalf("want the proposal itself applied (meaning: re-run succeeded), got %s", mp.Status)
	}
	got, err = e.Get(ctx, shadowOwner, "Ticket", ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Values["status"] != "Open" {
		t.Fatalf("record must still be Open — the real transition is parked in HITL, not applied, got %v", got.Values["status"])
	}
	pendingApprovals := e.PendingApprovals("Director")
	if len(pendingApprovals) != 1 {
		t.Fatalf("want exactly one real approval now pending for Director, got %d", len(pendingApprovals))
	}

	// the Director decides for real: NOW the transition applies.
	if _, err := e.Decide(ctx, shadowDirector, pendingApprovals[0].ID, true, nil, basis); err != nil {
		t.Fatal(err)
	}
	got, err = e.Get(ctx, shadowOwner, "Ticket", ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Values["status"] != "Closed" {
		t.Fatalf("after the real HITL decision the ticket must be Closed, got %v", got.Values["status"])
	}
}

func TestShadowReadUnaffected(t *testing.T) {
	e, _ := shadowEngine(t)

	ticket, err := e.Create(ctx, shadowOwner, "Ticket", map[string]any{"title": "t1"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.EnableShadow(ctx, shadowOwner, "Agent", basis); err != nil {
		t.Fatal(err)
	}

	// reads are untouched by shadow mode: Get/Query behave exactly as if the
	// actor's role were not in the shadow set.
	got, err := e.Get(ctx, shadowAgent, "Ticket", ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Values["title"] != "t1" {
		t.Fatalf("shadow actor must read normally, got %v", got.Values)
	}
	rows, err := e.Query(ctx, shadowAgent, "Ticket", QueryOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("shadow actor must query normally, got %d rows", len(rows))
	}
}

// Golden slice of test 6 (LLD-09 §4): an engine that never sees a single
// shadow/mutation-proposal event behaves identically to before — the new
// projections stay empty and inert.
func TestShadowInertWithoutEvents(t *testing.T) {
	e, _ := newEngine(t) // the pre-existing engine_test.go fixture, no shadow pack needed
	if len(e.ShadowRoles()) != 0 {
		t.Fatal("no role should be shadow on a fresh engine")
	}
	if len(e.PendingMutationProposals()) != 0 {
		t.Fatal("no mutation proposals should exist on a fresh engine")
	}
	doc, err := e.Create(ctx, admin, "Doc", map[string]any{"title": "a"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if doc.ShadowProposalID != "" {
		t.Fatal("ordinary create on a non-shadow role must not set ShadowProposalID")
	}
}
