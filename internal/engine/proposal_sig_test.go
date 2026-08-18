package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// proposalSigEngine returns an engine built over the shared testPack with the
// given options, plus an Owner-role human actor to decide with.
func proposalSigEngine(t *testing.T, opts ...Option) (*Engine, eventstore.Store, eventstore.Actor) {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"test.dsl": testPack})
	if len(errs) > 0 {
		t.Fatalf("test pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	e, err := New(ctx, model, store, opts...)
	if err != nil {
		t.Fatal(err)
	}
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	return e, store, owner
}

// TestDefinitionCanonicalDomainMatchesEngine pins the domain string shared by
// the Ed25519 signing message (engine DefinitionMessage) and the WebAuthn
// canonical (identity.BuildDefinitionCanonical). The two live in different
// packages, so drift would silently split the signing ceremony — this test is
// the coupling contract.
func TestDefinitionCanonicalDomainMatchesEngine(t *testing.T) {
	msg := string(DefinitionMessage("proposal-1", "granted"))
	if !strings.HasPrefix(msg, identity.DefinitionCanonicalDomain+"|") {
		t.Fatalf("DefinitionMessage %q must start with DefinitionCanonicalDomain %q", msg, identity.DefinitionCanonicalDomain)
	}
}

// TestProposalRequireHumanSignatureRejectsTokenDecision: under
// --require-human-signature a human without a WebAuthn assertion cannot decide
// a definition change — the same gate workflow Decide enforces (LLD-02 §5),
// now applied to the model-change path (product mission: signed changes).
func TestProposalRequireHumanSignatureRejectsTokenDecision(t *testing.T) {
	e, _, owner := proposalSigEngine(t, WithRequireHumanSignature())
	p, dslErrs, err := e.ProposeChange(ctx, admin, map[string]string{"test.dsl": testPack + "\nentity Boom:\n    x: int\n"},
		e.DefVersion(), "add Boom", basis, "idem-sig-0001")
	if err != nil || len(dslErrs) > 0 {
		t.Fatalf("propose: err=%v dslErrs=%v", err, dslErrs)
	}
	_, err = e.DecideProposal(ctx, owner, p.ID, true, nil, basis)
	wantCode(t, err, CodeSignatureRequired)
	got, gerr := e.GetProposal(p.ID)
	if gerr != nil {
		t.Fatal(gerr)
	}
	if got.Status != ProposalPending {
		t.Fatalf("a rejected token decision must leave the proposal pending, got %s", got.Status)
	}
}

// TestProposalHumanSignatureJournaled: a pre-verified WebAuthn assertion is
// accepted and journaled additively on the definition decision event, so an
// offline verifier can redo every check (LLD-02 §5 shape), and replay folds
// the same journal deterministically (I-5).
func TestProposalHumanSignatureJournaled(t *testing.T) {
	e, store, owner := proposalSigEngine(t, WithRequireHumanSignature())
	p, dslErrs, err := e.ProposeChange(ctx, admin, map[string]string{"test.dsl": testPack + "\nentity Boom:\n    x: int\n"},
		e.DefVersion(), "add Boom", basis, "idem-sig-0002")
	if err != nil || len(dslErrs) > 0 {
		t.Fatalf("propose: err=%v dslErrs=%v", err, dslErrs)
	}
	raw := json.RawMessage(`{"credential_id":"abc","authenticator_data":"YWJj","client_data_json":"e30=","signature":"c2ln"}`)
	if _, err := e.DecideProposal(ctx, owner, p.ID, true, nil, basis,
		WithProposalHumanSignature(raw, "example.com", 42)); err != nil {
		t.Fatalf("decide with human signature: %v", err)
	}
	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range events {
		if ev.Kind != eventstore.DefinitionApproved {
			continue
		}
		found = true
		var pl proposalPayload
		if err := json.Unmarshal([]byte(ev.Payload), &pl); err != nil {
			t.Fatalf("decision payload must unmarshal: %v", err)
		}
		if string(pl.HumanSignature) != string(raw) {
			t.Fatalf("human_signature not journaled: %q", pl.HumanSignature)
		}
		if pl.RPID != "example.com" || pl.HeadSeq != 42 {
			t.Fatalf("rp_id/head_seq not journaled: %q %d", pl.RPID, pl.HeadSeq)
		}
	}
	if !found {
		t.Fatal("no DefinitionApproved event found in journal")
	}
	model, errs := dsl.Compile(map[string]string{"test.dsl": testPack})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	e2, err := New(ctx, model, store)
	if err != nil {
		t.Fatalf("replay after signed decision: %v", err)
	}
	if e2.DefVersion() != e.DefVersion() {
		t.Fatalf("replay def_version %d != live %d", e2.DefVersion(), e.DefVersion())
	}
}

// TestProposalRawSignatureStillVerified: the pre-LLD-02 raw Ed25519 path keeps
// working — a provided signature is verified over DefinitionMessage, an
// invalid one is refused (I-1 backward compatibility).
func TestProposalRawSignatureStillVerified(t *testing.T) {
	var verified string
	e, _, owner := proposalSigEngine(t,
		WithVerifier(func(_ context.Context, _ string, msg, sig []byte) error {
			verified = string(msg) + "/" + string(sig)
			if string(sig) != "good" {
				return &Err{Code: CodePermissionDenied, Message: "bad signature"}
			}
			return nil
		}))
	p, dslErrs, err := e.ProposeChange(ctx, admin, map[string]string{"test.dsl": testPack + "\nentity Boom:\n    x: int\n"},
		e.DefVersion(), "add Boom", basis, "idem-sig-0003")
	if err != nil || len(dslErrs) > 0 {
		t.Fatalf("propose: err=%v dslErrs=%v", err, dslErrs)
	}
	if _, err := e.DecideProposal(ctx, owner, p.ID, true, []byte("bad"), basis); err == nil {
		t.Fatal("an invalid raw signature must be refused")
	}
	if verified != string(DefinitionMessage(p.ID, "granted"))+"/bad" {
		t.Fatalf("verifier must see DefinitionMessage, got %q", verified)
	}
}
