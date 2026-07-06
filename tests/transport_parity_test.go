// TestTransportParity is the RELEASE-1.0 N8 acceptance test
// (docs/lld/LLD-11-transport-parity.md §2, §5.1): the same operation
// (create/update/act), driven once through REST and once through MCP, must
// journal byte-identical events except for an explicit, documented allow-list
// of fields. It lives in package tests (not internal/api or internal/mcp)
// because it is the one test in the repo that legitimately needs both
// transports wired to the SAME engine/store/registry at once.
package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avangerus/kalita/internal/api"
	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
	"github.com/avangerus/kalita/internal/mcp"
)

// parityDSL is a minimal pack exercising create/update/act. Deliberately
// tiny — an unconditional transition, no approval, no automation — so the
// harness below tests transport parity, not unrelated engine behavior.
// Modeled after internal/engine/diff_test.go's inline Ticket fixture.
const parityDSL = `pack paritytest
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1
entity Widget:
    name:   string required
    status: enum[New, Done] default=New
workflow Widget on status:
    New -> Done: finish
roles:
    Operator
permissions:
    Operator:
        full [Widget]
        act  [finish]
`

// parityHarness wires one engine/store/registry behind BOTH a REST server
// (internal/api) and an MCP server (internal/mcp), authenticated with the
// SAME bearer token — so in this test the "actor" field of every produced
// event is in fact identical across transports too. It is still excluded
// from the comparison below (see normalizeEvent) because the LLD-11 contract
// treats actor as allowed to differ in general — REST and MCP resolve
// identity through independent code paths (internal/transport.ActorFromInfo
// is what keeps that mapping itself from drifting; this test cannot see
// that seam because both calls happen to authenticate as the same actor).
type parityHarness struct {
	t        *testing.T
	eng      *engine.Engine
	ctx      context.Context
	actor    eventstore.Actor
	restBase string
	mcpBase  string
	token    string
}

func newParityHarness(t *testing.T) *parityHarness {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"paritytest.dsl": parityDSL})
	if len(errs) > 0 {
		t.Fatalf("parity pack must compile: %v", errs[0])
	}
	ctx := context.Background()
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	token, err := reg.RegisterWithToken(ctx, registrar, "op-1", eventstore.ActorAgent, "Operator", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}

	restSrv := httptest.NewServer(api.Secure(api.New(eng, reg)))
	t.Cleanup(restSrv.Close)
	mcpSrv := httptest.NewServer(mcp.New(eng, reg))
	t.Cleanup(mcpSrv.Close)

	return &parityHarness{
		t: t, eng: eng, ctx: ctx,
		actor:    eventstore.Actor{Type: eventstore.ActorAgent, ID: "op-1", Role: "Operator"},
		restBase: restSrv.URL, mcpBase: mcpSrv.URL, token: token,
	}
}

// restDo performs one authenticated REST call and decodes the JSON body.
func (h *parityHarness) restDo(method, path string, body any) map[string]any {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			h.t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, h.restBase+path, &buf)
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode >= 300 {
		h.t.Fatalf("REST %s %s: status %d, body %v", method, path, resp.StatusCode, out)
	}
	return out
}

// mcpCall performs one authenticated MCP tools/call and decodes the tool's
// JSON result, failing the test on any protocol or tool error.
func (h *parityHarness) mcpCall(name string, args map[string]any) map[string]any {
	h.t.Helper()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	req, err := http.NewRequest("POST", h.mcpBase+"/", bytes.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var rpcResp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		h.t.Fatal(err)
	}
	if rpcResp.Error != nil {
		h.t.Fatalf("mcp %s: protocol error: %s", name, rpcResp.Error.Message)
	}
	if len(rpcResp.Result.Content) == 0 {
		h.t.Fatalf("mcp %s: empty result content", name)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(rpcResp.Result.Content[0].Text), &out); err != nil {
		h.t.Fatal(err)
	}
	if rpcResp.Result.IsError {
		h.t.Fatalf("mcp %s: tool error: %v", name, out)
	}
	return out
}

// lastEvent returns the most recent journal event for entity/id.
func (h *parityHarness) lastEvent(entity, id string) *eventstore.Event {
	h.t.Helper()
	events, err := h.eng.Journal(h.ctx, h.actor, entity, id, 10)
	if err != nil {
		h.t.Fatal(err)
	}
	if len(events) == 0 {
		h.t.Fatalf("no journal events for %s/%s", entity, id)
	}
	return events[len(events)-1]
}

