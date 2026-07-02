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

// TestDogfoodHRLeaveRequestApproval is the regression test for BUGS.md BUG-02
// (P2, approval-boundary / separation of duties): a Manager who is ALSO the
// Employee on a LeaveRequest (a working manager submitting their own leave)
// could submit() then approve() their own request end to end — the
// `requires approval(Manager)` gate never checked WHICH manager. The fix adds
// a `when employee.user != $me` guard on the approve/reject transitions
// (hr.dsl), checked against the actor requesting the transition, before any
// approval is ever parked. This proves both halves of the bug card's blind
// check: self-approval (and self-rejection) is blocked outright, and the
// ordinary path — a manager approving a DIFFERENT employee's request — still
// works end to end.
//
// hr BUG-01 (used_days/remaining_days summing Rejected/Draft requests) is a
// separate, still-open defect — blocked on a kernel gap in aggregate `where`
// clauses (see hr/BUGS.md). This test does not assert on used_days/remaining_days.
func TestDogfoodHRLeaveRequestApproval(t *testing.T) {
	ctx := context.Background()
	model, errs := dsl.Compile(packFiles(t, "../../packs/hr"))
	if len(errs) > 0 {
		t.Fatalf("hr pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	tok := func(id, role string) string {
		tk, err := reg.RegisterWithToken(ctx, root, id, eventstore.ActorHuman, role, nil, nil, nil)
		if err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
		return tk
	}
	mgr := tok("mgr-1", "Manager")

	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()
	mgrBasis := map[string]any{"type": "human", "id": "mgr-1"}

	// the manager provisions two Employee records: their own, and a subordinate's
	// (Employee has no create grant on Employee — only Manager does, via `full`)
	mgrEmp, e := call(t, srv.URL, mgr, "create_record", map[string]any{
		"entity": "Employee", "basis": mgrBasis,
		"values": map[string]any{"full_name": "Manager Self", "user": "mgr-1"}})
	if e {
		t.Fatalf("create manager's own Employee record: %v", mgrEmp)
	}
	mgrEmpID := mgrEmp["id"].(string)
	subEmp, e := call(t, srv.URL, mgr, "create_record", map[string]any{
		"entity": "Employee", "basis": mgrBasis,
		"values": map[string]any{"full_name": "Subordinate", "user": "emp-1"}})
	if e {
		t.Fatalf("create subordinate Employee record: %v", subEmp)
	}
	subEmpID := subEmp["id"].(string)

	// --- happy path: the manager approves a DIFFERENT employee's request ---
	subReq, e := call(t, srv.URL, mgr, "create_record", map[string]any{
		"entity": "LeaveRequest", "basis": mgrBasis,
		"values": map[string]any{"employee": subEmpID, "start_date": "2026-08-03", "days": 3}})
	if e {
		t.Fatalf("create subordinate LeaveRequest: %v", subReq)
	}
	subReqID := subReq["id"].(string)
	if r, e := call(t, srv.URL, mgr, "act", map[string]any{
		"entity": "LeaveRequest", "id": subReqID, "action": "submit", "basis": mgrBasis}); e {
		t.Fatalf("submit subordinate request: %v", r)
	}
	pend, isErr := call(t, srv.URL, mgr, "act", map[string]any{
		"entity": "LeaveRequest", "id": subReqID, "action": "approve", "basis": mgrBasis})
	if isErr || pend["status"] != "pending_approval" {
		t.Fatalf("approving a different employee's request must still park for a signature, got %v (isErr=%v)", pend, isErr)
	}
	appr := eng.PendingApprovals("Manager")
	if len(appr) != 1 {
		t.Fatalf("one approval should be pending, got %d", len(appr))
	}
	mgrActor := eventstore.Actor{Type: eventstore.ActorHuman, ID: "mgr-1", Role: "Manager"}
	if _, err := eng.Decide(ctx, mgrActor, appr[0].ID, true, nil, &eventstore.Basis{Type: "human", ID: "mgr-1"}); err != nil {
		t.Fatalf("manager decision: %v", err)
	}
	got, _ := call(t, srv.URL, mgr, "get_record", map[string]any{"entity": "LeaveRequest", "id": subReqID})
	if got["values"].(map[string]any)["status"] != "Approved" {
		t.Errorf("subordinate's request = %v, want Approved", got["values"])
	}

	// --- BUG-02 regression: the manager cannot self-approve their OWN leave ---
	selfReq, e := call(t, srv.URL, mgr, "create_record", map[string]any{
		"entity": "LeaveRequest", "basis": mgrBasis,
		"values": map[string]any{"employee": mgrEmpID, "start_date": "2026-09-01", "days": 5}})
	if e {
		t.Fatalf("create manager's own LeaveRequest: %v", selfReq)
	}
	selfReqID := selfReq["id"].(string)
	if r, e := call(t, srv.URL, mgr, "act", map[string]any{
		"entity": "LeaveRequest", "id": selfReqID, "action": "submit", "basis": mgrBasis}); e {
		t.Fatalf("submit own request: %v", r)
	}
	self, isErr := call(t, srv.URL, mgr, "act", map[string]any{
		"entity": "LeaveRequest", "id": selfReqID, "action": "approve", "basis": mgrBasis})
	if !isErr || self["code"] != "GUARD_FAILED" {
		t.Fatalf("the manager must not be able to approve their own leave request, got %v (isErr=%v)", self, isErr)
	}
	stillSubmitted, _ := call(t, srv.URL, mgr, "get_record", map[string]any{"entity": "LeaveRequest", "id": selfReqID})
	if stillSubmitted["values"].(map[string]any)["status"] != "Submitted" {
		t.Fatalf("a blocked self-approval must not move the record, got %v", stillSubmitted["values"])
	}
	// the same guard covers reject, not just approve
	selfReject, isErr := call(t, srv.URL, mgr, "act", map[string]any{
		"entity": "LeaveRequest", "id": selfReqID, "action": "reject", "basis": mgrBasis})
	if !isErr || selfReject["code"] != "GUARD_FAILED" {
		t.Fatalf("the manager must not be able to reject their own leave request either, got %v (isErr=%v)", selfReject, isErr)
	}
}
