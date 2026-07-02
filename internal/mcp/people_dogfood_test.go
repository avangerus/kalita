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

// packs/people is a teaching-reference pack: it demonstrates kalita's three
// behaviour levels in one place, and this test is the dogfood proof that
// event / emit / on-event actually work end to end on a FRESH pack (not just
// the existing crm/site fixtures):
//
//   Level 1 (action/transition): Employee.status Active<->Inactive, with
//   `deactivate` gated behind HRAdmin's HITL signature.
//   Level 2 (event): `emit_event` fires people.OutOfOffice; `on event
//   OutOfOffice` reacts by notifying the manager and creating a Coverage
//   record; that record's own `on create Coverage` then queues a task for
//   the CoverageBot agent role (the same two-step bridge packs/crm/crm.dsl
//   uses for LeadConverted -> Opportunity -> SalesBot).
//   Level 3 (worker code): simulated here via direct MCP calls under the
//   CoverageBot token — exactly what workers/coverage_bot/bot.py does — take
//   the task, read the record, write a comment, complete the task.
func TestDogfoodPeopleThreeLevels(t *testing.T) {
	ctx := context.Background()
	model, errs := dsl.Compile(packFiles(t, "../../packs/people"))
	if len(errs) > 0 {
		t.Fatalf("people must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	hr, _ := reg.RegisterWithToken(ctx, root, "hr-1", eventstore.ActorHuman, "HRAdmin", nil, nil, nil)
	emp, _ := reg.RegisterWithToken(ctx, root, "emp-1", eventstore.ActorHuman, "Employee", nil, nil, nil)
	bot, _ := reg.RegisterWithToken(ctx, root, "bot-1", eventstore.ActorAgent, "CoverageBot", nil, nil, nil)
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()
	hrActor := eventstore.Actor{Type: eventstore.ActorHuman, ID: "hr-1", Role: "HRAdmin"}
	basis := map[string]any{"type": "human", "id": "hr-1"}
	tb := func(id string) map[string]any { return map[string]any{"type": "task", "id": id} }

	// --- fixture: a manager and two reports -------------------------------
	alice, e := call(t, srv.URL, hr, "create_record", map[string]any{
		"entity": "Employee", "basis": basis,
		"values": map[string]any{"name": "Alice Manager", "email": "alice@example.com"}})
	if e {
		t.Fatalf("create Alice: %v", alice)
	}
	aliceID := alice["id"].(string)
	bobRec, e := call(t, srv.URL, hr, "create_record", map[string]any{
		"entity": "Employee", "basis": basis,
		"values": map[string]any{"name": "Bob Report", "email": "bob@example.com", "manager": aliceID}})
	if e {
		t.Fatalf("create Bob: %v", bobRec)
	}
	bobID := bobRec["id"].(string)
	carolRec, e := call(t, srv.URL, hr, "create_record", map[string]any{
		"entity": "Employee", "basis": basis,
		"values": map[string]any{"name": "Carol Report", "email": "carol@example.com", "manager": aliceID}})
	if e {
		t.Fatalf("create Carol: %v", carolRec)
	}
	carolID := carolRec["id"].(string)

	// =======================================================================
	// LEVEL 1 — action/transition: deactivate is HITL, activate is not.
	// =======================================================================

	// (a) deactivate without HRAdmin's signature parks pending; status unchanged.
	d1 := call2ok(t, srv.URL, hr, "act", map[string]any{
		"entity": "Employee", "id": bobID, "action": "deactivate", "basis": basis})
	if d1["status"] != "pending_approval" {
		t.Fatalf("deactivate must park for HRAdmin's signature, got %v", d1)
	}
	got, _ := call(t, srv.URL, hr, "get_record", map[string]any{"entity": "Employee", "id": bobID})
	if got["values"].(map[string]any)["status"] != "Active" {
		t.Fatalf("Bob must still be Active before the signature, got %v", got["values"])
	}
	pend := eng.PendingApprovals("HRAdmin")
	if len(pend) != 1 {
		t.Fatalf("want 1 pending deactivate approval, got %d", len(pend))
	}

	// (b) with HRAdmin's signature, the transition actually applies.
	if _, err := eng.Decide(ctx, hrActor, pend[0].ID, true, nil, &eventstore.Basis{Type: "human", ID: "hr-1"}); err != nil {
		t.Fatalf("HRAdmin decision: %v", err)
	}
	got, _ = call(t, srv.URL, hr, "get_record", map[string]any{"entity": "Employee", "id": bobID})
	if got["values"].(map[string]any)["status"] != "Inactive" {
		t.Fatalf("Bob must be Inactive after HRAdmin's signature, got %v", got["values"])
	}

	// =======================================================================
	// LEVEL 2 — event: emit_event(OutOfOffice) -> notify + create Coverage
	// -> (record trigger) queue a CoverageBot task.
	// =======================================================================

	fact, e := call(t, srv.URL, emp, "emit_event", map[string]any{
		"name": "OutOfOffice",
		"payload": map[string]any{
			"employee": carolID, "from": "2026-08-01", "to": "2026-08-10"},
	})
	if e {
		t.Fatalf("Employee must be able to emit_event(OutOfOffice): %v", fact)
	}
	if fact["name"] != "people.OutOfOffice" {
		t.Errorf("emitted fact name = %v, want people.OutOfOffice", fact["name"])
	}
	facts := eng.EmittedEvents()
	if len(facts) != 1 || facts[0].Name != "people.OutOfOffice" {
		t.Fatalf("journal must carry exactly 1 people.OutOfOffice event.emitted, got %v", facts)
	}

	// the automation's create Coverage(...) actually ran, with employee.manager
	// resolved one ref-hop from the event payload (Alice, not empty/blank).
	cov, e := call(t, srv.URL, hr, "query", map[string]any{"entity": "Coverage"})
	if e {
		t.Fatalf("query Coverage: %v", cov)
	}
	crecs, _ := cov["records"].([]any)
	if len(crecs) != 1 {
		t.Fatalf("want 1 Coverage record created by the automation, got %d: %v", len(crecs), cov)
	}
	crow := crecs[0].(map[string]any)
	cid := crow["id"].(string)
	cvals := crow["values"].(map[string]any)
	if cvals["employee"] != carolID {
		t.Errorf("Coverage.employee = %v, want %v", cvals["employee"], carolID)
	}
	if cvals["manager"] != aliceID {
		t.Errorf("Coverage.manager = %v, want %v (resolved via employee.manager)", cvals["manager"], aliceID)
	}

	// the notify action fired too (Role-less TaskNotification — LLD-01's
	// `notify` carries no role, just a free-form Args hint).
	notifs := eng.Tasks("", engine.TaskOpen)
	found := false
	for _, nt := range notifs {
		if nt.Kind == engine.TaskNotification {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a TaskNotification queued by `notify email(employee.manager)`, got %v", notifs)
	}

	// the SECOND automation step (`on create Coverage: agent CoverageBot: ...`)
	// queued a REAL task with entity/record_id pointing at the new Coverage.
	mine, e := call(t, srv.URL, bot, "list_my_tasks", map[string]any{})
	if e {
		t.Fatalf("CoverageBot list_my_tasks: %v", mine)
	}
	tasks, _ := mine["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("want 1 CoverageBot task, got %d: %v", len(tasks), mine)
	}
	task := tasks[0].(map[string]any)
	if task["entity"] != "Coverage" || task["record_id"] != cid || task["action"] != "arrange_coverage" {
		t.Fatalf("CoverageBot task = %v, want entity=Coverage record_id=%s action=arrange_coverage", task, cid)
	}
	tid := task["id"].(string)

	// =======================================================================
	// LEVEL 3 — worker code: what workers/coverage_bot/bot.py does, simulated
	// here as the same MCP calls under CoverageBot's own least-privilege token.
	// =======================================================================

	if _, e := call(t, srv.URL, bot, "take_task", map[string]any{"task_id": tid}); e {
		t.Fatal("CoverageBot could not take its own task")
	}
	covRec, e := call(t, srv.URL, bot, "get_record", map[string]any{"entity": "Coverage", "id": cid})
	if e {
		t.Fatalf("CoverageBot could not read the Coverage record it was assigned: %v", covRec)
	}
	mgrID, _ := covRec["values"].(map[string]any)["manager"].(string)
	mgrRec, e := call(t, srv.URL, bot, "get_record", map[string]any{"entity": "Employee", "id": mgrID})
	if e {
		t.Fatalf("CoverageBot could not read the manager Employee record: %v", mgrRec)
	}
	mgrName, _ := mgrRec["values"].(map[string]any)["name"].(string)
	if mgrName != "Alice Manager" {
		t.Fatalf("resolved manager name = %q, want Alice Manager", mgrName)
	}
	body := "замена организована: " + mgrName
	if _, e := call(t, srv.URL, bot, "comment", map[string]any{
		"entity": "Coverage", "id": cid, "body": body, "basis": tb(tid)}); e {
		t.Fatal("CoverageBot could not post its coverage comment")
	}
	if _, e := call(t, srv.URL, bot, "complete_task", map[string]any{"task_id": tid, "result": body}); e {
		t.Fatal("CoverageBot could not complete its task")
	}

	// the comment landed in the journal under the CoverageBot actor.
	cm, e := call(t, srv.URL, hr, "read_comments", map[string]any{"entity": "Coverage", "id": cid})
	if e {
		t.Fatalf("read_comments: %v", cm)
	}
	comments, _ := cm["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("want 1 comment on the Coverage record, got %d: %v", len(comments), cm)
	}
	c0 := comments[0].(map[string]any)
	if c0["body"] != body {
		t.Errorf("comment body = %v, want %v", c0["body"], body)
	}
	author, _ := c0["author"].(map[string]any)
	if author["role"] != "CoverageBot" || author["id"] != "bot-1" {
		t.Errorf("comment author = %v, want role=CoverageBot id=bot-1 (the agent's OWN identity, not a system actor)", author)
	}

	// CoverageBot cannot do what is outside its least-privilege grant: it must
	// NOT be able to deactivate (or activate) an Employee, by `act`...
	if r, e := call(t, srv.URL, bot, "act", map[string]any{
		"entity": "Employee", "id": carolID, "action": "deactivate", "basis": tb(tid)}); !e {
		t.Fatalf("CoverageBot must not be able to deactivate an Employee, got %v", r)
	}
	// ...nor by writing the workflow field directly.
	if r, e := call(t, srv.URL, bot, "update_record", map[string]any{
		"entity": "Employee", "id": carolID, "basis": tb(tid),
		"values": map[string]any{"status": "Inactive"}}); !e {
		t.Fatalf("CoverageBot must not be able to write Employee.status directly, got %v", r)
	}
}
