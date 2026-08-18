package engine

import (
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// TestProposalAddsPackToMultiPackNode is the product-mission kernel contract:
// on a node already running packs (here: builder + finance-like test packs),
// a proposal that merges a NEW pack into the full current source set applies
// — the additive gate sees nothing removed. This is what the conversational
// builder does via get_definition -> merge -> propose_change.
func TestProposalAddsPackToMultiPackNode(t *testing.T) {
	boot := map[string]string{
		"one.dsl": "pack one\nversion 0.1.0\n\nentity Alpha:\n    title: string\n",
		"two.dsl": "pack two\nversion 0.1.0\n\nentity Beta:\n    title: string\n",
	}
	model, errs := dsl.CompileMulti(boot)
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	e, err := New(ctx, model, store, WithSources(boot))
	if err != nil {
		t.Fatal(err)
	}
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}

	merged := map[string]string{}
	for k, v := range boot {
		merged[k] = v
	}
	merged["three.dsl"] = "pack three\nversion 0.1.0\n\nentity Gamma:\n    title: string\n"

	p, dslErrs, err := e.ProposeChange(ctx, admin, merged, e.DefVersion(),
		"add pack three (merged definition)", basis, "idem-multi-0001")
	if err != nil || len(dslErrs) > 0 {
		t.Fatalf("propose merged: err=%v dslErrs=%v", err, dslErrs)
	}
	if _, err := e.DecideProposal(ctx, owner, p.ID, true, nil, basis); err != nil {
		t.Fatalf("decide merged: %v", err)
	}
	if e.model.Entities["Gamma"] == nil || e.model.Entities["Alpha"] == nil || e.model.Entities["Beta"] == nil {
		t.Fatal("after apply, ALL packs' entities must exist")
	}
	files, defV := e.DefinitionSources()
	if files["three.dsl"] == "" || defV != 2 {
		t.Fatalf("DefinitionSources must expose the applied sources: %v def=%d", files, defV)
	}
}

// TestProposalReplacingDefinitionStillRejected: a proposal that drops an
// existing pack is refused by the additive gate — the LLM can never remove
// what exists, merged-set or not.
func TestProposalReplacingDefinitionStillRejected(t *testing.T) {
	boot := map[string]string{
		"one.dsl": "pack one\nversion 0.1.0\n\nentity Alpha:\n    title: string\n",
	}
	model, errs := dsl.CompileMulti(boot)
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	e, err := New(ctx, model, store, WithSources(boot))
	if err != nil {
		t.Fatal(err)
	}
	// replaces the whole definition WITHOUT pack one — Alpha would disappear
	_, _, err = e.ProposeChange(ctx, admin, map[string]string{
		"three.dsl": "pack three\nversion 0.1.0\n\nentity Gamma:\n    title: string\n",
	}, e.DefVersion(), "drop pack one", basis, "idem-multi-0002")
	if err == nil {
		t.Fatal("a definition-dropping proposal must be rejected by the additive gate")
	}
}

// TestProposalsIsTheModelTimeline: Proposals() returns pending AND decided
// changes newest-first (uuid v7 = time order) — the product Changes/Audit
// view's data contract (mission C: who changed the company, when, why).
func TestProposalsIsTheModelTimeline(t *testing.T) {
	boot := map[string]string{
		"one.dsl": "pack one\nversion 0.1.0\n\nentity Alpha:\n    title: string\n",
	}
	model, errs := dsl.CompileMulti(boot)
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	e, err := New(ctx, model, store, WithSources(boot))
	if err != nil {
		t.Fatal(err)
	}
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	mk := func(idem string) *Proposal {
		merged := map[string]string{"one.dsl": boot["one.dsl"],
			"two.dsl": "pack two\nversion 0.1.0\n\nentity Beta:\n    title: string\n"}
		p, dslErrs, err := e.ProposeChange(ctx, admin, merged, e.DefVersion(),
			"add two", basis, idem)
		if err != nil || len(dslErrs) > 0 {
			t.Fatalf("propose: %v %v", err, dslErrs)
		}
		return p
	}
	p1 := mk("idem-timeline-1")
	p2 := mk("idem-timeline-2")
	if _, err := e.DecideProposal(ctx, owner, p1.ID, false, nil, basis); err != nil {
		t.Fatal(err)
	}
	// p1 rejected, p2 pending: both must appear, newest (p2) first
	all := e.Proposals()
	if len(all) != 2 {
		t.Fatalf("Proposals() must include decided ones, got %d", len(all))
	}
	if all[0].ID != p2.ID || all[1].ID != p1.ID {
		t.Fatalf("timeline order wrong: %s, %s", all[0].ID, all[1].ID)
	}
	if all[1].Status != ProposalRejected {
		t.Fatalf("decided proposal must keep its status, got %s", all[1].Status)
	}
	pending := e.PendingProposals()
	if len(pending) != 1 || pending[0].ID != p2.ID {
		t.Fatalf("PendingProposals must stay pending-only: %+v", pending)
	}
}

// TestDefinitionSourcesReplay: the sources state must survive a restart —
// they replay from the DefinitionApplied payload exactly like the model.
func TestDefinitionSourcesReplay(t *testing.T) {
	boot := map[string]string{
		"one.dsl": "pack one\nversion 0.1.0\n\nentity Alpha:\n    title: string\n",
	}
	model, errs := dsl.CompileMulti(boot)
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	e, err := New(ctx, model, store, WithSources(boot))
	if err != nil {
		t.Fatal(err)
	}
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	merged := map[string]string{"one.dsl": boot["one.dsl"],
		"two.dsl": "pack two\nversion 0.1.0\n\nentity Beta:\n    title: string\n"}
	p, dslErrs, err := e.ProposeChange(ctx, admin, merged, e.DefVersion(),
		"add pack two", basis, "idem-multi-0003")
	if err != nil || len(dslErrs) > 0 {
		t.Fatalf("propose: %v %v", err, dslErrs)
	}
	if _, err := e.DecideProposal(ctx, owner, p.ID, true, nil, basis); err != nil {
		t.Fatal(err)
	}
	e2, err := New(ctx, model, store) // restart WITHOUT WithSources: replay must reseed
	if err != nil {
		t.Fatal(err)
	}
	files, defV := e2.DefinitionSources()
	if defV != 2 || files["two.dsl"] == "" {
		t.Fatalf("sources must replay from the journal: %v def=%d", files, defV)
	}
}