// normalizeEvent renders ev as canonical JSON with the LLD-11 §2 allow-list
// of fields removed — everything else must match byte-for-byte between the
// REST-triggered and MCP-triggered events for the parity assertions below to
// pass. Each removed field is commented with WHY it is allowed to differ:
//
//   - actor: the contract's own stated exception — REST and MCP resolve
//     identity via independent request paths (now both routed through
//     internal/transport.ActorFromInfo, but that is a shared implementation,
//     not a shared VALUE requirement across arbitrary callers).
//   - ts: wall-clock time of the Append call; inherently non-deterministic
//     per call, regardless of transport.
//   - event_id, seq, prev_hash, hash, signature: chain-position artifacts —
//     a function of where in the journal (and after which prior events) the
//     append landed. Two REST-only calls would differ here exactly as much
//     as a REST call and an MCP call do; this is not a transport property.
//   - subject.record_id: engine.Create assigns a fresh uuid.NewV7() per call
//     (internal/engine/records.go) independent of transport — comparing
//     generated ids would never pass even for two calls on the same transport.
//   - idempotency_key: client-supplied. The calls below deliberately use
//     DIFFERENT literal keys for the REST and MCP side of each comparison so
//     that the second call is never mistaken for an idempotent retry of the
//     first (internal/engine's idemKey short-circuit is keyed globally, not
//     per record) — the value differing here is a test-harness necessity,
//     not evidence that the two transports treat idempotency differently.
func normalizeEvent(t *testing.T, ev *eventstore.Event) string {
	t.Helper()
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "event_id")
	delete(m, "seq")
	delete(m, "prev_hash")
	delete(m, "hash")
	delete(m, "signature")
	delete(m, "ts")
	delete(m, "actor")
	delete(m, "idempotency_key")
	if subj, ok := m["subject"].(map[string]any); ok {
		delete(subj, "record_id")
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestTransportParity is the RELEASE-1.0 N8 / LLD-11 §5 test 1 (main gate).
func TestTransportParity(t *testing.T) {
	h := newParityHarness(t)
	basis := map[string]any{"type": "human", "id": "op-1"}

	// --- create_record: REST vs MCP, on two fresh records ------------------
	restCreated := h.restDo("POST", "/api/records/Widget", map[string]any{
		"values": map[string]any{"name": "A"}, "basis": basis, "idempotency_key": "parity-create-rest",
	})
	restID, _ := restCreated["id"].(string)
	if restID == "" {
		t.Fatalf("REST create returned no id: %v", restCreated)
	}
	mcpCreated := h.mcpCall("create_record", map[string]any{
		"entity": "Widget", "values": map[string]any{"name": "A"}, "basis": basis, "idempotency_key": "parity-create-mcp",
	})
	mcpID, _ := mcpCreated["id"].(string)
	if mcpID == "" {
		t.Fatalf("MCP create_record returned no id: %v", mcpCreated)
	}
	restCreateEv := h.lastEvent("Widget", restID)
	mcpCreateEv := h.lastEvent("Widget", mcpID)
	if got, want := normalizeEvent(t, mcpCreateEv), normalizeEvent(t, restCreateEv); got != want {
		t.Errorf("create parity: MCP create_record vs REST POST /api/records diverge\nREST: %s\nMCP:  %s", want, got)
	}

	// --- update_record: apply each transport to the OTHER transport's
	// record, so the comparison isolates which transport performed the
	// UPDATE rather than which created the record. --------------------------
	h.mcpCall("update_record", map[string]any{ // MCP updates the REST-created record
		"entity": "Widget", "id": restID, "values": map[string]any{"name": "B"}, "basis": basis, "idempotency_key": "parity-update-mcp",
	})
	h.restDo("PATCH", "/api/records/Widget/"+mcpID, map[string]any{ // REST updates the MCP-created record
		"values": map[string]any{"name": "B"}, "basis": basis, "idempotency_key": "parity-update-rest",
	})
	updateViaMCP := h.lastEvent("Widget", restID)
	updateViaREST := h.lastEvent("Widget", mcpID)
	if got, want := normalizeEvent(t, updateViaMCP), normalizeEvent(t, updateViaREST); got != want {
		t.Errorf("update parity: update_record (MCP) vs PATCH /api/records (REST) diverge\nREST: %s\nMCP:  %s", want, got)
	}

	// --- act: same cross-wiring as update -----------------------------------
	h.mcpCall("act", map[string]any{ // MCP acts on the REST-created record
		"entity": "Widget", "id": restID, "action": "finish", "basis": basis, "idempotency_key": "parity-act-mcp",
	})
	h.restDo("POST", "/api/records/Widget/"+mcpID+"/act", map[string]any{ // REST acts on the MCP-created record
		"action": "finish", "basis": basis, "idempotency_key": "parity-act-rest",
	})
	actViaMCP := h.lastEvent("Widget", restID)
	actViaREST := h.lastEvent("Widget", mcpID)
	if got, want := normalizeEvent(t, actViaMCP), normalizeEvent(t, actViaREST); got != want {
		t.Errorf("act parity: act (MCP) vs POST /act (REST) diverge\nREST: %s\nMCP:  %s", want, got)
	}
}
