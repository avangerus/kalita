package mcp

import (
	"regexp"
	"testing"
)

// dshNameRe is the public tool-name contract DeepSeek Harness's mcp-client
// enforces before it normalizes a raw MCP name: [A-Za-z0-9_-]{1,64}. Kalita's
// raw names are already strict snake_case, so DSH normalization is the identity
// mapping (no hash suffix, no collision). This test locks that, so a future tool
// name cannot silently break the DSH integration.
var dshNameRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

// dshWorkerTools are the tools the kalita-worker skill's loop depends on. Drop
// or rename one and the DSH agent's loop breaks — fail the build.
var dshWorkerTools = []string{
	"describe_system", "describe_entity", "list_my_tasks", "wait_for_task",
	"take_task", "get_record", "update_record", "create_record", "act",
	"comment", "report_progress", "complete_task", "fail_task", "read_journal",
}

// TestDshClientCompat proves the MCP surface DeepSeek Harness's mcp-client
// relies on: (1) initialize returns protocolVersion/capabilities/serverInfo/
// instructions, (2) tools/list returns the full tool set with DSH-valid names
// and MCP-standard camelCase keys, (3) the worker loop runs end-to-end over
// tools/call with the agent's bearer token.
func TestDshClientCompat(t *testing.T) {
	srv, collectorToken, annaToken := newMCP(t)

	// (1) initialize — the handshake dsh-mcp-client performs on connect.
	init := rpc(t, srv.URL, "", "initialize", map[string]any{})
	if init["protocolVersion"] != protocolVersion {
		t.Fatalf("initialize protocolVersion: got %v, want %s", init["protocolVersion"], protocolVersion)
	}
	for _, k := range []string{"capabilities", "serverInfo", "instructions"} {
		if init[k] == nil {
			t.Fatalf("initialize must return %q for dsh-mcp-client", k)
		}
	}
	if caps, ok := init["capabilities"].(map[string]any); !ok || caps["tools"] == nil {
		t.Fatalf("initialize capabilities.tools missing: %v", init["capabilities"])
	}

	// (2) tools/list — every name must survive DSH normalization unchanged.
	tools := rpc(t, srv.URL, "", "tools/list", nil)["tools"].([]any)
	if len(tools) != len(toolDefs) {
		t.Fatalf("tools/list count %d != toolDefs %d", len(tools), len(toolDefs))
	}
	present := map[string]bool{}
	for _, td := range tools {
		m, _ := td.(map[string]any)
		name, _ := m["name"].(string)
		if name == "" || !dshNameRe.MatchString(name) {
			t.Fatalf("tool name %q is not DSH-valid ([A-Za-z0-9_]{1,64})", name)
		}
		for _, k := range []string{"description", "inputSchema"} {
			if m[k] == nil {
				t.Fatalf("tool %q must expose MCP-standard %q", name, k)
			}
		}
		present[name] = true
	}
	for _, n := range dshWorkerTools {
		if !present[n] {
			t.Fatalf("kalita-worker loop tool %q missing from tools/list", n)
		}
	}

	// (3) the worker loop, over the wire, with the agent's bearer token.
	basis := map[string]any{"type": "human", "id": "anna"}
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

	tasks, isErr := call(t, srv.URL, collectorToken, "list_my_tasks", map[string]any{})
	if isErr {
		t.Fatalf("list_my_tasks: %v", tasks)
	}
	list := tasks["tasks"].([]any)
	if len(list) != 1 {
		t.Fatalf("collector must see exactly 1 task: %v", tasks)
	}
	taskID := list[0].(map[string]any)["id"].(string)

	if _, isErr := call(t, srv.URL, collectorToken, "take_task", map[string]any{"task_id": taskID}); isErr {
		t.Fatal("take_task failed")
	}
	actRes, isErr := call(t, srv.URL, collectorToken, "act", map[string]any{
		"entity": "Debtor", "id": debtorID, "action": "send_claim",
		"basis": map[string]any{"type": "task", "id": taskID},
	})
	if isErr || actRes["status"] != "applied" {
		t.Fatalf("act send_claim: %v", actRes)
	}
	if _, isErr := call(t, srv.URL, collectorToken, "report_progress", map[string]any{
		"task_id": taskID, "note": "claim sent"}); isErr {
		t.Fatal("report_progress failed")
	}
	if _, isErr := call(t, srv.URL, collectorToken, "complete_task", map[string]any{
		"task_id": taskID, "result": "claim sent"}); isErr {
		t.Fatal("complete_task failed")
	}
}
