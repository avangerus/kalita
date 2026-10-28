package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// LIM-1: `on update` rules see workflow moves (act and approval decide), and
// `field changed` / `field changed to V` compare against the pre-change state.
const updateTriggerPack = `
entity Order:
    title: string required
    note: string
    status: enum[New, Review, Done] default=New

entity Audit:
    label: string

workflow Order on status:
    New    -> Review: submit
    Review -> Done:   close requires approval(Boss)

roles:
    Clerk
    Boss

permissions:
    Clerk:
        full [Order, Audit]
        act [submit, close]
    Boss:
        read [Order, Audit]
        approve [close]

automation:
    on update Order when status changed to Done:
        create Audit(label = "done")
    on update Order when status changed:
        create Audit(label = "moved")
    on update Order when note changed:
        create Audit(label = "note")
`

func updateTriggerEngine(t *testing.T) *Engine {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"t.dsl": updateTriggerPack})
	if len(errs) > 0 {
		t.Fatalf("pack must compile: %v", errs[0])
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func auditLabels(t *testing.T, e *Engine, actor eventstore.Actor) map[string]int {
	t.Helper()
	recs, err := e.Query(ctx, actor, "Audit", QueryOpts{})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, r := range recs {
		l, _ := r.Values["label"].(string)
		out[l]++
	}
	return out
}

func TestUpdateTriggersSeeWorkflowMoves(t *testing.T) {
	e := updateTriggerEngine(t)
	clerk := eventstore.Actor{Type: eventstore.ActorHuman, ID: "c", Role: "Clerk"}
	boss := eventstore.Actor{Type: eventstore.ActorHuman, ID: "b", Role: "Boss"}

	o, err := e.Create(ctx, clerk, "Order", map[string]any{"title": "A"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := auditLabels(t, e, clerk); len(got) != 0 {
		t.Fatalf("create must not fire on update rules, got %v", got)
	}

	if _, err := e.Act(ctx, clerk, "Order", o.ID, "submit", basis, ""); err != nil {
		t.Fatal(err)
	}
	if got := auditLabels(t, e, clerk); got["moved"] != 1 || got["done"] != 0 || got["note"] != 0 {
		t.Fatalf("act New->Review: want moved=1 only, got %v", got)
	}

	// a plain field update: status did not change, note did
	if _, err := e.Update(ctx, clerk, "Order", o.ID, map[string]any{"note": "x"}, basis, ""); err != nil {
		t.Fatal(err)
	}
	if got := auditLabels(t, e, clerk); got["moved"] != 1 || got["note"] != 1 {
		t.Fatalf("note update: want moved=1 note=1, got %v", got)
	}

	res, err := e.Act(ctx, clerk, "Order", o.ID, "close", basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "pending_approval" {
		t.Fatalf("close must park for approval, got %s", res.Status)
	}
	if got := auditLabels(t, e, clerk); got["done"] != 0 {
		t.Fatalf("a parked approval is not a move, got %v", got)
	}
	if _, err := e.Decide(ctx, boss, res.ApprovalID, true, nil, basis); err != nil {
		t.Fatal(err)
	}
	if got := auditLabels(t, e, clerk); got["done"] != 1 || got["moved"] != 2 || got["note"] != 1 {
		t.Fatalf("decided Review->Done: want done=1 moved=2 note=1, got %v", got)
	}
}

func TestChangedFailsClosedWithoutPreviousState(t *testing.T) {
	vals := map[string]any{"status": "Done"}
	if evalWhere("status changed", evalCtx{values: vals}) {
		t.Fatal("`changed` outside an update rule must be false")
	}
	if evalWhere("status changed to Done", evalCtx{values: vals}) {
		t.Fatal("`changed to` outside an update rule must be false")
	}
	c := evalCtx{values: vals, prev: map[string]any{"status": "Review"}}
	if !evalWhere("status changed to Done", c) || evalWhere("status changed to Review", c) {
		t.Fatal("`changed to` must compare the new value")
	}
	money := map[string]any{"amount": 5.0, "currency": "EUR"}
	c = evalCtx{values: map[string]any{"amount": money},
		prev: map[string]any{"amount": map[string]any{"amount": 5.0, "currency": "USD"}}}
	if !evalWhere("amount changed", c) {
		t.Fatal("a currency change is a change")
	}
	if !evalWhere("amount changed and not (status changed)", evalCtx{values: map[string]any{"amount": money, "status": "A"},
		prev: map[string]any{"amount": 1.0, "status": "A"}}) {
		t.Fatal("`changed` must compose with and/not")
	}
}

// Every shipped pack and example must load: an automation condition the
// evaluator cannot read is refused at load instead of sitting silently dead.
func TestShippedAutomationConditionsParse(t *testing.T) {
	dirs, _ := filepath.Glob(filepath.Join("..", "..", "packs", "*"))
	ex, _ := filepath.Glob(filepath.Join("..", "..", "examples", "*"))
	checked := 0
	for _, dir := range append(dirs, ex...) {
		files := map[string]string{}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, ent := range entries {
			if filepath.Ext(ent.Name()) == ".dsl" {
				raw, err := os.ReadFile(filepath.Join(dir, ent.Name()))
				if err != nil {
					t.Fatal(err)
				}
				files[ent.Name()] = string(raw)
			}
		}
		if len(files) == 0 {
			continue
		}
		model, errs := dsl.Compile(files)
		if len(errs) > 0 {
			continue // multi-pack composition, compiled by its own tests
		}
		if err := checkAutomationConditions(model); err != nil {
			t.Errorf("%s: %v", dir, err)
		}
		checked++
	}
	if checked < 10 {
		t.Fatalf("only %d packs checked — the glob is not finding them", checked)
	}
}

func TestUnparseableAutomationConditionIsRefused(t *testing.T) {
	model, errs := dsl.Compile(map[string]string{"t.dsl": `
entity Doc:
    status: enum[A, B] default=A

automation:
    on update Doc when status becomes B:
        escalate_to Boss

roles:
    Boss
`})
	if len(errs) > 0 {
		t.Fatalf("compile: %v", errs[0])
	}
	_, err := New(ctx, model, eventstore.NewMemStore(nil))
	wantCode(t, err, CodeValidation)
}

func TestChangedToReadsMoneyAmount(t *testing.T) {
	c := evalCtx{values: map[string]any{"amount": map[string]any{"amount": 5.0, "currency": "EUR"}},
		prev: map[string]any{"amount": map[string]any{"amount": 3.0, "currency": "EUR"}}}
	if !evalWhere("amount changed to 5", c) || evalWhere("amount changed to 3", c) {
		t.Fatal("`changed to` on money must compare the amount, like `=`")
	}
}
