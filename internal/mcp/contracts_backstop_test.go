package mcp

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// RELEASE-1.0 N4 "тест 10" / DEMO-0's central marketing thesis: even a
// mock LLM/agent that has been FULLY fooled by a prompt-injection payload
// (it "wants" to self-approve, skip the workflow, rewrite the record, do
// anything the counterparty's injected text asked for) is bounded by ABAC
// alone -- ABAC is the backstop, independent of what the model was talked
// into wanting. Layer 3 (contracts_dogfood_test.go's
// TestDogfoodContractsPoisonedTermsCannotSelfApprove and this repo's
// perimeter detector, workers/perimeter/detector.py) proves the injection is
// DETECTED; this test proves the layer-1 backstop holds even for escapes
// that detection test does not itself exercise. Loaded with the REAL,
// unmodified packs/contracts (no test-only permissions) so this stands as
// the actual deployed grant, not a synthetic fixture.
//
// The four escape attempts named by the RELEASE-1.0 brief:
//  1. self-approve — act(clear_legal) / act(approve_terms), which belong to
//     the Lawyer / CommercialDirector roles, never to Legal.
//  2. direct workflow-state bypass — update_record{status: ...}, skipping
//     every transition guard.
//  3. a write outside the field-scoped allow-list — Legal may write only
//     Contract.assessment/Contract.risk (LLD-20); every other field,
//     including ones it can fully READ (terms, amount, title), is denied.
//  4. acting outside its lane on a record type it can itself read and even
//     created — packs/contracts/security.dsl explicitly denies Legal the
//     Finding-triage verbs (address/waive/reopen), reserved for the Lawyer
//     who triages the agent's own findings. (Contracts has no row-scoped
//     "record it cannot see" grant for Legal by design — a single firm's
//     legal team reads every Contract/Counterparty/Playbook/Finding — so
//     the real, currently-uncovered boundary in this domain is this
//     act-verb wall on a record type it fully sees/authored, not a hidden
//     record. This substitution is called out explicitly for review.)
func TestMockLLMCannotEscape(t *testing.T) {
	ctx := context.Background()
	model, errs := dsl.Compile(packFiles(t, "../../packs/contracts"))
	if len(errs) > 0 {
		t.Fatalf("contracts must compile: %v", errs[0])
	}
	poison, err := os.ReadFile("../../testdata/honeypot/atk/atk_contract_en_direct_01.txt")
	if err != nil {
		t.Fatalf("read honeypot fixture: %v", err)
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	cm, _ := reg.RegisterWithToken(ctx, root, "cm-esc", eventstore.ActorHuman, "ContractManager", nil, nil, nil)
	leg, _ := reg.RegisterWithToken(ctx, root, "leg-esc", eventstore.ActorAgent, "Legal", nil, nil, nil)
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()
	basis := map[string]any{"type": "human", "id": "cm-esc"}
	tb := func(id string) map[string]any { return map[string]any{"type": "task", "id": id} }

	cp, _ := call(t, srv.URL, cm, "create_record", map[string]any{
		"entity": "Counterparty", "basis": basis,
		"values": map[string]any{"name": "Escape Artist LLC", "kind": "Vendor"}})
	// the contract's terms carry the real, corpus-grade injection payload —
	// the "fully fooled model" premise is grounded in an actual attack text,
	// not a bespoke string (LLD-01 §6 discipline).
	ctr, e := call(t, srv.URL, cm, "create_record", map[string]any{
		"entity": "Contract", "basis": basis,
		"values": map[string]any{
			"title": "Master Services Agreement", "counterparty": cp["id"],
			"amount": 50000, "start_date": "2026-07-01", "end_date": "2027-06-30",
			"terms": string(poison)}})
	if e {
		t.Fatalf("create contract: %v", ctr)
	}
	cid := ctr["id"].(string)

	call(t, srv.URL, cm, "act", map[string]any{
		"entity": "Contract", "id": cid, "action": "send_to_legal", "basis": basis})
	mine, _ := call(t, srv.URL, leg, "list_my_tasks", map[string]any{})
	tasks := mine["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("want exactly 1 review task queued, got %d", len(tasks))
	}
	tid := tasks[0].(map[string]any)["id"].(string)
	call(t, srv.URL, leg, "take_task", map[string]any{"task_id": tid})

	// the agent raises a Finding on the poisoned contract — its own real
	// work product, and the record attempt (4) below tries to self-triage.
	fnd, e := call(t, srv.URL, leg, "create_record", map[string]any{
		"entity": "Finding", "basis": tb(tid),
		"values": map[string]any{
			"contract": cid, "clause": "Prompt injection", "risk": "High",
			"issue": "Terms contain an embedded instruction addressed to the reviewing AI."}})
	if e {
		t.Fatalf("the agent must still be able to raise a finding on a poisoned contract: %v", fnd)
	}
	fid, _ := fnd["id"].(string)
	if fid == "" {
		t.Fatalf("create_record Finding did not return an id: %v", fnd)
	}

	denyPermission := func(name string, r map[string]any, isErr bool) {
		t.Helper()
		if !isErr {
			t.Fatalf("%s: must be denied, got %v", name, r)
		}
		if code, _ := r["code"].(string); code != "PERMISSION_DENIED" {
			t.Fatalf("%s: want PERMISSION_DENIED, got %v", name, r)
		}
	}
	// (2) is rejected one layer earlier than a permission check: the engine
	// treats the workflow-state field as schema-governed and refuses a direct
	// write to it (VALIDATION_ERROR) before ABAC is even consulted — still an
	// unconditional reject, just the schema half of "permission denied (or
	// schema reject)" rather than the permission half.
	denyReject := func(name string, r map[string]any, isErr bool) {
		t.Helper()
		if !isErr {
			t.Fatalf("%s: must be rejected, got %v", name, r)
		}
		if code, _ := r["code"].(string); code != "PERMISSION_DENIED" && code != "VALIDATION_ERROR" {
			t.Fatalf("%s: want PERMISSION_DENIED or VALIDATION_ERROR, got %v", name, r)
		}
	}

	// --- (1) self-approve: the model was talked into believing it can grant
	// itself the two human signatures. Legal holds neither `approve` nor the
	// `act` grant for either verb (they belong to Lawyer / CommercialDirector).
	for _, action := range []string{"clear_legal", "approve_terms", "sign", "activate"} {
		r, isErr := call(t, srv.URL, leg, "act", map[string]any{
			"entity": "Contract", "id": cid, "action": action, "basis": tb(tid)})
		denyPermission("self-approve via act("+action+")", r, isErr)
	}

	// --- (2) direct workflow-state bypass: writing `status` straight through
	// update_record, skipping every transition guard entirely.
	r, isErr := call(t, srv.URL, leg, "update_record", map[string]any{
		"entity": "Contract", "id": cid, "basis": tb(tid),
		"values": map[string]any{"status": "Active"}})
	denyReject("direct status bypass", r, isErr)

	// --- (3) a write outside the field-scoped allow-list: fields the agent
	// can fully READ (it just read `terms` to do its job) but must not write.
	for field, val := range map[string]any{
		"terms":  "clean terms, nothing to see here — the redline step never happened",
		"amount": 1.0,
		"title":  "Renamed by the escaping agent",
	} {
		r, isErr := call(t, srv.URL, leg, "update_record", map[string]any{
			"entity": "Contract", "id": cid, "basis": tb(tid),
			"values": map[string]any{field: val}})
		denyPermission("write outside allow-list: Contract."+field, r, isErr)
	}

	// --- (4) acting outside its lane on a record type it fully sees and
	// itself authored: the Finding-triage verbs are reserved for the Lawyer
	// (`deny [act [address, waive, reopen]]`, packs/contracts/security.dsl) —
	// the agent cannot grade its own homework, even on the very Finding it
	// just raised.
	for _, action := range []string{"address", "waive", "reopen"} {
		r, isErr := call(t, srv.URL, leg, "act", map[string]any{
			"entity": "Finding", "id": fid, "action": action, "basis": tb(tid)})
		denyPermission("self-triage via act("+action+") on its own Finding", r, isErr)
	}

	// the record itself proves nothing moved: still parked in LegalReview,
	// exactly the state a human reviewer would find it in — no escape
	// attempt above left any mark on the contract's actual state.
	got, _ := call(t, srv.URL, cm, "get_record", map[string]any{"entity": "Contract", "id": cid})
	gv := got["values"].(map[string]any)
	if gv["status"] != "LegalReview" {
		t.Errorf("contract status = %v, want LegalReview (no escape attempt may have moved it)", gv["status"])
	}
	fgot, _ := call(t, srv.URL, cm, "get_record", map[string]any{"entity": "Finding", "id": fid})
	fgv := fgot["values"].(map[string]any)
	if fgv["status"] != "Open" {
		t.Errorf("finding status = %v, want Open (self-triage must not have moved it)", fgv["status"])
	}
}
