package mcp

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
	"github.com/avangerus/kalita/internal/pii"
)

// LLD-16 §4 acceptance: erase-subject is the governed, journaled shred — gated
// on an explicit `erase [subject]` grant, idempotent, and it proves itself with
// a subject.erased event while the subject's data reads back as a tombstone.
func TestEraseSubjectGoverned(t *testing.T) {
	model, errs := dsl.Compile(map[string]string{"p.dsl": `
pack pii
version 0.1.0

entity Contract:
    counterparty: ref[core.User] required
    terms: text pii subject=counterparty

roles:
    Lawyer agent
    DataAdmin

permissions:
    Lawyer:
        create [Contract]
        read [Contract]
        update [Contract]
        deny [delete *]
    DataAdmin:
        read [Contract]
        erase [subject]
`})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}

	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	ctx := context.Background()
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	lawyerToken, err := reg.RegisterWithToken(ctx, registrar, "lawyer-1", eventstore.ActorAgent, "Lawyer", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	adminToken, err := reg.RegisterWithToken(ctx, registrar, "da-1", eventstore.ActorAgent, "DataAdmin", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	piiStore, err := pii.NewStore("")
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(ctx, model, store, engine.WithPIIStore(piiStore))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()

	basis := map[string]any{"type": "human", "id": "root"}

	rec, isErr := call(t, srv.URL, lawyerToken, "create_record", map[string]any{
		"entity": "Contract", "basis": basis,
		"values": map[string]any{"counterparty": "user-42", "terms": "secret terms"},
	})
	if isErr {
		t.Fatalf("create: %v", rec)
	}

	// erase without the grant -> denied (never inferred from read/create)
	out, isErr := call(t, srv.URL, lawyerToken, "erase_subject", map[string]any{
		"subject_id": "user-42", "reason": "GDPR request", "basis": basis,
	})
	if !isErr || out["code"] != "PERMISSION_DENIED" {
		t.Fatalf("erase without grant must be denied: %v (isErr=%v)", out, isErr)
	}

	// erase with the grant -> succeeds
	out, isErr = call(t, srv.URL, adminToken, "erase_subject", map[string]any{
		"subject_id": "user-42", "reason": "GDPR request", "basis": basis,
	})
	if isErr {
		t.Fatalf("erase with grant must succeed: %v", out)
	}

	// subject.erased is journaled (the proof), and carries no personal data
	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var proof string
	for _, ev := range events {
		if ev.Kind == eventstore.SubjectErased {
			proof = string(ev.Payload)
		}
	}
	if proof == "" {
		t.Fatal("subject.erased must be journaled")
	}

	// the subject's data now reads as a tombstone
	got, isErr := call(t, srv.URL, lawyerToken, "get_record", map[string]any{"entity": "Contract", "id": rec["id"]})
	if isErr {
		t.Fatalf("get after erase: %v", got)
	}
	vals, _ := got["values"].(map[string]any)
	if vals["terms"] != "[shredded: user-42]" {
		t.Fatalf("after erase, terms must be the tombstone: %v", vals["terms"])
	}

	// idempotent: a second erase is a clean no-op
	if _, isErr := call(t, srv.URL, adminToken, "erase_subject", map[string]any{
		"subject_id": "user-42", "reason": "GDPR request", "basis": basis,
	}); isErr {
		t.Fatalf("second erase must be a no-op: %v", out)
	}
}
