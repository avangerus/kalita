package mcp

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// TestAuthorComposePackLoop proves the STRUCTURED authoring path — the
// "user describes a business result, the node renders a process" loop. An
// author agent passes a JSON pack spec (entities/fields, a workflow with an
// approval gate, roles, permissions) through compose_pack, proposes the
// rendered DSL, a human signs it, and the resulting system is live and
// HITL-gated. This is the kalita-author skill's kernel surface, over the wire.
func TestAuthorComposePackLoop(t *testing.T) {
	model, errs := dsl.Compile(map[string]string{})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	ctx := context.Background()
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	// One token plays both the author (proposing) and the worker (acting) here;
	// its role "Agent" matches the agent role the pack declares.
	authorToken, err := reg.RegisterWithToken(ctx, registrar, "author-1", eventstore.ActorAgent, "Agent", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()

	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "mike", Role: "Owner"}
	humanBasis := &eventstore.Basis{Type: "human", ID: "mike"}
	agentBasis := map[string]any{"type": "human", "id": "mike"}

	// (1) structured business description → DSL, no grammar prose in context.
	spec := map[string]any{
		"pack": "reqhub",
		"entities": []any{
			map[string]any{"name": "Request", "fields": []any{
				map[string]any{"name": "title", "type": "string", "required": true},
				map[string]any{"name": "status", "type": "enum", "values": []any{"New", "Done"}, "default": "New"},
			}},
		},
		"workflows": []any{
			map[string]any{"entity": "Request", "field": "status", "transitions": []any{
				map[string]any{"from": "New", "to": "Done", "action": "fulfill", "requires_approval": "Manager"},
			}},
		},
		"roles": []any{
			map[string]any{"name": "Manager"},
			map[string]any{"name": "Agent", "agent": true},
		},
		"permissions": []any{
			map[string]any{"role": "Manager", "rules": []any{"full [Request]", "approve [fulfill]"}},
			map[string]any{"role": "Agent", "rules": []any{"read [Request]", "create [Request]", "act [fulfill]", "deny [delete *]"}},
		},
	}
	composed, isErr := call(t, srv.URL, authorToken, "compose_pack", spec)
	if isErr || composed["ok"] != true {
		t.Fatalf("compose_pack must render a valid pack: %v", composed)
	}
	dslText, _ := composed["dsl"].(string)
	if dslText == "" {
		t.Fatal("compose_pack must return rendered DSL")
	}

	// (2) propose against the live def_version — parked for a human.
	sys, _ := call(t, srv.URL, authorToken, "describe_system", map[string]any{})
	baseDef := sys["def_version"].(float64)
	prop, isErr := call(t, srv.URL, authorToken, "propose_change", map[string]any{
		"files": map[string]string{"reqhub.dsl": dslText},
		"base_def_version": baseDef, "description": "request hub", "basis": agentBasis,
	})
	if isErr || prop["ok"] != true {
		t.Fatalf("propose: %v", prop)
	}
	proposalID := prop["proposal_id"].(string)
	st, _ := call(t, srv.URL, authorToken, "get_proposal", map[string]any{"proposal_id": proposalID})
	if st["status"] != "pending" {
		t.Fatalf("proposal must be pending until a human signs: %v", st)
	}

	// (3) the human signs.
	if _, err := eng.DecideProposal(ctx, owner, proposalID, true, nil, humanBasis); err != nil {
		t.Fatalf("owner decision: %v", err)
	}
	st, _ = call(t, srv.URL, authorToken, "get_proposal", map[string]any{"proposal_id": proposalID})
	if st["status"] != "applied" {
		t.Fatalf("proposal must be applied after signature: %v", st)
	}
	sys, _ = call(t, srv.URL, authorToken, "describe_system", map[string]any{})
	if sys["def_version"].(float64) != baseDef+1 {
		t.Fatal("def_version must bump on apply")
	}

	// (4) the system the user described is live: the agent works in it.
	rec, isErr := call(t, srv.URL, authorToken, "create_record", map[string]any{
		"entity": "Request", "basis": agentBasis, "values": map[string]any{"title": "first"},
	})
	if isErr {
		t.Fatalf("create Request: %v", rec)
	}

	// (5) the approval gate the model added (not the user's literal ask) holds:
	// the agent can initiate, but cannot rush the human.
	res, isErr := call(t, srv.URL, authorToken, "act", map[string]any{
		"entity": "Request", "id": rec["id"], "action": "fulfill", "basis": agentBasis,
	})
	if isErr {
		t.Fatalf("agent must be able to initiate fulfill: %v", res)
	}
	if res["status"] != "pending_approval" {
		t.Fatalf("fulfill requires approval(Manager): got %v, want pending_approval", res["status"])
	}
}
