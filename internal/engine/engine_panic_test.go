package engine

import (
	"reflect"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// TestFoldSurvivesPanicInDefinitionCompile proves the RELEASE-1.0 N8 §7.7
// catastrophic scenario cannot happen: a DefinitionApplied event already in
// the journal whose compile would panic must not crash replay — which runs
// on EVERY node restart (the pack directory is only the genesis seed;
// definitions live in the journal, see applyEvent's DefinitionApplied case).
//
// dsl.Compile itself can no longer panic — it recovers internally (see
// TestCompileRecoversInternalPanic in internal/dsl/panic_test.go), which is
// what actually makes journal poisoning impossible in production. This test
// injects the panic at the dslCompile seam (engine.go) instead, to prove the
// FOLD PATH ITSELF survives even in the hypothetical case that barrier were
// ever bypassed — belt and suspenders for the one call that, unlike a live
// request, cannot simply be retried: the poison is already in the journal.
func TestFoldSurvivesPanicInDefinitionCompile(t *testing.T) {
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}

	baseModel, errs := dsl.Compile(map[string]string{"test.dsl": testPack})
	if len(errs) > 0 {
		t.Fatalf("test pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	e, err := New(ctx, baseModel, store)
	if err != nil {
		t.Fatal(err)
	}

	// A real, additive, perfectly valid change — proposed and approved
	// through the normal live pipeline, exactly like a human/agent would.
	// This exercises the REAL dsl.Compile at propose+decide time (unaffected
	// by the fault injection below, which only targets the fold/replay call).
	nextFiles := map[string]string{"test.dsl": testPack + "\nentity Boom:\n    x: int\n"}
	p, dslErrs, err := e.ProposeChange(ctx, admin, nextFiles, e.DefVersion(), "add Boom", basis, "idem-add-boom-0001")
	if err != nil || len(dslErrs) > 0 {
		t.Fatalf("propose: err=%v dslErrs=%v", err, dslErrs)
	}
	if _, err := e.DecideProposal(ctx, owner, p.ID, true, nil, basis); err != nil {
		t.Fatalf("decide: %v", err)
	}
	if _, ok := e.model.Entities["Boom"]; !ok {
		t.Fatal("sanity: live apply must have added Boom")
	}

	// Now simulate a restart: replay the SAME journal, but with dslCompile
	// forced to panic — as if the journal's DefinitionApplied payload were
	// the poisoned DSL construct §7.7 warns about.
	orig := dslCompile
	defer func() { dslCompile = orig }()
	dslCompile = func(files map[string]string) (*dsl.Model, []*dsl.Error) {
		panic("simulated compiler panic reaching fold replay")
	}

	e2, err := New(ctx, baseModel, store)
	if err != nil {
		t.Fatalf("replay must survive a panicking definition compile, got a hard error instead: %v", err)
	}
	if _, ok := e2.model.Entities["Boom"]; ok {
		t.Fatal("a recovered panic must degrade like any other compile failure: Boom must NOT be applied")
	}
	if e2.defVersion != e.defVersion {
		t.Fatalf("def_version must still advance across a recovered panic: got %d want %d", e2.defVersion, e.defVersion)
	}
	gotProp, err := e2.GetProposal(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotProp.Status != ProposalApplied {
		t.Fatalf("proposal status = %s, want applied (fold must not stall on a recovered panic)", gotProp.Status)
	}

	// Determinism under the SAME injected fault: replaying twice with the
	// panicking dslCompile must produce an identical degraded model — the
	// recover must not leak wall-clock time or any other non-deterministic
	// value into the projection (I-5: fold is a pure function of event
	// order).
	e2b, err := New(ctx, baseModel, store)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(e2.model, e2b.model) {
		t.Fatal("two replays under the identical fault must produce identical models (I-5 replay-determinism)")
	}

	// Restore the real compiler and replay again: normal replay must recover
	// completely and match the live-applied state exactly — the earlier
	// fault injection must leave no trace in the journal or anywhere else.
	dslCompile = orig
	e3, err := New(ctx, baseModel, store)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := e3.model.Entities["Boom"]; !ok {
		t.Fatal("replay with the real compiler must apply Boom, matching the live engine")
	}
	if !reflect.DeepEqual(e.model, e3.model) {
		t.Fatal("replay with the real compiler must match live-applied state exactly (I-5 replay-determinism)")
	}
}
