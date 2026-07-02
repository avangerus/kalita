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

// LLD-06 §2/§3 PK-3, PR-1: the `assistant` pack (Session/Message) is an
// ORDINARY governed pack — no bespoke chat storage, no bespoke ABAC engine.
// This dogfood test is the PR-1 acceptance pair named in the LLD (§3 tests
// 4, 6): a write made under one user's own bearer journals with that user as
// actor (test 4 — proven here structurally the same way chat_runner, LLD-06
// PR-3, will drive it: create_record under the caller's own token, nothing
// else), and Message visibility/undo is governed by the SAME Permission
// Engine and revert mechanism as any other pack (test 6).
func TestAssistantChatHistoryIsGoverned(t *testing.T) {
	ctx := context.Background()
	model, errs := dsl.Compile(packFiles(t, "../../packs/assistant"))
	if len(errs) > 0 {
		t.Fatalf("assistant must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	alice, err := reg.RegisterWithToken(ctx, root, "alice", eventstore.ActorHuman, "User", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	bob, err := reg.RegisterWithToken(ctx, root, "bob", eventstore.ActorHuman, "User", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	adminTok, err := reg.RegisterWithToken(ctx, root, "ops-1", eventstore.ActorHuman, "Admin", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()
	basis := map[string]any{"type": "human", "id": "alice"}

	// --- test 4: a chat write under alice's OWN bearer journals actor=alice ---
	sess, e := call(t, srv.URL, alice, "create_record", map[string]any{
		"entity": "Session", "basis": basis, "values": map[string]any{"title": "Contract review chat"}})
	if e {
		t.Fatalf("alice create Session: %v", sess)
	}
	sid := sess["id"].(string)
	// owner defaults to $me — the creating actor, not a service identity.
	if sess["values"].(map[string]any)["owner"] != "alice" {
		t.Errorf("Session.owner = %v, want alice (default=$me)", sess["values"].(map[string]any)["owner"])
	}

	msg, e := call(t, srv.URL, alice, "create_record", map[string]any{
		"entity": "Message", "basis": basis,
		"values": map[string]any{"session": sid, "role": "user", "body": "проверь этот договор"}})
	if e {
		t.Fatalf("alice create Message: %v", msg)
	}
	mid := msg["id"].(string)

	journ, e := call(t, srv.URL, alice, "read_journal", map[string]any{"entity": "Message", "id": mid})
	if e {
		t.Fatalf("read_journal: %v", journ)
	}
	events, _ := journ["events"].([]any)
	if len(events) == 0 {
		t.Fatal("expected at least one journaled event for the Message")
	}
	first := events[0].(map[string]any)
	actorInfo, _ := first["actor"].(map[string]any)
	if actorInfo["id"] != "alice" || actorInfo["type"] != "human" {
		t.Errorf("Message create event actor = %v, want {id:alice, type:human} — chat writes must journal as the "+
			"HUMAN who sent them, never a service account (LLD-06 §1/§2 pass-through)", actorInfo)
	}

	// --- test 6a: ABAC — Message is visible only to the owner of its Session ---
	bobQuery, e := call(t, srv.URL, bob, "query", map[string]any{"entity": "Message"})
	if e {
		t.Fatalf("bob query Message: %v", bobQuery)
	}
	if recs, _ := bobQuery["records"].([]any); len(recs) != 0 {
		t.Errorf("bob sees %d Message record(s) from alice's session, want 0 (ABAC: where session.owner = $me)", len(recs))
	}
	if _, e := call(t, srv.URL, bob, "get_record", map[string]any{"entity": "Message", "id": mid}); !e {
		t.Fatal("bob must not be able to get_record alice's Message directly by id")
	}

	aliceQuery, e := call(t, srv.URL, alice, "query", map[string]any{"entity": "Message"})
	if e {
		t.Fatalf("alice query Message: %v", aliceQuery)
	}
	if recs, _ := aliceQuery["records"].([]any); len(recs) != 1 {
		t.Errorf("alice sees %d Message record(s) of her own session, want 1", len(recs))
	}

	// bob cannot read alice's Session either (owner-scoped the same way).
	if _, e := call(t, srv.URL, bob, "get_record", map[string]any{"entity": "Session", "id": sid}); !e {
		t.Fatal("bob must not be able to read alice's Session")
	}

	// a plain chat User cannot undo history — revert requires an unconditional
	// `full` grant (internal/engine/revert.go hasFull), which User does not hold.
	if _, e := call(t, srv.URL, alice, "revert_record", map[string]any{"entity": "Message", "id": mid, "basis": basis}); !e {
		t.Fatal("a plain chat User must not be able to revert_record — undo is a supervisory (Admin, full-grant) action")
	}

	// --- test 6b: undo — Message supports revert like any governed record ---
	upd, e := call(t, srv.URL, adminTok, "update_record", map[string]any{
		"entity": "Message", "id": mid, "basis": map[string]any{"type": "human", "id": "ops-1"},
		"values": map[string]any{"body": "EDITED"}})
	if e {
		t.Fatalf("admin update Message: %v", upd)
	}
	rev, e := call(t, srv.URL, adminTok, "revert_record", map[string]any{
		"entity": "Message", "id": mid, "basis": map[string]any{"type": "human", "id": "ops-1"}})
	if e {
		t.Fatalf("admin revert_record Message: %v", rev)
	}
	if rev["values"].(map[string]any)["body"] != "проверь этот договор" {
		t.Errorf("after revert, Message.body = %v, want the original text restored", rev["values"].(map[string]any)["body"])
	}
}
