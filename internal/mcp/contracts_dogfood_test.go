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

// Contract management (договорная работа) with multi-party HITL: a legal AGENT
// reviews the terms and writes a clause-level assessment, then the contract
// clears TWO sequential human signatures by TWO different roles — the lawyer
// (legal risk) and the commercial director (money) — before it is signed and
// goes live. Separation of duties is enforced: neither signer can take the
// other's decision, and the agent can sign nothing. The founder's "Договорная
// работа" + "Юристы" agents on a platform that makes the sequence un-skippable.
func TestDogfoodContractsMultiSignature(t *testing.T) {
	ctx := context.Background()
	model, errs := dsl.Compile(packFiles(t, "../../packs/contracts"))
	if len(errs) > 0 {
		t.Fatalf("contracts must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	cm, _ := reg.RegisterWithToken(ctx, root, "cm-1", eventstore.ActorHuman, "ContractManager", nil, nil, nil)
	law, _ := reg.RegisterWithToken(ctx, root, "law-1", eventstore.ActorHuman, "Lawyer", nil, nil, nil)
	cd, _ := reg.RegisterWithToken(ctx, root, "cd-1", eventstore.ActorHuman, "CommercialDirector", nil, nil, nil)
	leg, _ := reg.RegisterWithToken(ctx, root, "leg-1", eventstore.ActorAgent, "Legal", nil, nil, nil)
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()
	lawActor := eventstore.Actor{Type: eventstore.ActorHuman, ID: "law-1", Role: "Lawyer"}
	cdActor := eventstore.Actor{Type: eventstore.ActorHuman, ID: "cd-1", Role: "CommercialDirector"}
	basis := map[string]any{"type": "human", "id": "cm-1"}
	tb := func(id string) map[string]any { return map[string]any{"type": "task", "id": id} }

	cp, _ := call(t, srv.URL, cm, "create_record", map[string]any{
		"entity": "Counterparty", "basis": basis,
		"values": map[string]any{"name": "Orion Integrators GmbH", "kind": "Vendor"}})
	ctr, e := call(t, srv.URL, cm, "create_record", map[string]any{
		"entity": "Contract", "basis": basis,
		"values": map[string]any{
			"title": "Managed Kubernetes support", "counterparty": cp["id"],
			"amount": 4800000, "start_date": "2026-07-01", "end_date": "2027-06-30",
			"terms": "Liability capped at one month of fees. Vendor may subcontract without consent."}})
	if e {
		t.Fatalf("create contract: %v", ctr)
	}
	cid := ctr["id"].(string)

	// the manager sends it to legal; entering LegalReview queues a review task
	res, e := call(t, srv.URL, cm, "act", map[string]any{
		"entity": "Contract", "id": cid, "action": "send_to_legal", "basis": basis})
	if e || res["status"] != "applied" {
		t.Fatalf("send_to_legal: %v", res)
	}

	// --- the legal agent reviews: takes the task, writes its assessment, submits ---
	mine, _ := call(t, srv.URL, leg, "list_my_tasks", map[string]any{})
	tasks := mine["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("the Legal pool should hold 1 review task, got %d", len(tasks))
	}
	task := tasks[0].(map[string]any)
	if task["action"] != "review" || task["record_id"] != cid {
		t.Fatalf("task should be review on the contract, got %v", task)
	}
	tid := task["id"].(string)
	call(t, srv.URL, leg, "take_task", map[string]any{"task_id": tid})
	// the agent writes its clause-level assessment into the result field — real work
	if _, e := call(t, srv.URL, leg, "update_record", map[string]any{
		"entity": "Contract", "id": cid, "basis": tb(tid),
		"values": map[string]any{"assessment": "Liability cap of one month is low; subcontracting without consent is a risk.", "risk": "Medium"}}); e {
		t.Fatal("agent could not write its assessment")
	}
	// the agent must NOT be able to move the workflow state directly
	if r, e := call(t, srv.URL, leg, "update_record", map[string]any{
		"entity": "Contract", "id": cid, "basis": tb(tid), "values": map[string]any{"status": "Approved"}}); !e {
		t.Fatalf("the agent must not write the workflow state, got %v", r)
	}
	rev, _ := call(t, srv.URL, leg, "act", map[string]any{
		"entity": "Contract", "id": cid, "action": "review", "basis": tb(tid)})
	if rev["status"] != "applied" {
		t.Fatalf("review: %v", rev)
	}
	call(t, srv.URL, leg, "complete_task", map[string]any{"task_id": tid, "result": "assessed"})

	// the agent can sign nothing: neither legal nor commercial approvals
	if r, e := call(t, srv.URL, leg, "act", map[string]any{
		"entity": "Contract", "id": cid, "action": "clear_legal", "basis": tb(tid)}); !e {
		t.Fatalf("the agent must not clear legal, got %v", r)
	}

	// --- separation of duties: neither signer can take the other's decision ---
	if r, e := call(t, srv.URL, law, "act", map[string]any{
		"entity": "Contract", "id": cid, "action": "approve_terms", "basis": basis}); !e {
		t.Fatalf("the lawyer must not approve the money terms, got %v", r)
	}
	if r, e := call(t, srv.URL, cd, "act", map[string]any{
		"entity": "Contract", "id": cid, "action": "clear_legal", "basis": basis}); !e {
		t.Fatalf("the commercial director must not clear legal, got %v", r)
	}

	// --- signature 1: the lawyer clears legal (Reviewed -> Negotiation) ---
	s1 := call2ok(t, srv.URL, law, "act", map[string]any{
		"entity": "Contract", "id": cid, "action": "clear_legal", "basis": basis})
	if s1["status"] != "pending_approval" {
		t.Fatalf("clear_legal must park for the lawyer's signature, got %v", s1)
	}
	lp := eng.PendingApprovals("Lawyer")
	if len(lp) != 1 {
		t.Fatalf("one legal approval should await the lawyer, got %d", len(lp))
	}
	if _, err := eng.Decide(ctx, lawActor, lp[0].ID, true, nil, &eventstore.Basis{Type: "human", ID: "law-1"}); err != nil {
		t.Fatalf("lawyer decision: %v", err)
	}

	// --- signature 2: a DIFFERENT human, the commercial director, approves money ---
	s2 := call2ok(t, srv.URL, cd, "act", map[string]any{
		"entity": "Contract", "id": cid, "action": "approve_terms", "basis": basis})
	if s2["status"] != "pending_approval" {
		t.Fatalf("approve_terms must park for the director's signature, got %v", s2)
	}
	dp := eng.PendingApprovals("CommercialDirector")
	if len(dp) != 1 {
		t.Fatalf("one commercial approval should await the director, got %d", len(dp))
	}
	if _, err := eng.Decide(ctx, cdActor, dp[0].ID, true, nil, &eventstore.Basis{Type: "human", ID: "cd-1"}); err != nil {
		t.Fatalf("director decision: %v", err)
	}

	// --- the manager records the signature and activates the live contract ---
	call(t, srv.URL, cm, "act", map[string]any{"entity": "Contract", "id": cid, "action": "sign", "basis": basis})
	if r := call2ok(t, srv.URL, cm, "act", map[string]any{
		"entity": "Contract", "id": cid, "action": "activate", "basis": basis}); r["status"] != "applied" {
		t.Fatalf("activate: %v", r)
	}
	got, _ := call(t, srv.URL, cm, "get_record", map[string]any{"entity": "Contract", "id": cid})
	gv := got["values"].(map[string]any)
	if gv["status"] != "Active" {
		t.Errorf("a fully signed contract = %v, want Active", gv["status"])
	}
	if gv["assessment"] == nil || gv["assessment"] == "" {
		t.Error("the agent's assessment must survive on the record")
	}
	// days_left = days_until(end_date) is forward-looking and positive for a future end
	if dl, _ := gv["days_left"].(float64); dl <= 0 {
		t.Errorf("days_left = %v, want > 0 for an end date in the future", gv["days_left"])
	}

	// the desk shows the contract's value as active
	dash, _ := call(t, srv.URL, cm, "dashboard", map[string]any{"name": "ContractDesk"})
	if got := tileValue(t, dash, "Active value"); got != 4800000 {
		t.Errorf("Active value = %v, want 4800000", got)
	}
}
