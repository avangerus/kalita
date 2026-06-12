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

// Receivables (дебиторка) with a collections AGENT: an overdue invoice is taken
// from the pool by the agent, which chases the client and logs contact; payment
// settles it, and writing a debt off is a signed human decision. Money roll-ups
// (paid / balance) are computed; the founder's original "Дебиторка" agent, now
// real on a mature platform.
func TestDogfoodFinanceCollections(t *testing.T) {
	ctx := context.Background()
	model, errs := dsl.Compile(packFiles(t, "../../packs/finance"))
	if len(errs) > 0 {
		t.Fatalf("finance must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	acc, _ := reg.RegisterWithToken(ctx, root, "acc-1", eventstore.ActorHuman, "Accountant", nil, nil, nil)
	fin, _ := reg.RegisterWithToken(ctx, root, "fin-1", eventstore.ActorHuman, "FinanceManager", nil, nil, nil)
	col, _ := reg.RegisterWithToken(ctx, root, "col-1", eventstore.ActorAgent, "Collector", nil, nil, nil)
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()
	finActor := eventstore.Actor{Type: eventstore.ActorHuman, ID: "fin-1", Role: "FinanceManager"}
	basis := map[string]any{"type": "human", "id": "acc-1"}
	tb := func(id string) map[string]any { return map[string]any{"type": "task", "id": id} }

	client, _ := call(t, srv.URL, acc, "create_record", map[string]any{
		"entity": "Client", "basis": basis, "values": map[string]any{"name": "Vector LLC", "inn": "7700000000"}})
	mkInvoice := func() string {
		inv, e := call(t, srv.URL, acc, "create_record", map[string]any{
			"entity": "Invoice", "basis": basis,
			"values": map[string]any{"client": client["id"], "amount": 100000, "due_date": "2026-05-01"}})
		if e {
			t.Fatalf("create invoice: %v", inv)
		}
		// issue it; past the due date it auto-flags Overdue and queues a chase task
		res, e := call(t, srv.URL, acc, "act", map[string]any{
			"entity": "Invoice", "id": inv["id"], "action": "issue", "basis": basis})
		if e || res["status"] != "applied" {
			t.Fatalf("issue: %v", res)
		}
		return inv["id"].(string)
	}

	// --- the collections path: agent chases, client pays, accountant settles ---
	invA := mkInvoice()
	gotA, _ := call(t, srv.URL, acc, "get_record", map[string]any{"entity": "Invoice", "id": invA})
	av := gotA["values"].(map[string]any)
	if av["status"] != "Overdue" {
		t.Fatalf("an issued, past-due invoice must auto-flag Overdue, got %v", av["status"])
	}
	if bal, _ := av["balance"].(float64); bal != 100000 {
		t.Errorf("balance = %v, want 100000 (nothing paid yet)", av["balance"])
	}

	// the collections agent finds the chase task in its pool
	mine, _ := call(t, srv.URL, col, "list_my_tasks", map[string]any{})
	tasks := mine["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("the Collector pool should hold 1 chase task, got %d", len(tasks))
	}
	task := tasks[0].(map[string]any)
	if task["action"] != "chase" || task["record_id"] != invA {
		t.Fatalf("task should be chase on the overdue invoice, got %v", task)
	}
	tid := task["id"].(string)
	call(t, srv.URL, col, "take_task", map[string]any{"task_id": tid})
	// the agent logs the contact and posts a reminder — real work, journaled
	if _, e := call(t, srv.URL, col, "update_record", map[string]any{
		"entity": "Invoice", "id": invA, "basis": tb(tid),
		"values": map[string]any{"last_reminder": "2026-06-12T10:00:00Z"}}); e {
		t.Fatal("agent could not log the reminder")
	}
	call(t, srv.URL, col, "comment", map[string]any{
		"entity": "Invoice", "id": invA, "body": "Reminder sent; client promises payment this week.", "basis": tb(tid)})
	// the agent must NOT be able to move the workflow state directly (rejected by
	// the workflow-field guard and the deny rule alike)
	if res, e := call(t, srv.URL, col, "update_record", map[string]any{
		"entity": "Invoice", "id": invA, "basis": tb(tid), "values": map[string]any{"status": "Paid"}}); !e {
		t.Fatalf("the agent must not write the workflow state, got %v", res)
	}
	chaseRes, _ := call(t, srv.URL, col, "act", map[string]any{
		"entity": "Invoice", "id": invA, "action": "chase", "basis": tb(tid)})
	if chaseRes["status"] != "applied" {
		t.Fatalf("chase: %v", chaseRes)
	}
	call(t, srv.URL, col, "complete_task", map[string]any{"task_id": tid, "result": "in collection, reminder sent"})

	// the client pays; the accountant settles
	if _, e := call(t, srv.URL, acc, "create_record", map[string]any{
		"entity": "Payment", "basis": basis,
		"values": map[string]any{"invoice": invA, "amount": 100000, "method": "Bank"}}); e {
		t.Fatal("record payment")
	}
	collectedRes, _ := call(t, srv.URL, acc, "act", map[string]any{
		"entity": "Invoice", "id": invA, "action": "collected", "basis": basis})
	if collectedRes["status"] != "applied" {
		t.Fatalf("collected (balance should be 0): %v", collectedRes)
	}
	paid, _ := call(t, srv.URL, acc, "get_record", map[string]any{"entity": "Invoice", "id": invA})
	pv := paid["values"].(map[string]any)
	if pv["status"] != "Paid" {
		t.Errorf("settled invoice = %v, want Paid", pv["status"])
	}
	if bal, _ := pv["balance"].(float64); bal != 0 {
		t.Errorf("balance after full payment = %v, want 0", pv["balance"])
	}

	// --- the write-off path: a debt written off behind a human signature ---
	invB := mkInvoice()
	// drive it into collection (the agent's chase) then have the manager write it off
	mine2, _ := call(t, srv.URL, col, "list_my_tasks", map[string]any{})
	tid2 := mine2["tasks"].([]any)[0].(map[string]any)["id"].(string)
	call(t, srv.URL, col, "take_task", map[string]any{"task_id": tid2})
	call(t, srv.URL, col, "act", map[string]any{"entity": "Invoice", "id": invB, "action": "chase", "basis": tb(tid2)})
	// the agent cannot write off its own debt — no act grant, and it is HITL
	if res, e := call(t, srv.URL, col, "act", map[string]any{
		"entity": "Invoice", "id": invB, "action": "write_off", "basis": tb(tid2)}); !e {
		t.Fatalf("the agent must not write off a debt, got %v", res)
	}
	// the finance manager writes it off — parked for a signature
	wo := call2ok(t, srv.URL, fin, "act", map[string]any{"entity": "Invoice", "id": invB, "action": "write_off", "basis": basis})
	if wo["status"] != "pending_approval" {
		t.Fatalf("write_off must park for a signature, got %v", wo)
	}
	pend := eng.PendingApprovals("FinanceManager")
	if len(pend) != 1 {
		t.Fatalf("one write-off approval should await the manager, got %d", len(pend))
	}
	if _, err := eng.Decide(ctx, finActor, pend[0].ID, true, nil, &eventstore.Basis{Type: "human", ID: "fin-1"}); err != nil {
		t.Fatalf("manager decision: %v", err)
	}
	woGot, _ := call(t, srv.URL, fin, "get_record", map[string]any{"entity": "Invoice", "id": invB})
	if woGot["values"].(map[string]any)["status"] != "WrittenOff" {
		t.Errorf("written-off invoice = %v, want WrittenOff", woGot["values"])
	}

	// receivables board reflects: one paid, one written off -> nothing outstanding
	dash, _ := call(t, srv.URL, fin, "dashboard", map[string]any{"name": "Receivables"})
	if got := tileValue(t, dash, "Outstanding"); got != 0 {
		t.Errorf("Outstanding = %v, want 0 (one collected, one written off)", got)
	}
}
