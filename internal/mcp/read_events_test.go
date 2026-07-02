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

// LLD-08 §5 acceptance tests: the MCP tool `read_events`, the `tail [kind]`
// permission verb, and its §3 kind-scoping (v1 serves only event.emitted and
// non-sensitive system kinds, never record.created/updated).

const testTailPack = `pack testtail
version 0.1.0

entity Order:
    ref_no: string

event OrderPlaced "Order placed":
    order_id: string
    amount: money

roles:
    Op agent
    NoGrant agent
    RecordsTailer agent

permissions:
    Op:
        read   [Order]
        create [Order]
        emit   [OrderPlaced]
        tail   [event.emitted]
        deny   [delete *]
    NoGrant:
        read [Order]
        deny [delete *]
    RecordsTailer:
        tail [event.emitted, record.created]
        deny [delete *, create *, update *, act *, emit *, read *]
`

// newTailMCP boots a node from testTailPack with three agent tokens: Op holds
// create/emit/tail[event.emitted]; NoGrant holds ordinary read but no tail
// grant at all; RecordsTailer holds tail on BOTH event.emitted and
// record.created (to prove §3's v1-servable-set cap applies even to an
// explicit grant, not just to an ungranted kind).
func newTailMCP(t *testing.T) (srv *httptest.Server, opToken, noGrantToken, recordsTailerToken string) {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"testtail.dsl": testTailPack})
	if len(errs) > 0 {
		t.Fatalf("compile testTailPack: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	opToken, err := reg.RegisterWithToken(context.Background(), registrar,
		"op-1", eventstore.ActorAgent, "Op", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	noGrantToken, err = reg.RegisterWithToken(context.Background(), registrar,
		"nogrant-1", eventstore.ActorAgent, "NoGrant", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	recordsTailerToken, err = reg.RegisterWithToken(context.Background(), registrar,
		"recordstailer-1", eventstore.ActorAgent, "RecordsTailer", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(context.Background(), model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv = httptest.NewServer(New(eng, reg))
	t.Cleanup(srv.Close)
	return srv, opToken, noGrantToken, recordsTailerToken
}

// eventsOf extracts the `events` array from a read_events tool result.
func eventsOf(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	raw, _ := out["events"].([]any)
	events := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("event entry is not an object: %v", r)
		}
		events = append(events, m)
	}
	return events
}

func emitOrderPlaced(t *testing.T, srv *httptest.Server, token, idemKey string) {
	t.Helper()
	out, isErr := call(t, srv.URL, token, "emit_event", map[string]any{
		"name":            "OrderPlaced",
		"payload":         map[string]any{"order_id": idemKey, "amount": 10},
		"idempotency_key": idemKey,
	})
	if isErr {
		t.Fatalf("emit_event %s must succeed: %v", idemKey, out)
	}
}

// Test 1 (LLD-08 §5): 3 emits in a row; read_events since=0 returns all 3 with
// a correct next_since; since=next_since returns empty; a new emit appears.
func TestReadEventsSince(t *testing.T) {
	srv, opToken, _, _ := newTailMCP(t)

	for _, key := range []string{"since-key-1", "since-key-2", "since-key-3"} {
		emitOrderPlaced(t, srv, opToken, key)
	}

	out, isErr := call(t, srv.URL, opToken, "read_events", map[string]any{"since": 0})
	if isErr {
		t.Fatalf("read_events since=0 must succeed: %v", out)
	}
	events := eventsOf(t, out)
	if len(events) != 3 {
		t.Fatalf("want 3 events, got %d: %v", len(events), events)
	}
	nextSince, _ := out["next_since"].(float64)
	if nextSince == 0 {
		t.Fatalf("next_since must advance past the last event, got %v", out["next_since"])
	}

	out2, isErr := call(t, srv.URL, opToken, "read_events", map[string]any{"since": nextSince})
	if isErr {
		t.Fatalf("read_events since=next_since must succeed: %v", out2)
	}
	if events2 := eventsOf(t, out2); len(events2) != 0 {
		t.Fatalf("read_events since=next_since must return no events yet, got %v", events2)
	}

	emitOrderPlaced(t, srv, opToken, "since-key-4")
	out3, isErr := call(t, srv.URL, opToken, "read_events", map[string]any{"since": nextSince})
	if isErr {
		t.Fatalf("read_events after a new emit must succeed: %v", out3)
	}
	if events3 := eventsOf(t, out3); len(events3) != 1 {
		t.Fatalf("a new emit past the cursor must appear exactly once, got %v", events3)
	}
}

// Test 2 (LLD-08 §5, closes the LLD-07 gap): a free-standing MCP emit_event
// (no enclosing transition, empty subject) is visible in read_events.
func TestReadEventsSeesFreestandingEmit(t *testing.T) {
	srv, opToken, _, _ := newTailMCP(t)

	emitOrderPlaced(t, srv, opToken, "freestanding-1")

	out, isErr := call(t, srv.URL, opToken, "read_events", map[string]any{"since": 0})
	if isErr {
		t.Fatalf("read_events must succeed: %v", out)
	}
	events := eventsOf(t, out)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d: %v", len(events), events)
	}
	subj, _ := events[0]["subject"].(map[string]any)
	if entity, _ := subj["entity"].(string); entity != "" {
		t.Fatalf("a free-standing emit_event must carry an empty subject, got %v", subj)
	}
	if kind, _ := events[0]["kind"].(string); kind != string(eventstore.EventEmitted) {
		t.Fatalf("want kind event.emitted, got %v", events[0]["kind"])
	}
}

// Test 3 (LLD-08 §5): an actor with no `tail` grant at all is denied —
// PermissionDenied, before the store is even touched.
func TestReadEventsRequiresTailGrant(t *testing.T) {
	srv, opToken, noGrantToken, _ := newTailMCP(t)
	emitOrderPlaced(t, srv, opToken, "requires-grant-1")

	out, isErr := call(t, srv.URL, noGrantToken, "read_events", map[string]any{"since": 0})
	if !isErr || out["code"] != "PERMISSION_DENIED" {
		t.Fatalf("read_events without a tail grant must be PERMISSION_DENIED, got: %v", out)
	}

	// the actor WITH the grant succeeds on the identical call.
	out2, isErr := call(t, srv.URL, opToken, "read_events", map[string]any{"since": 0})
	if isErr {
		t.Fatalf("read_events with tail [event.emitted] must succeed: %v", out2)
	}
}

// Test 4 (LLD-08 §5/§3): `tail [event.emitted]` does not leak record.created —
// even a role explicitly granted `tail [event.emitted, record.created]` gets
// nothing back for record.created, because read_events v1 never serves it
// (the stronger tail_records grant this LLD does not add).
func TestReadEventsKindScoped(t *testing.T) {
	srv, opToken, _, recordsTailerToken := newTailMCP(t)

	// Op creates an Order (record.created) and emits OrderPlaced (event.emitted)
	// — read_events is node-wide, so RecordsTailer (a different actor) sees
	// BOTH kinds in the raw stream if it were allowed to.
	createOut, isErr := call(t, srv.URL, opToken, "create_record", map[string]any{
		"entity": "Order", "values": map[string]any{"ref_no": "R-1"},
		"basis": map[string]any{"type": "human", "id": "op-1"},
	})
	if isErr {
		t.Fatalf("create_record must succeed: %v", createOut)
	}
	emitOrderPlaced(t, srv, opToken, "kind-scoped-1")

	// explicit empty kinds list = "every kind this role may tail" (§1), capped
	// to what v1 can ever serve (§3) — record.created must not appear even
	// though the grant nominally covers it.
	out, isErr := call(t, srv.URL, recordsTailerToken, "read_events", map[string]any{
		"since": 0, "kinds": []string{},
	})
	if isErr {
		t.Fatalf("read_events with an explicit empty kinds list must succeed: %v", out)
	}
	for _, ev := range eventsOf(t, out) {
		if kind, _ := ev["kind"].(string); kind == string(eventstore.RecordCreated) {
			t.Fatalf("read_events v1 must never surface record.created, even under an explicit tail grant: %v", ev)
		}
	}

	// an explicit request for record.created is refused outright, not just
	// silently dropped from an unrelated query.
	denied, isErr := call(t, srv.URL, recordsTailerToken, "read_events", map[string]any{
		"since": 0, "kinds": []string{"record.created"},
	})
	if !isErr || denied["code"] != "PERMISSION_DENIED" {
		t.Fatalf("explicitly requesting record.created must be PERMISSION_DENIED, got: %v", denied)
	}
}

// Test 5 (LLD-08 §5): limit-paginated reads via next_since cover the whole
// stream without gaps or duplicates.
func TestReadEventsPagination(t *testing.T) {
	srv, opToken, _, _ := newTailMCP(t)
	const n = 7
	for i := 0; i < n; i++ {
		emitOrderPlaced(t, srv, opToken, "pagination-key-"+string(rune('a'+i)))
	}

	seen := map[float64]bool{}
	var order []float64
	since := float64(0)
	for pages := 0; pages < 20; pages++ { // hard cap so a stuck cursor fails fast, not hangs
		out, isErr := call(t, srv.URL, opToken, "read_events", map[string]any{"since": since, "limit": 3})
		if isErr {
			t.Fatalf("read_events page must succeed: %v", out)
		}
		events := eventsOf(t, out)
		nextSince, _ := out["next_since"].(float64)
		t.Logf("page %d: since=%v got=%d next_since=%v", pages, since, len(events), nextSince)
		for _, ev := range events {
			seq, _ := ev["seq"].(float64)
			if seen[seq] {
				t.Fatalf("seq %v returned twice across pages", seq)
			}
			seen[seq] = true
			order = append(order, seq)
		}
		if len(events) == 0 && nextSince == since {
			break // cursor stopped advancing: the stream is exhausted
		}
		since = nextSince
	}
	if len(seen) != n {
		t.Fatalf("pagination must cover all %d events without gaps, got %d: %v", n, len(seen), order)
	}
	for i := 1; i < len(order); i++ {
		if order[i] <= order[i-1] {
			t.Fatalf("events must come back in increasing seq order, got %v", order)
		}
	}
}
