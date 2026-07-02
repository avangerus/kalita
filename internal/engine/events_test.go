package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// engineFromSrc compiles a single-file inline pack and builds an engine over a
// fresh in-memory journal.
func engineFromSrc(t *testing.T, src string) *Engine {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"pack.dsl": src})
	if len(errs) > 0 {
		t.Fatalf("compile: %v", errs)
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// engineFromPack loads a real pack directory (all its .dsl files) and builds an
// engine over a fresh in-memory journal — used to prove the shipped fixes.
func engineFromPack(t *testing.T, dir string) *Engine {
	t.Helper()
	files := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range entries {
		if filepath.Ext(ent.Name()) != ".dsl" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, ent.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[ent.Name()] = string(raw)
	}
	model, errs := dsl.Compile(files)
	if len(errs) > 0 {
		t.Fatalf("compile %s: %v", dir, errs)
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func countRecords(e *Engine, entity string) int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.records[entity])
}

// Test 4 (LLD-01 §7): a transition emits an event; two subscribers fire. One
// succeeds (its effect lands), the other fails — and its failure is journaled
// and scheduled for retry individually, WITHOUT rolling back the emit or the
// sibling subscriber.
func TestEventEmitDispatch(t *testing.T) {
	const src = `pack a
version 0.1.0
event Placed:
    n: int
entity Order:
    title: string
    status: enum[Draft, Sent] default=Draft
entity Audit:
    label: string
entity FailLog:
    msg:  string required
    note: string
workflow Order on status:
    Draft -> Sent: send emits a.Placed(n = 1)
roles:
    Op
permissions:
    Op:
        full [Order, Audit, FailLog]
        act  [send]
automation:
    on event a.Placed:
        create Audit(label = "seen")
    on event a.Placed:
        create FailLog(note = "boom")
`
	e := engineFromSrc(t, src)
	op := eventstore.Actor{Type: eventstore.ActorHuman, ID: "u1", Role: "Op"}

	order, err := e.Create(ctx, op, "Order", map[string]any{"title": "T"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Act(ctx, op, "Order", order.ID, "send", basis, ""); err != nil {
		t.Fatalf("send transition (with emit) must apply: %v", err)
	}

	// the domain event was journaled with its canonical <pack>.<Event> name.
	emitted := e.EmittedEvents()
	if len(emitted) != 1 || emitted[0].Name != "a.Placed" {
		t.Fatalf("want one emitted a.Placed, got %+v", emitted)
	}

	// the transition committed despite a failing subscriber (no rollback).
	got, _ := e.Get(ctx, op, "Order", order.ID)
	if got.Values["status"] != "Sent" {
		t.Fatalf("emit failure of one subscriber must not roll back the transition: status=%v", got.Values["status"])
	}

	// the healthy subscriber's effect landed; the failing one produced nothing.
	if n := countRecords(e, "Audit"); n != 1 {
		t.Fatalf("succeeding subscriber must create one Audit, got %d", n)
	}
	if n := countRecords(e, "FailLog"); n != 0 {
		t.Fatalf("failing subscriber must create no FailLog, got %d", n)
	}

	// the failing subscriber is journaled and scheduled for retry individually.
	failed := countEventsOfKind(t, e, eventstore.AutomationFailed)
	if failed != 1 {
		t.Fatalf("exactly the failing subscriber must journal automation.failed, got %d", failed)
	}
	if len(e.failedActions) != 1 {
		t.Fatalf("failing subscriber must be in the retry projection, got %d entries", len(e.failedActions))
	}

	// the whole emit+dispatch is one op-bracket (LLD-01 §2).
	if b, d := countEventsOfKind(t, e, eventstore.OpBegin), countEventsOfKind(t, e, eventstore.OpEnd); b != 1 || d != 1 {
		t.Fatalf("emit cascade must be one balanced op-bracket: begins=%d ends=%d", b, d)
	}
}

// Test 8 (LLD-01 §7/§4.2): an emit whose payload cannot be evaluated aborts the
// WHOLE transition — nothing is journaled, the record does not move.
func TestEmitsFailureAbortsTransition(t *testing.T) {
	const src = `pack a
version 0.1.0
event Placed:
    n: int
entity Order:
    title: string
    status: enum[Draft, Sent] default=Draft
workflow Order on status:
    Draft -> Sent: send emits Placed(n = missing_field * 2)
roles:
    Op
permissions:
    Op:
        full [Order]
        act  [send]
`
	e := engineFromSrc(t, src)
	op := eventstore.Actor{Type: eventstore.ActorHuman, ID: "u1", Role: "Op"}
	order, err := e.Create(ctx, op, "Order", map[string]any{"title": "T"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	before := countEventsOfKind(t, e, eventstore.RecordAction)

	if _, err := e.Act(ctx, op, "Order", order.ID, "send", basis, ""); err == nil {
		t.Fatal("a broken emit payload must abort the transition with an error")
	}

	got, _ := e.Get(ctx, op, "Order", order.ID)
	if got.Values["status"] != "Draft" {
		t.Fatalf("aborted transition must leave the record in Draft, got %v", got.Values["status"])
	}
	if n := len(e.EmittedEvents()); n != 0 {
		t.Fatalf("no event.emitted may be journaled when the transition aborts, got %d", n)
	}
	if after := countEventsOfKind(t, e, eventstore.RecordAction); after != before {
		t.Fatalf("no record.action may be journaled when the transition aborts: %d -> %d", before, after)
	}
}

// Test 13 (LLD-01 §7): the crm convert cascade. Converting a qualified lead
// creates the Opportunity in the same op, and the Opportunity's own on-create
// automation fires within that cascade.
func TestAutomationCreateAction(t *testing.T) {
	e := engineFromPack(t, filepath.Join("..", "..", "packs", "crm"))
	mgr := eventstore.Actor{Type: eventstore.ActorHuman, ID: "mgr", Role: "SalesManager"}

	lead, err := e.Create(ctx, mgr, "Lead",
		map[string]any{"name": "Acme deal", "company": "Acme", "est_value": 50000.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"start_work", "qualify", "convert"} {
		if _, err := e.Act(ctx, mgr, "Lead", lead.ID, action, basis, ""); err != nil {
			t.Fatalf("act %s: %v", action, err)
		}
	}

	if n := countRecords(e, "Opportunity"); n != 1 {
		t.Fatalf("convert must create exactly one Opportunity, got %d", n)
	}
	// the created opportunity carries the mapped lead fields.
	opps, _ := e.Query(ctx, mgr, "Opportunity", QueryOpts{})
	if len(opps) != 1 || opps[0].Values["name"] != "Acme deal" {
		t.Fatalf("opportunity should carry the lead name, got %+v", opps)
	}
	// the Opportunity's `on create` triage fired in the same cascade: SalesBot
	// has an agent task for qualify_opp.
	var triage *Task
	for _, tk := range e.Tasks("SalesBot", TaskOpen) {
		if tk.Kind == TaskAgent && tk.Action == "qualify_opp" && tk.Entity == "Opportunity" {
			triage = tk
		}
	}
	if triage == nil {
		t.Fatalf("on create Opportunity must fire in the convert cascade, tasks: %+v", e.Tasks("SalesBot", TaskOpen))
	}
	// the lead reached Converted.
	got, _ := e.Get(ctx, mgr, "Lead", lead.ID)
	if got.Values["status"] != "Converted" {
		t.Fatalf("lead must be Converted, got %v", got.Values["status"])
	}
}

// Test 14 (LLD-01 §7): the eshop stock decrement. Adding an order line
// decrements its product's in_stock via the automation update ref-action.
func TestAutomationUpdateRefAction(t *testing.T) {
	e := engineFromPack(t, filepath.Join("..", "..", "packs", "eshop"))
	clerk := eventstore.Actor{Type: eventstore.ActorHuman, ID: "clerk", Role: "Clerk"}

	cust, err := e.Create(ctx, clerk, "Customer", map[string]any{"name": "Bob"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	prod, err := e.Create(ctx, clerk, "Product",
		map[string]any{"sku": "SKU-1", "name": "Widget", "in_stock": 2.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	order, err := e.Create(ctx, clerk, "Order", map[string]any{"customer": cust.ID}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Create(ctx, clerk, "OrderLine",
		map[string]any{"order": order.ID, "product": prod.ID, "qty": 2.0}, basis, ""); err != nil {
		t.Fatal(err)
	}

	got, _ := e.Get(ctx, clerk, "Product", prod.ID)
	if n, _ := toFloat(got.Values["in_stock"]); n != 0 {
		t.Fatalf("adding a line of qty 2 must decrement in_stock 2 -> 0, got %v", got.Values["in_stock"])
	}
}
