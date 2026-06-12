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

// Dogfood on a REAL spec: the СТП ПУИТ ITSM Service Desk (D:/work/tecius/it_puit
// HLD). We load the functional-core pack, then drive it like operators do —
// create an incident, run it through its state machine, gate a service-request
// approval and a change CAB approval behind human signatures, and read the
// operator dashboard back (group-by + `assignee = null`). This is the test that
// proves the dashboard/null work holds up on a demanding domain, not a toy.
func TestDogfoodServiceDeskPack(t *testing.T) {
	ctx := context.Background()
	files := map[string]string{}
	for _, f := range []string{"pack.dsl", "servicedesk.dsl"} {
		src, err := os.ReadFile("../../packs/servicedesk/" + f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		files[f] = string(src)
	}
	model, errs := dsl.Compile(files)
	if len(errs) > 0 {
		t.Fatalf("servicedesk pack must compile: %v", errs[0])
	}

	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	tok := func(id, role string) string {
		tk, err := reg.RegisterWithToken(ctx, root, id, eventstore.ActorAgent, role, nil, nil, nil)
		if err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
		return tk
	}
	l1 := tok("op-l1", "OperatorL1")
	l2 := tok("op-l2", "OperatorL2")
	sup := tok("sup-1", "Supervisor")
	chg := tok("chg-1", "ChangeManager")
	lkp := tok("lkp-1", "LkpUser")
	adm := tok("adm-1", "Admin")

	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()
	basis := map[string]any{"type": "human", "id": "mike"}

	// (1) L1 opens two incidents; both unassigned, status New, number auto-issued
	mkInc := func(title string) (string, map[string]any) {
		rec, e := call(t, srv.URL, l1, "create_record", map[string]any{
			"entity": "Incident", "basis": basis,
			"values": map[string]any{"title": title, "priority": "P2", "source": "Manual"}})
		if e {
			t.Fatalf("create incident: %v", rec)
		}
		return rec["id"].(string), rec
	}
	inc1, rec1 := mkInc("Сервис ЛКП недоступен")
	mkInc("Не открывается отчёт")
	vals1, _ := rec1["values"].(map[string]any)
	if vals1 == nil || vals1["number"] == nil || vals1["number"] == "" {
		t.Errorf("incident must get a serial number on create, got %v", rec1["values"])
	}
	if vals1 == nil || vals1["age_days"] == nil {
		t.Errorf("age_days computed must be in the create response (days_since(opened)), got %v", rec1["values"])
	}

	// (2) drive inc1 through its state machine: investigate -> identify -> resolve -> close
	act := func(tk, entity, id, action, who string) map[string]any {
		res, e := call(t, srv.URL, tk, "act", map[string]any{
			"entity": entity, "id": id, "action": action, "basis": basis})
		if e {
			t.Fatalf("%s %s by %s: %v", action, id, who, res)
		}
		return res
	}
	act(l1, "Incident", inc1, "investigate", "L1") // New -> Investigating, queues a task for OperatorL2
	act(l2, "Incident", inc1, "identify", "L2")
	act(l2, "Incident", inc1, "resolve_incident", "L2")
	r := act(l2, "Incident", inc1, "close_incident", "L2")
	if r["status"] != "applied" {
		t.Fatalf("close_incident should apply: %v", r)
	}

	// (3) HITL: a service request that needs approval parks for a Supervisor signature
	sr, e := call(t, srv.URL, lkp, "create_record", map[string]any{
		"entity": "ServiceRequest", "basis": basis,
		"values": map[string]any{"approval_required": true}})
	if e {
		t.Fatalf("lkp create SR: %v", sr)
	}
	srID := sr["id"].(string)
	act(sup, "ServiceRequest", srID, "require_approval", "Supervisor") // -> ApprovalPending
	pend := act(sup, "ServiceRequest", srID, "approve_request", "Supervisor")
	if pend["status"] != "pending_approval" {
		t.Fatalf("approve_request must park for a human signature, got %v", pend)
	}

	// (4) HITL: a change blocked at CAB until the Change Manager signs
	cr, e := call(t, srv.URL, chg, "create_record", map[string]any{
		"entity": "Change", "basis": basis,
		"values": map[string]any{"title": "Обновление БД", "risk": "High", "change_type": "Normal"}})
	if e {
		t.Fatalf("create change: %v", cr)
	}
	crID := cr["id"].(string)
	act(chg, "Change", crID, "submit_change", "ChangeManager")
	act(chg, "Change", crID, "request_cab", "ChangeManager")
	cab := act(chg, "Change", crID, "approve_change", "ChangeManager")
	if cab["status"] != "pending_approval" {
		t.Fatalf("approve_change must park for CAB signature, got %v", cab)
	}

	// (5) the operator dashboard: group-by status + the `assignee = null` tile
	dash, isErr := call(t, srv.URL, sup, "dashboard", map[string]any{"name": "OperatorBoard"})
	if isErr {
		t.Fatalf("dashboard: %v", dash)
	}
	// inc1 is Closed, the second incident is still New -> one "open" incident
	if got := tileValue(t, dash, "Open incidents"); got != 1 {
		t.Errorf("Открытые инциденты = %v, want 1 (the un-driven New incident)", got)
	}
	// neither incident ever got an assignee set -> both unassigned
	if got := tileValue(t, dash, "Unassigned"); got != 2 {
		t.Errorf("Не назначены = %v, want 2 (assignee = null on both)", got)
	}
	byStatus := tileGroups(t, dash, "Incidents by status")
	if byStatus["New"] != 1 || byStatus["Closed"] != 1 {
		t.Errorf("Инциденты по статусу = %v, want New:1 Closed:1", byStatus)
	}

	// (6) the change dashboard sees the RFC stuck at CAB
	cdash, _ := call(t, srv.URL, chg, "dashboard", map[string]any{"name": "ChangesBoard"})
	if got := tileValue(t, cdash, "Awaiting CAB"); got != 1 {
		t.Errorf("Ждут CAB = %v, want 1", got)
	}

	// (7) live SLA: a P1 policy with a 30-min resolution, an incident opened long
	// ago -> sla_left = 30 - minutes_since(opened) goes deeply negative -> breached.
	// Proves minutes_since + a ref-path computed (sla_policy.resolution_minutes)
	// feed a dashboard `where sla_left < 0`.
	pol, e2 := call(t, srv.URL, adm, "create_record", map[string]any{
		"entity": "SLAPolicy", "basis": basis,
		"values": map[string]any{"name": "P1 24x7", "priority": "P1", "resolution_minutes": 30}})
	if e2 {
		t.Fatalf("create SLAPolicy: %v", pol)
	}
	br, e2 := call(t, srv.URL, adm, "create_record", map[string]any{
		"entity": "Incident", "basis": basis,
		"values": map[string]any{"title": "Старый инцидент", "source": "Manual",
			"sla_policy": pol["id"], "opened": "2020-01-01T00:00:00Z"}})
	if e2 {
		t.Fatalf("create breaching incident: %v", br)
	}
	if left, _ := br["values"].(map[string]any)["sla_left"].(float64); left >= 0 {
		t.Errorf("sla_left = %v, want negative (opened in 2020, 30-min SLA)", left)
	}
	sdash, _ := call(t, srv.URL, adm, "dashboard", map[string]any{"name": "OperatorBoard"})
	if got := tileValue(t, sdash, "SLA breaches"); got != 1 {
		t.Errorf("Просрочка SLA = %v, want 1 (only the breaching incident has a policy)", got)
	}

	// (8) array[file]: an incident carries several attachments (screenshots+logs)
	withFiles, e2 := call(t, srv.URL, l1, "create_record", map[string]any{
		"entity": "Incident", "basis": basis,
		"values": map[string]any{"title": "С логами", "source": "Manual", "attachments": []any{
			map[string]any{"hash": "abc123", "name": "screenshot.png", "size": 4096},
			map[string]any{"hash": "def456", "name": "app.log", "size": 8192},
		}}})
	if e2 {
		t.Fatalf("create incident with attachments: %v", withFiles)
	}
	att, _ := withFiles["values"].(map[string]any)["attachments"].([]any)
	if len(att) != 2 {
		t.Errorf("attachments round-trip = %v, want 2 files", att)
	}
	// a malformed attachment (no hash) is rejected
	bad, isErr2 := call(t, srv.URL, l1, "create_record", map[string]any{
		"entity": "Incident", "basis": basis,
		"values": map[string]any{"title": "Битое вложение", "source": "Manual",
			"attachments": []any{map[string]any{"name": "no-hash.txt"}}}})
	if !isErr2 || bad["code"] != "VALIDATION_ERROR" {
		t.Errorf("attachment without a hash must be rejected, got %v", bad)
	}
}
