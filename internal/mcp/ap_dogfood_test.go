package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// Incoming invoices (AP): the founder's scenario as an executable pack. An
// invoice arrives from "system X" (APClerk), the AP CHECKER AGENT verifies
// contract/amount/requisites and submits, and the approval ladder is
// threshold-based: ≤100k the manager signs, ≤1M the finance director, above
// that the general director. After the signature the agent hands the invoice
// to "system Y" (send_to_payment + InvoiceSentToPayment + webhook egress
// task). Wrong tiers never park, the agent never signs, and a parked approval
// cannot be signed against a changed sum — by construction, all journaled.
//
// Skeptic-review regressions baked in (Cycle-001 REVIEW): boundary amounts at
// exactly 100000/1000000, the park→decide tamper window (deny rows at the pack
// level as defense-in-depth — since Cycle-003 the kernel itself re-checks the
// guard at Decide time, engine/decide_guard_test.go), emitted facts carry the
// right payload, and the hand-off journals a real webhook egress task.
func TestDogfoodAPIncomingInvoice(t *testing.T) {
	ctx := context.Background()
	model, errs := dsl.Compile(packFiles(t, "../../packs/ap"))
	if len(errs) > 0 {
		t.Fatalf("ap must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	clk, _ := reg.RegisterWithToken(ctx, root, "clk-1", eventstore.ActorHuman, "APClerk", nil, nil, nil)
	mgr, _ := reg.RegisterWithToken(ctx, root, "mgr-1", eventstore.ActorHuman, "Manager", nil, nil, nil)
	fin, _ := reg.RegisterWithToken(ctx, root, "fin-1", eventstore.ActorHuman, "FinanceDirector", nil, nil, nil)
	gen, _ := reg.RegisterWithToken(ctx, root, "gen-1", eventstore.ActorHuman, "GeneralDirector", nil, nil, nil)
	chk, _ := reg.RegisterWithToken(ctx, root, "chk-1", eventstore.ActorAgent, "APChecker", nil, nil, nil)
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()
	basis := map[string]any{"type": "human", "id": "clk-1"}
	tb := func(id string) map[string]any { return map[string]any{"type": "task", "id": id} }
	mgrActor := eventstore.Actor{Type: eventstore.ActorHuman, ID: "mgr-1", Role: "Manager"}
	finActor := eventstore.Actor{Type: eventstore.ActorHuman, ID: "fin-1", Role: "FinanceDirector"}
	genActor := eventstore.Actor{Type: eventstore.ActorHuman, ID: "gen-1", Role: "GeneralDirector"}

	vendor, e := call(t, srv.URL, clk, "create_record", map[string]any{
		"entity": "Vendor", "basis": basis,
		"values": map[string]any{"name": "Электронсервис ООО", "inn": "7712345678",
			"kpp": "771401001", "bank_bic": "044525225", "bank_account": "40702810000000012345"}})
	if e {
		t.Fatalf("create vendor: %v", vendor)
	}
	vendorID := vendor["id"].(string)

	// "system X" delivers an invoice: the clerk registers it; the checker
	// task is queued right away (start_checking assignee=agent(APChecker))
	mkInvoice := func(amount float64) string {
		inv, err2 := call(t, srv.URL, clk, "create_record", map[string]any{
			"entity": "VendorInvoice", "basis": basis,
			"values": map[string]any{"vendor": vendorID, "contract_no": "Д-2026-001",
				"amount": amount, "due_date": "2026-07-15"}})
		if err2 {
			t.Fatalf("create invoice: %v", inv)
		}
		return inv["id"].(string)
	}

	// the agent's pool task for one invoice, or fail
	taskFor := func(invID, action string) map[string]any {
		mine, _ := call(t, srv.URL, chk, "list_my_tasks", map[string]any{})
		tasks := mine["tasks"].([]any)
		for _, t0 := range tasks {
			tm := t0.(map[string]any)
			if tm["record_id"] == invID && tm["action"] == action {
				return tm
			}
		}
		t.Fatalf("APChecker pool should hold a %s task for %s, got %v", action, invID, tasks)
		return nil
	}

	// the checker verifies contract/amount/requisites and submits — the agent
	// path between "system X" and the human signature
	checkOK := func(invID string) {
		task := taskFor(invID, "start_checking")
		tid := task["id"].(string)
		call(t, srv.URL, chk, "take_task", map[string]any{"task_id": tid})
		if _, err2 := call(t, srv.URL, chk, "update_record", map[string]any{
			"entity": "VendorInvoice", "id": invID, "basis": tb(tid),
			"values": map[string]any{"contract_ok": true, "amount_ok": true,
				"requisites_ok": true, "check_notes": "Договор сверен; сумма и реквизиты совпадают."}}); err2 {
			t.Fatal("agent could not record check results")
		}
		if res, err2 := call(t, srv.URL, chk, "act", map[string]any{"entity": "VendorInvoice", "id": invID, "action": "start_checking", "basis": tb(tid)}); err2 || res["status"] != "applied" {
			t.Fatalf("start_checking: %v", res)
		}
		if res, err2 := call(t, srv.URL, chk, "act", map[string]any{"entity": "VendorInvoice", "id": invID, "action": "submit_check", "basis": tb(tid)}); err2 || res["status"] != "applied" {
			t.Fatalf("submit_check: %v", res)
		}
		call(t, srv.URL, chk, "complete_task", map[string]any{"task_id": tid, "result": "checked, submitted"})
	}

	// the agent can never sign, touch the amount, or write the workflow state
	agentBounded := func(invID string) {
		if res, err2 := call(t, srv.URL, chk, "act", map[string]any{"entity": "VendorInvoice", "id": invID, "action": "approve_small", "basis": basis}); !err2 {
			t.Fatalf("the checker must not call an approval transition, got %v", res)
		}
		if res, err2 := call(t, srv.URL, chk, "update_record", map[string]any{
			"entity": "VendorInvoice", "id": invID, "basis": basis, "values": map[string]any{"status": "Approved"}}); !err2 {
			t.Fatalf("the agent must not write the workflow state, got %v", res)
		}
		if res, err2 := call(t, srv.URL, chk, "update_record", map[string]any{
			"entity": "VendorInvoice", "id": invID, "basis": basis, "values": map[string]any{"amount": 1}}); !err2 {
			t.Fatalf("the agent must not touch the amount, got %v", res)
		}
	}

	// park one ladder transition and return the approval id
	park := func(tok string, actor eventstore.Actor, action, invID string) string {
		res := call2ok(t, srv.URL, tok, "act", map[string]any{"entity": "VendorInvoice", "id": invID, "action": action, "basis": basis})
		if res["status"] != "pending_approval" {
			t.Fatalf("%s must park for a signature, got %v", action, res)
		}
		pend := eng.PendingApprovals(actor.Role)
		if len(pend) != 1 {
			t.Fatalf("one approval should await %s, got %d", actor.Role, len(pend))
		}
		return pend[0].ID
	}

	decide := func(actor eventstore.Actor, approvalID string) {
		if _, err2 := eng.Decide(ctx, actor, approvalID, true, nil, &eventstore.Basis{Type: "human", ID: actor.ID}); err2 != nil {
			t.Fatalf("%s decision: %v", actor.Role, err2)
		}
	}

	approve := func(tok string, actor eventstore.Actor, action, invID string) {
		decide(actor, park(tok, actor, action, invID))
	}

	// after the signature the agent hands the invoice to "system Y"
	sendToY := func(invID string) {
		task := taskFor(invID, "send_to_payment")
		tid := task["id"].(string)
		call(t, srv.URL, chk, "take_task", map[string]any{"task_id": tid})
		if res, err2 := call(t, srv.URL, chk, "act", map[string]any{"entity": "VendorInvoice", "id": invID, "action": "send_to_payment", "basis": tb(tid)}); err2 || res["status"] != "applied" {
			t.Fatalf("send_to_payment: %v", res)
		}
		call(t, srv.URL, chk, "complete_task", map[string]any{"task_id": tid, "result": "handed to Y"})
	}

	statusOf := func(invID string) string {
		got, _ := call(t, srv.URL, clk, "get_record", map[string]any{"entity": "VendorInvoice", "id": invID})
		return got["values"].(map[string]any)["status"].(string)
	}

	// emitted facts folded from the journal, by event short name
	emittedFacts := func() map[string][]map[string]any {
		out := map[string][]map[string]any{}
		all, err2 := store.All(ctx)
		if err2 != nil {
			t.Fatalf("journal read: %v", err2)
		}
		for _, ev := range all {
			if ev.Kind != eventstore.EventEmitted {
				continue
			}
			var p struct {
				Name    string         `json:"name"`
				Payload map[string]any `json:"payload"`
			}
			if json.Unmarshal(ev.Payload, &p) != nil {
				continue
			}
			name := p.Name
			if i := strings.LastIndex(name, "."); i >= 0 {
				name = name[i+1:]
			}
			out[name] = append(out[name], p.Payload)
		}
		return out
	}

	// --- tier 1: 50k — the MANAGER signs ---
	invA := mkInvoice(50000)
	agentBounded(invA)
	checkOK(invA)
	if st := statusOf(invA); st != "ApprovalPending" {
		t.Fatalf("after the check the invoice must await approval, got %v", st)
	}
	// the wrong tier never parks: a 50k invoice is not the finance director's range
	if res, err2 := call(t, srv.URL, fin, "act", map[string]any{"entity": "VendorInvoice", "id": invA, "action": "approve_mid", "basis": basis}); !err2 {
		t.Fatalf("approve_mid must not apply to a 50k invoice, got %v", res)
	}
	aidA := park(mgr, mgrActor, "approve_small", invA)
	// Skeptic P1 regression: a parked approval cannot be signed against a
	// changed sum — the clerk is locked out of the money fields once submitted
	if res, err2 := call(t, srv.URL, clk, "update_record", map[string]any{
		"entity": "VendorInvoice", "id": invA, "basis": basis, "values": map[string]any{"amount": 2000000}}); !err2 {
		t.Fatalf("the clerk must not edit the amount while an approval is pending, got %v", res)
	}
	decide(mgrActor, aidA)
	if st := statusOf(invA); st != "Approved" {
		t.Errorf("tier-1 invoice = %v, want Approved", st)
	}
	sendToY(invA)
	if st := statusOf(invA); st != "SentToPayment" {
		t.Errorf("tier-1 invoice = %v, want SentToPayment", st)
	}
	// the hand-off journals a real webhook egress task (TaskWebhook)
	var webhookTasks []*engine.Task
	for _, tsk := range eng.Tasks("", engine.TaskOpen) {
		if tsk.Kind == engine.TaskWebhook {
			webhookTasks = append(webhookTasks, tsk)
		}
	}
	if len(webhookTasks) != 1 || webhookTasks[0].RecordID != invA {
		t.Fatalf("the hand-off must journal one webhook egress task for %s, got %+v", invA, webhookTasks)
	}

	// --- tier 2: 500k — the FINANCE DIRECTOR signs; the manager is out of range ---
	invB := mkInvoice(500000)
	checkOK(invB)
	if res, err2 := call(t, srv.URL, mgr, "act", map[string]any{"entity": "VendorInvoice", "id": invB, "action": "approve_small", "basis": basis}); !err2 {
		t.Fatalf("approve_small must not apply to a 500k invoice, got %v", res)
	}
	approve(fin, finActor, "approve_mid", invB)
	sendToY(invB)

	// --- tier 3: 2M — the GENERAL DIRECTOR signs; the finance director is out of range ---
	invC := mkInvoice(2000000)
	checkOK(invC)
	if res, err2 := call(t, srv.URL, fin, "act", map[string]any{"entity": "VendorInvoice", "id": invC, "action": "approve_mid", "basis": basis}); !err2 {
		t.Fatalf("approve_mid must not apply to a 2M invoice, got %v", res)
	}
	approve(gen, genActor, "approve_large", invC)
	sendToY(invC)

	// --- boundary: exactly 100k is still the manager's tier ---
	invF := mkInvoice(100000)
	checkOK(invF)
	if res, err2 := call(t, srv.URL, fin, "act", map[string]any{"entity": "VendorInvoice", "id": invF, "action": "approve_mid", "basis": basis}); !err2 {
		t.Fatalf("approve_mid must not apply at exactly 100k, got %v", res)
	}
	approve(mgr, mgrActor, "approve_small", invF)
	sendToY(invF)

	// --- boundary: exactly 1M is still the finance director's tier ---
	invE := mkInvoice(1000000)
	checkOK(invE)
	if res, err2 := call(t, srv.URL, mgr, "act", map[string]any{"entity": "VendorInvoice", "id": invE, "action": "approve_small", "basis": basis}); !err2 {
		t.Fatalf("approve_small must not apply at exactly 1M, got %v", res)
	}
	approve(fin, finActor, "approve_mid", invE)
	sendToY(invE)

	// --- mismatch path: the agent flags a requisites mismatch, a human reopens ---
	invD := mkInvoice(70000)
	taskD := taskFor(invD, "start_checking")
	tidD := taskD["id"].(string)
	call(t, srv.URL, chk, "take_task", map[string]any{"task_id": tidD})
	if _, err2 := call(t, srv.URL, chk, "update_record", map[string]any{
		"entity": "VendorInvoice", "id": invD, "basis": tb(tidD),
		"values": map[string]any{"requisites_ok": false, "check_notes": "БИК банка не совпадает с договором."}}); err2 {
		t.Fatal("agent could not record the mismatch")
	}
	if res, err2 := call(t, srv.URL, chk, "act", map[string]any{"entity": "VendorInvoice", "id": invD, "action": "start_checking", "basis": tb(tidD)}); err2 || res["status"] != "applied" {
		t.Fatalf("start_checking: %v", res)
	}
	if res, err2 := call(t, srv.URL, chk, "act", map[string]any{"entity": "VendorInvoice", "id": invD, "action": "flag_mismatch", "basis": tb(tidD)}); err2 || res["status"] != "applied" {
		t.Fatalf("flag_mismatch: %v", res)
	}
	call(t, srv.URL, chk, "complete_task", map[string]any{"task_id": tidD, "result": "requisites mismatch flagged"})
	if st := statusOf(invD); st != "Rejected" {
		t.Fatalf("mismatched invoice = %v, want Rejected", st)
	}
	// the general director reopens it for another check (human decision)
	if res, err2 := call(t, srv.URL, gen, "act", map[string]any{"entity": "VendorInvoice", "id": invD, "action": "reopen", "basis": basis}); err2 || res["status"] != "applied" {
		t.Fatalf("reopen: %v", res)
	}
	if st := statusOf(invD); st != "Checking" {
		t.Errorf("reopened invoice = %v, want Checking", st)
	}

	// --- emitted facts: five approvals, five hand-offs, right payloads ---
	facts := emittedFacts()
	if got := len(facts["InvoiceApproved"]); got != 5 {
		t.Errorf("InvoiceApproved facts = %d, want 5", got)
	}
	if got := len(facts["InvoiceSentToPayment"]); got != 5 {
		t.Errorf("InvoiceSentToPayment facts = %d, want 5", got)
	}
	for i, f := range facts["InvoiceApproved"] {
		if f["contract_no"] != "Д-2026-001" {
			t.Errorf("InvoiceApproved[%d] contract_no = %v", i, f["contract_no"])
		}
		if _, ok := f["amount"].(float64); !ok {
			t.Errorf("InvoiceApproved[%d] amount missing, got %v", i, f)
		}
	}

	// the board reflects: five handed to Y, one back in check, nothing parked
	dash, _ := call(t, srv.URL, gen, "dashboard", map[string]any{"name": "APBoard"})
	if got := tileValue(t, dash, "Awaiting approval"); got != 0 {
		t.Errorf("Awaiting approval = %v, want 0", got)
	}
	if got := tileValue(t, dash, "Sent to Y"); got != 5 {
		t.Errorf("Sent to Y = %v, want 5", got)
	}
	if got := tileValue(t, dash, "In check"); got != 1 {
		t.Errorf("In check = %v, want 1", got)
	}
}
