package mcp

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// THE MVP acceptance test (MCP-CONTRACT §8, BACKLOG week 8 DoD): an agent over
// bare MCP, starting from an EMPTY node, builds itself a workplace:
// reads the grammar → iterates DSL to green → proposes the pack → a human
// signs → the agent works in the system it built, inside its own boundaries.
func TestAcceptanceAgentBuildsItsWorkplace(t *testing.T) {
	// genesis: empty definition, nothing but the journal
	model, errs := dsl.Compile(map[string]string{})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	ctx := context.Background()
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	builderToken, err := reg.RegisterWithToken(ctx, registrar, "builder-1", eventstore.ActorAgent, "Helper", nil, nil)
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

	// (1) the agent reads the grammar
	g, _ := call(t, srv.URL, builderToken, "get_grammar", map[string]any{})
	example := g["example"].(string)

	// (2) iterates broken DSL to green via fix hints
	broken := strings.Replace(example, "default=New", "default=Missing", 1)
	out, _ := call(t, srv.URL, builderToken, "validate_dsl", map[string]any{
		"files": map[string]string{"ticket.kal": broken}})
	if out["ok"] != true && out["ok"] != false {
		t.Fatal("validate_dsl must answer")
	}
	if out["ok"] == true {
		t.Fatal("broken example must not validate")
	}
	out, _ = call(t, srv.URL, builderToken, "validate_dsl", map[string]any{
		"files": map[string]string{"ticket.kal": example}})
	if out["ok"] != true {
		t.Fatalf("canonical example must validate: %v", out)
	}

	// (3) proposes the pack against the live def_version
	sys, _ := call(t, srv.URL, builderToken, "describe_system", map[string]any{})
	baseDef := sys["def_version"].(float64)
	prop, isErr := call(t, srv.URL, builderToken, "propose_change", map[string]any{
		"files": map[string]string{"ticket.kal": example},
		"base_def_version": baseDef, "description": "ticket tracker pack", "basis": agentBasis,
	})
	if isErr || prop["ok"] != true {
		t.Fatalf("propose: %v", prop)
	}
	proposalID := prop["proposal_id"].(string)
	if len(prop["migration_plan"].([]any)) == 0 {
		t.Fatal("the signer must see a migration plan")
	}

	// the agent cannot rush it: status stays pending until the human acts
	st, _ := call(t, srv.URL, builderToken, "get_proposal", map[string]any{"proposal_id": proposalID})
	if st["status"] != "pending" {
		t.Fatalf("proposal must be pending: %v", st)
	}

	// (4) the human signs (the UI stand-in calls the engine directly)
	if _, err := eng.DecideProposal(ctx, owner, proposalID, true, nil, humanBasis); err != nil {
		t.Fatalf("owner decision: %v", err)
	}
	st, _ = call(t, srv.URL, builderToken, "get_proposal", map[string]any{"proposal_id": proposalID})
	if st["status"] != "applied" {
		t.Fatalf("proposal must be applied: %v", st)
	}

	// (5) the system exists now — the agent creates 5 records in it
	sys, _ = call(t, srv.URL, builderToken, "describe_system", map[string]any{})
	if sys["def_version"].(float64) != baseDef+1 {
		t.Fatal("def_version must bump")
	}
	var firstID string
	for i := 0; i < 5; i++ {
		rec, isErr := call(t, srv.URL, builderToken, "create_record", map[string]any{
			"entity": "Ticket", "basis": agentBasis,
			"values": map[string]any{"title": "ticket"},
		})
		if isErr {
			t.Fatalf("create %d: %v", i, rec)
		}
		if firstID == "" {
			firstID = rec["id"].(string)
		}
	}

	// (6) works its queued task and hits its own deny boundary
	tasks, _ := call(t, srv.URL, builderToken, "list_my_tasks", map[string]any{})
	list := tasks["tasks"].([]any)
	if len(list) != 5 {
		t.Fatalf("5 tickets in New must queue 5 take_ticket tasks, got %d", len(list))
	}
	taskID := list[0].(map[string]any)["id"].(string)
	if _, isErr := call(t, srv.URL, builderToken, "take_task", map[string]any{"task_id": taskID}); isErr {
		t.Fatal("take_task")
	}
	res, isErr := call(t, srv.URL, builderToken, "act", map[string]any{
		"entity": "Ticket", "id": firstID, "action": "take_ticket", "basis": agentBasis})
	if isErr || res["status"] != "applied" {
		t.Fatalf("take_ticket: %v", res)
	}
	// deny boundary: priority is protected from the agent that built the system
	out, isErr = call(t, srv.URL, builderToken, "update_record", map[string]any{
		"entity": "Ticket", "id": firstID, "values": map[string]any{"priority": "High"}, "basis": agentBasis})
	if !isErr || out["code"] != "PERMISSION_DENIED" || out["rule"] == "" {
		t.Fatalf("the builder is still bounded by its deny: %v", out)
	}
	// close requires a human signature — pending, not applied
	res, _ = call(t, srv.URL, builderToken, "act", map[string]any{
		"entity": "Ticket", "id": firstID, "action": "close", "basis": agentBasis})
	if res["status"] != "pending_approval" {
		t.Fatalf("close must park for approval: %v", res)
	}
	if _, isErr := call(t, srv.URL, builderToken, "complete_task", map[string]any{
		"task_id": taskID, "result": "ticket taken"}); isErr {
		t.Fatal("complete_task")
	}

	// finale: the node restarts from the journal alone and remembers everything
	eng2, err := engine.New(ctx, mustCompileEmpty(t), store)
	if err != nil {
		t.Fatal(err)
	}
	lead := eventstore.Actor{Type: eventstore.ActorHuman, ID: "mike", Role: "Lead"}
	rec, err := eng2.Get(ctx, lead, "Ticket", firstID)
	if err != nil {
		t.Fatalf("replayed node must know the Ticket entity and record: %v", err)
	}
	if rec.Values["status"] != "InWork" {
		t.Fatalf("replayed state: %v", rec.Values["status"])
	}
}

func mustCompileEmpty(t *testing.T) *dsl.Model {
	t.Helper()
	m, errs := dsl.Compile(map[string]string{})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	return m
}
