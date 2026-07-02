package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// PR-5 acceptance tests (LLD-01 §4.2 / §5, §7 tests 5 and 6): the MCP tool
// `emit_event`, the `emit [Name]` permission verb, and mandatory MCP
// idempotency.

// testEmitPack declares one event (OrderPlaced) and two agent roles: Emitter
// holds `emit [OrderPlaced]`, Watcher does not — the minimal fixture for the
// permission-denied and idempotent-retry acceptance tests.
const testEmitPack = `pack testemit
version 0.1.0

entity Order:
    ref_no: string

event OrderPlaced "Order placed":
    order_id: string
    amount: money

roles:
    Emitter agent
    Watcher agent

permissions:
    Emitter:
        read [Order]
        emit [OrderPlaced]
        deny [delete *]
    Watcher:
        read [Order]
        deny [delete *]
`

// newEmitMCP boots a node from testEmitPack with two agent tokens: one under
// the Emitter role (holds the emit grant) and one under Watcher (does not).
// It returns the engine directly too, so tests can inspect the journal
// (EmittedEvents) beyond what a single record's read_journal would show — an
// MCP-level emit_event has no record subject to hang a journal query off of.
func newEmitMCP(t *testing.T) (*httptest.Server, *engine.Engine, string, string) {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"testemit.dsl": testEmitPack})
	if len(errs) > 0 {
		t.Fatalf("compile testEmitPack: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	emitterToken, err := reg.RegisterWithToken(context.Background(), registrar,
		"emitter-1", eventstore.ActorAgent, "Emitter", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	watcherToken, err := reg.RegisterWithToken(context.Background(), registrar,
		"watcher-1", eventstore.ActorAgent, "Watcher", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(context.Background(), model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	t.Cleanup(srv.Close)
	return srv, eng, emitterToken, watcherToken
}

// callRaw is call() without the idempotency_key auto-fill (server_test.go) —
// used where a test deliberately wants a mutating call WITHOUT the key.
func callRaw(t *testing.T, url, token, tool string, args any) (map[string]any, bool) {
	t.Helper()
	res := rpc(t, url, token, "tools/call", map[string]any{"name": tool, "arguments": args})
	content := res["content"].([]any)[0].(map[string]any)["text"].(string)
	isErr, _ := res["isError"].(bool)
	var decoded map[string]any
	_ = json.Unmarshal([]byte(content), &decoded)
	return decoded, isErr
}

// Test 5 (LLD-01 §7): emit_event without the `emit [Name]` grant is denied,
// and the journal carries NO event.emitted for that call.
func TestEmitPermissionDenied(t *testing.T) {
	srv, eng, _, watcherToken := newEmitMCP(t)

	out, isErr := call(t, srv.URL, watcherToken, "emit_event", map[string]any{
		"name":    "OrderPlaced",
		"payload": map[string]any{"order_id": "o-1", "amount": 100},
	})
	if !isErr || out["code"] != "PERMISSION_DENIED" {
		t.Fatalf("emit_event without an emit grant must be denied: %v", out)
	}
	if rule, _ := out["rule"].(string); rule == "" {
		t.Fatalf("PERMISSION_DENIED must name the rule: %v", out)
	}
	if facts := eng.EmittedEvents(); len(facts) != 0 {
		t.Fatalf("a denied emit_event must journal no event.emitted, got %v", facts)
	}
}

// Test 6 (LLD-01 §7): a repeat emit_event call with the same idempotency_key
// produces exactly one event.emitted, not a duplicate — and does not
// re-dispatch subscribers a second time.
func TestEmitIdempotent(t *testing.T) {
	srv, eng, emitterToken, _ := newEmitMCP(t)
	const key = "emit-retry-same-key-001"
	payload := map[string]any{"order_id": "o-1", "amount": 100}

	out1, isErr := call(t, srv.URL, emitterToken, "emit_event", map[string]any{
		"name": "OrderPlaced", "payload": payload, "idempotency_key": key,
	})
	if isErr {
		t.Fatalf("first emit_event must succeed: %v", out1)
	}
	out2, isErr := call(t, srv.URL, emitterToken, "emit_event", map[string]any{
		"name": "OrderPlaced", "payload": payload, "idempotency_key": key,
	})
	if isErr {
		t.Fatalf("retried emit_event must succeed (idempotent replay, not an error): %v", out2)
	}
	if out1["seq"] != out2["seq"] {
		t.Fatalf("retried emit_event must return the SAME journaled fact: first=%v second=%v", out1, out2)
	}

	facts := eng.EmittedEvents()
	n := 0
	for _, f := range facts {
		if f.Name == "testemit.OrderPlaced" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want exactly 1 event.emitted for the repeated idempotency_key, got %d: %v", n, facts)
	}
}

// Companion to tests 5/6: a fresh idempotency_key on a second, distinct call
// DOES journal a second event.emitted — idempotency dedups retries of the
// SAME attempt, it does not silently collapse distinct emits.
func TestEmitDistinctKeysJournalTwice(t *testing.T) {
	srv, eng, emitterToken, _ := newEmitMCP(t)
	payload := map[string]any{"order_id": "o-1", "amount": 100}

	if _, isErr := call(t, srv.URL, emitterToken, "emit_event", map[string]any{
		"name": "OrderPlaced", "payload": payload, "idempotency_key": "emit-key-aaaaaaa1",
	}); isErr {
		t.Fatal("first emit_event must succeed")
	}
	if _, isErr := call(t, srv.URL, emitterToken, "emit_event", map[string]any{
		"name": "OrderPlaced", "payload": payload, "idempotency_key": "emit-key-bbbbbbb2",
	}); isErr {
		t.Fatal("second emit_event (distinct key) must succeed")
	}
	facts := eng.EmittedEvents()
	n := 0
	for _, f := range facts {
		if f.Name == "testemit.OrderPlaced" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("two distinct idempotency_key calls must journal two events, got %d: %v", n, facts)
	}
}

// Plus one more test (per PR-5 spec): calling a mutating MCP tool without
// idempotency_key is INVALID_PARAMS with a fix_hint — checked on create_record
// (the existing collections harness) so it is independent of the emit fixture.
func TestMCPMutatorRequiresIdempotencyKey(t *testing.T) {
	srv, collectorToken, _ := newMCP(t)
	out, isErr := callRaw(t, srv.URL, collectorToken, "create_record", map[string]any{
		"entity": "Contract", "basis": map[string]any{"type": "human", "id": "anna"},
		"values": map[string]any{"company": "NoKey LLC", "due_date": "2026-05-01", "amount": 1},
	})
	if !isErr || out["code"] != "INVALID_PARAMS" || out["fix_hint"] == "" {
		t.Fatalf("create_record without idempotency_key must be INVALID_PARAMS with a fix_hint: %v", out)
	}
}
