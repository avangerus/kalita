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

// The opus magnum, end to end: a human files an issue, an AGENT takes it from
// the pool over MCP, does the work (writes a result and a progress report) and
// submits, and the human accepts the output behind a signature. No ERP framework
// or tracker does this natively — agents as audited, human-supervised employees.
func TestDogfoodDevTrackAgentLoop(t *testing.T) {
	ctx := context.Background()
	model, errs := dsl.Compile(packFiles(t, "../../packs/devtrack"))
	if len(errs) > 0 {
		t.Fatalf("devtrack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	leadTok, _ := reg.RegisterWithToken(ctx, root, "lead-1", eventstore.ActorHuman, "Lead", nil, nil, nil)
	engTok, _ := reg.RegisterWithToken(ctx, root, "eng-1", eventstore.ActorAgent, "Engineer", nil, nil, nil)
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()
	lead := eventstore.Actor{Type: eventstore.ActorHuman, ID: "lead-1", Role: "Lead"}
	basis := map[string]any{"type": "human", "id": "lead-1"}
	taskBasis := func(id string) map[string]any { return map[string]any{"type": "task", "id": id} }

	// (1) the human files an issue — entering Backlog queues it for the agent pool
	iss, e := call(t, srv.URL, leadTok, "create_record", map[string]any{
		"entity": "Issue", "basis": basis,
		"values": map[string]any{"title": "Add a health-check endpoint", "priority": "High"}})
	if e {
		t.Fatalf("file issue: %v", iss)
	}
	issID := iss["id"].(string)

	// (2) the agent finds its task in the pool
	mine, _ := call(t, srv.URL, engTok, "list_my_tasks", map[string]any{})
	tasks := mine["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("the Engineer pool should hold 1 pick_up task, got %d", len(tasks))
	}
	task := tasks[0].(map[string]any)
	taskID := task["id"].(string)
	if task["action"] != "pick_up" || task["record_id"] != issID {
		t.Fatalf("task should be pick_up on the issue, got %v", task)
	}

	// (3) the agent takes it, does the work, and submits — all over MCP
	if _, e := call(t, srv.URL, engTok, "take_task", map[string]any{"task_id": taskID}); e {
		t.Fatal("take_task")
	}
	agentAct := func(action string) map[string]any {
		res, e := call(t, srv.URL, engTok, "act", map[string]any{
			"entity": "Issue", "id": issID, "action": action, "basis": taskBasis(taskID)})
		if e {
			t.Fatalf("agent act %s: %v", action, res)
		}
		return res
	}
	agentAct("pick_up") // Backlog -> InProgress
	// the agent writes its actual output and a human-readable report
	if _, e := call(t, srv.URL, engTok, "update_record", map[string]any{
		"entity": "Issue", "id": issID, "basis": taskBasis(taskID),
		"values": map[string]any{"result": "Added GET /healthz returning 200; covered by a test."}}); e {
		t.Fatal("agent could not write its result")
	}
	call(t, srv.URL, engTok, "comment", map[string]any{
		"entity": "Issue", "id": issID, "body": "Done. Endpoint added and tested.", "basis": taskBasis(taskID)})
	agentAct("submit") // InProgress -> Review
	if _, e := call(t, srv.URL, engTok, "complete_task", map[string]any{
		"task_id": taskID, "result": "health-check endpoint added"}); e {
		t.Fatal("complete_task")
	}

	// (4) the agent must NOT be able to accept its own work (HITL gate)
	if res, e := call(t, srv.URL, engTok, "act", map[string]any{
		"entity": "Issue", "id": issID, "action": "accept", "basis": taskBasis(taskID)}); !e {
		t.Fatalf("the agent must not accept its own output, got %v", res)
	}

	// (5) the human reviews and accepts — a signed, human-in-the-loop decision
	res := call2ok(t, srv.URL, leadTok, "act", map[string]any{
		"entity": "Issue", "id": issID, "action": "accept", "basis": basis})
	if res["status"] != "pending_approval" {
		t.Fatalf("accept must park for the lead's signature, got %v", res)
	}
	pend := eng.PendingApprovals("Lead")
	if len(pend) != 1 {
		t.Fatalf("one approval should await the Lead, got %d", len(pend))
	}
	if _, err := eng.Decide(ctx, lead, pend[0].ID, true, nil, &eventstore.Basis{Type: "human", ID: "lead-1"}); err != nil {
		t.Fatalf("lead decision: %v", err)
	}

	// the issue is Done, carrying the agent's result — the loop closed
	got, _ := call(t, srv.URL, leadTok, "get_record", map[string]any{"entity": "Issue", "id": issID})
	gv := got["values"].(map[string]any)
	if gv["status"] != "Done" {
		t.Errorf("issue status = %v, want Done", gv["status"])
	}
	if gv["result"] == nil || gv["result"] == "" {
		t.Errorf("the agent's result must persist, got %v", gv["result"])
	}
}
