package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// Week 6 acceptance (MCP-CONTRACT §8 steps 1,3,4,5,6): an agent over bare
// MCP JSON-RPC reads the system, works records and tasks, hits permission
// walls with structured errors, and iterates DSL through validate_dsl.

func newMCP(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()
	files := map[string]string{}
	for _, name := range []string{"pack.dsl", "collections.dsl"} {
		raw, err := os.ReadFile("../../examples/collections/" + name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(raw)
	}
	model, errs := dsl.Compile(files)
	if len(errs) > 0 {
		t.Fatalf("compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	collectorToken, err := reg.RegisterWithToken(context.Background(), registrar,
		"collector-1", eventstore.ActorAgent, "Collector", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	annaToken, err := reg.RegisterWithToken(context.Background(), registrar,
		"anna", eventstore.ActorHuman, "Accountant", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(context.Background(), model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	t.Cleanup(srv.Close)
	return srv, collectorToken, annaToken
}

// rpc performs a JSON-RPC call, returning the raw result map.
func rpc(t *testing.T, url, token, method string, params any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Result map[string]any `json:"result"`
		Error  *rpcError      `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Error != nil {
		t.Fatalf("rpc error: %+v", out.Error)
	}
	return out.Result
}

// mutatingIdemTools is every MCP tool whose dispatch case now requires
// idempotency_key (PR-5, LLD-01 §5). Kept here (not in tools.go) so the test
// harness's auto-fill below is visibly a TEST convenience, not product policy.
var mutatingIdemTools = map[string]bool{
	"create_record": true, "update_record": true, "act": true,
	"revert_record": true, "rewind_record": true, "take_task": true,
	"report_progress": true, "complete_task": true, "fail_task": true,
	"comment": true, "propose_change": true, "emit_event": true,
}

// call invokes a tool and decodes the single text content block.
//
// PR-5 note: idempotency_key became mandatory on every mutating MCP tool
// (LLD-01 §5) after this whole suite (~80 call sites across acceptance_test.go
// and the *_dogfood_test.go packs) was written against a world where it did
// not exist. Rather than hand-edit every literal args map — which would only
// paste the same placeholder key everywhere and risk accidental idem
// collisions between distinct calls in the same test (e.g. loops that create
// several records) — this single choke point fills in a fresh, unique key
// (uuid) for any mutating-tool call whose args map does not already set one.
// Every test still exercises exactly what it exercised before (permissions,
// workflow, journal content, error codes); the only thing that changes is
// that a previously-nonexistent required field is now populated.
func call(t *testing.T, url, token, tool string, args any) (map[string]any, bool) {
	t.Helper()
	if mutatingIdemTools[tool] {
		if m, ok := args.(map[string]any); ok {
			if _, has := m["idempotency_key"]; !has {
				m["idempotency_key"] = "test-idem-" + uuid.NewString()
			}
		}
	}
	res := rpc(t, url, token, "tools/call", map[string]any{"name": tool, "arguments": args})
	content := res["content"].([]any)[0].(map[string]any)["text"].(string)
	isErr, _ := res["isError"].(bool)
	var decoded map[string]any
	_ = json.Unmarshal([]byte(content), &decoded)
	return decoded, isErr
}

func TestMCPHandshakeAndTools(t *testing.T) {
	srv, _, _ := newMCP(t)
	init := rpc(t, srv.URL, "", "initialize", map[string]any{})
	if init["protocolVersion"] != protocolVersion {
		t.Fatalf("initialize: %v", init)
	}
	tools := rpc(t, srv.URL, "", "tools/list", nil)["tools"].([]any)
	if len(tools) < 15 {
		t.Fatalf("want >=15 tools, got %d", len(tools))
	}
}

func TestMCPAuthRequired(t *testing.T) {
	srv, _, _ := newMCP(t)
	out, isErr := call(t, srv.URL, "", "describe_system", map[string]any{})
	if !isErr || out["code"] != "AUTH_REQUIRED" || out["fix_hint"] == "" {
		t.Fatalf("anonymous call must fail with AUTH_REQUIRED + fix_hint: %v", out)
	}
	out, isErr = call(t, srv.URL, "wrong-token", "describe_system", map[string]any{})
	if !isErr || out["code"] != "AUTH_REQUIRED" {
		t.Fatalf("bad token must fail: %v", out)
	}
}

func TestMCPAgentWorkflow(t *testing.T) {
	srv, collectorToken, annaToken := newMCP(t)
	basis := map[string]any{"type": "human", "id": "anna"}

	// agent orients itself
	sys, _ := call(t, srv.URL, collectorToken, "describe_system", map[string]any{})
	if sys["your_role"] != "Collector" {
		t.Fatalf("describe_system must report the caller's role: %v", sys["your_role"])
	}
	ent, _ := call(t, srv.URL, collectorToken, "describe_entity", map[string]any{"entity": "Debtor"})
	if ent["workflow"] == nil {
		t.Fatal("describe_entity must include the workflow")
	}

	// accountant (via her token) sets up an overdue debtor
	contract, isErr := call(t, srv.URL, annaToken, "create_record", map[string]any{
		"entity": "Contract", "basis": basis,
		"values": map[string]any{"company": "Vector LLC", "due_date": "2026-05-01", "amount": 100000},
	})
	if isErr {
		t.Fatalf("create contract: %v", contract)
	}
	debtor, isErr := call(t, srv.URL, annaToken, "create_record", map[string]any{
		"entity": "Debtor", "basis": basis,
		"values": map[string]any{"company": "Vector LLC", "contract": contract["id"], "debt": 100000},
	})
	if isErr {
		t.Fatalf("create debtor: %v", debtor)
	}
	debtorID := debtor["id"].(string)

	// the agent finds its queued task (entering Overdue queued send_claim)
	tasks, _ := call(t, srv.URL, collectorToken, "list_my_tasks", map[string]any{})
	list := tasks["tasks"].([]any)
	if len(list) != 1 {
		t.Fatalf("collector must see 1 task: %v", tasks)
	}
	taskID := list[0].(map[string]any)["id"].(string)

	if _, isErr := call(t, srv.URL, collectorToken, "take_task", map[string]any{"task_id": taskID}); isErr {
		t.Fatal("take_task failed")
	}
	// permission wall: the agent tries to touch the debt
	out, isErr := call(t, srv.URL, collectorToken, "update_record", map[string]any{
		"entity": "Debtor", "id": debtorID, "basis": map[string]any{"type": "task", "id": taskID},
		"values": map[string]any{"debt": 0},
	})
	if !isErr || out["code"] != "PERMISSION_DENIED" || out["rule"] == "" {
		t.Fatalf("debt update must be denied with the rule named: %v", out)
	}
	// the agent acts within its permissions
	res, isErr := call(t, srv.URL, collectorToken, "act", map[string]any{
		"entity": "Debtor", "id": debtorID, "action": "send_claim",
		"basis": map[string]any{"type": "task", "id": taskID},
	})
	if isErr || res["status"] != "applied" {
		t.Fatalf("send_claim: %v", res)
	}
	if _, isErr := call(t, srv.URL, collectorToken, "report_progress", map[string]any{
		"task_id": taskID, "note": "претензия отправлена"}); isErr {
		t.Fatal("report_progress failed")
	}
	if _, isErr := call(t, srv.URL, collectorToken, "complete_task", map[string]any{
		"task_id": taskID, "result": "claim sent"}); isErr {
		t.Fatal("complete_task failed")
	}

	// journal is readable and carries provenance
	j, isErr := call(t, srv.URL, collectorToken, "read_journal", map[string]any{
		"entity": "Debtor", "id": debtorID})
	if isErr {
		t.Fatalf("read_journal: %v", j)
	}
	if len(j["events"].([]any)) < 3 {
		t.Fatalf("journal must show the record's history: %v", j)
	}
}

func TestMCPValidateDSLLoop(t *testing.T) {
	srv, collectorToken, _ := newMCP(t)

	broken := `entity A:
    s: enum[X, Y] default=Z

roles:
    Bot agent

permissions:
    Bot:
        read [A]
`
	out, _ := call(t, srv.URL, collectorToken, "validate_dsl", map[string]any{
		"files": map[string]string{"a.dsl": broken}})
	if out["ok"] != false {
		t.Fatal("broken DSL must not validate")
	}
	errs := out["errors"].([]any)
	if len(errs) < 2 {
		t.Fatalf("want default-not-in-enum + agent-without-deny, got %v", errs)
	}
	for _, e := range errs {
		if e.(map[string]any)["fix_hint"] == "" {
			t.Fatal("every error must carry fix_hint")
		}
	}

	fixed := strings.ReplaceAll(broken, "default=Z", "default=X") +
		"        deny [delete *, update A.*]\n"
	out, _ = call(t, srv.URL, collectorToken, "validate_dsl", map[string]any{
		"files": map[string]string{"a.dsl": fixed}})
	if out["ok"] != true {
		t.Fatalf("fixed DSL must validate: %v", out)
	}
}

func TestMCPGrammar(t *testing.T) {
	srv, collectorToken, _ := newMCP(t)
	g, _ := call(t, srv.URL, collectorToken, "get_grammar", map[string]any{})
	example := g["example"].(string)
	// the canonical example must itself compile — grammar and compiler in lockstep
	if _, errs := dsl.Compile(map[string]string{"example.dsl": example}); len(errs) > 0 {
		t.Fatalf("grammar example must compile: %v", errs[0])
	}
}
