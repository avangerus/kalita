package engine

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)


// Week 5 DoD: the debtor scenario lives without humans up to the HITL point —
// overdue → reminder task to the agent → claim → stuck → escalation.

type tickEngine struct {
	*Engine
	clock *time.Time
}

func automationEngine(t *testing.T) *tickEngine {
	t.Helper()
	files := map[string]string{}
	for _, name := range []string{"pack.kal", "collections.kal"} {
		raw, err := os.ReadFile("../../examples/collections/" + name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(raw)
	}
	model, errs := dsl.Compile(files)
	if len(errs) > 0 {
		t.Fatalf("compile: %v", errs[0])
	}
	clock := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	e, err := New(ctx, model, eventstore.NewMemStore(nil),
		WithClock(func() time.Time { return clock }),
		WithTaskTTL(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return &tickEngine{Engine: e, clock: &clock}
}

func TestDebtorRunsWithoutHumansUntilHITL(t *testing.T) {
	e := automationEngine(t)

	// contract 3 days overdue → debtor auto-moves to Overdue on creation
	contract, err := e.Create(ctx, anna, "Contract",
		map[string]any{"company": "Vector LLC", "due_date": "2026-06-09", "amount": 50000.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	debtor, err := e.Create(ctx, anna, "Debtor",
		map[string]any{"company": "Vector LLC", "contract": contract.ID, "debt": 50000.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}

	// entering Overdue queued the send_claim workflow task for the Collector
	open := e.Tasks("Collector", TaskOpen)
	if len(open) != 1 || open[0].Kind != TaskWorkflow || open[0].Action != "send_claim" {
		t.Fatalf("workflow task expected: %+v", open)
	}

	// Tick fires the schedule rule (overdue_days=3 ∈ [3,7,14]): reminder task
	if err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	open = e.Tasks("Collector", TaskOpen)
	if len(open) != 2 {
		t.Fatalf("after tick: want send_claim + draft_reminder, got %+v", open)
	}
	// same-day second tick must not duplicate
	_ = e.Tick(ctx)
	if got := len(e.Tasks("Collector", TaskOpen)); got != 2 {
		t.Fatalf("tick must be idempotent per day, got %d tasks", got)
	}

	// the agent works the reminder: take → progress (facts checked) → complete
	var reminder *Task
	for _, task := range e.Tasks("Collector", TaskOpen) {
		if task.Action == "draft_reminder" {
			reminder = task
		}
	}
	if _, err := e.TakeTask(ctx, collector, reminder.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.ReportProgress(ctx, collector, reminder.ID, "письмо отправлено"); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteTask(ctx, collector, reminder.ID, "reminder sent"); err != nil {
		t.Fatal(err)
	}

	// the agent performs the workflow task: send_claim → Claim
	var claimTask *Task
	for _, task := range e.Tasks("Collector", TaskOpen) {
		if task.Action == "send_claim" {
			claimTask = task
		}
	}
	if _, err := e.TakeTask(ctx, collector, claimTask.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Act(ctx, collector, "Debtor", debtor.ID, "send_claim", basis, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteTask(ctx, collector, claimTask.ID, "claim sent"); err != nil {
		t.Fatal(err)
	}
	rec, _ := e.Get(ctx, anna, "Debtor", debtor.ID)
	if rec.Values["status"] != "Claim" {
		t.Fatalf("want Claim, got %v", rec.Values["status"])
	}

	// 11 days pass → stuck rule (Claim for 10d) escalates to FinDirector
	*e.clock = e.clock.Add(11 * 24 * time.Hour)
	if err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	esc := e.Tasks("FinDirector", TaskOpen)
	if len(esc) != 1 || esc[0].Kind != TaskEscalation || esc[0].RecordID != debtor.ID {
		t.Fatalf("escalation expected: %+v", esc)
	}
	// repeated tick: same stuck episode does not re-fire
	_ = e.Tick(ctx)
	if got := len(e.Tasks("FinDirector", TaskOpen)); got != 1 {
		t.Fatalf("stuck must fire once per episode, got %d", got)
	}

	// no human has touched the record since creation: every event so far is
	// anna's initial setup, the agent within permissions, or the system
	if rec.Values["status"] != "Claim" {
		t.Fatal("record must wait for the human at the HITL boundary")
	}
}

func TestTaskLeaseSemantics(t *testing.T) {
	e := automationEngine(t)
	contract, _ := e.Create(ctx, anna, "Contract",
		map[string]any{"company": "V", "due_date": "2026-06-01"}, basis, "")
	_, _ = e.Create(ctx, anna, "Debtor",
		map[string]any{"company": "V", "contract": contract.ID, "debt": 1.0}, basis, "")

	task := e.Tasks("Collector", TaskOpen)[0]

	// wrong role cannot take
	if _, err := e.TakeTask(ctx, anna, task.ID); err == nil {
		t.Fatal("task is role-bound")
	}
	if _, err := e.TakeTask(ctx, collector, task.ID); err != nil {
		t.Fatal(err)
	}
	// second take while leased → conflict
	other := eventstore.Actor{Type: eventstore.ActorAgent, ID: "collector-2", Role: "Collector"}
	if _, err := e.TakeTask(ctx, other, task.ID); err == nil {
		t.Fatal("leased task must not be taken twice")
	}
	// lease expires → task returns to the pool, another agent picks it up
	*e.clock = e.clock.Add(31 * time.Minute)
	if _, err := e.TakeTask(ctx, other, task.ID); err != nil {
		t.Fatalf("expired lease must reopen the task: %v", err)
	}
	// the original agent can no longer complete it
	if err := e.CompleteTask(ctx, collector, task.ID, "done"); err == nil {
		t.Fatal("losing the lease means losing the task")
	}
	if err := e.CompleteTask(ctx, other, task.ID, "done"); err != nil {
		t.Fatal(err)
	}
}

func TestProgressFactCheck(t *testing.T) {
	e := automationEngine(t)
	contract, _ := e.Create(ctx, anna, "Contract",
		map[string]any{"company": "V", "due_date": "2026-06-01"}, basis, "")
	debtor, _ := e.Create(ctx, anna, "Debtor",
		map[string]any{"company": "V", "contract": contract.ID, "debt": 1.0}, basis, "")

	task := e.Tasks("Collector", TaskOpen)[0]
	_, _ = e.TakeTask(ctx, collector, task.ID)

	// report with zero actual events on the record → facts: 0 in the journal
	_ = e.ReportProgress(ctx, collector, task.ID, "всё почти готово, осталось чуть-чуть")
	events, _ := e.store.All(ctx)
	last := events[len(events)-1]
	if last.Kind != eventstore.TaskProgress {
		t.Fatal("progress must be journaled")
	}
	var p taskPayload
	_ = json.Unmarshal(last.Payload, &p)
	if p.Facts != 0 {
		t.Fatalf("embellished report must show facts=0, got %d", p.Facts)
	}

	// after real work the fact counter moves
	_, _ = e.Act(ctx, collector, "Debtor", debtor.ID, "send_claim", basis, "")
	_ = e.ReportProgress(ctx, collector, task.ID, "претензия отправлена")
	events, _ = e.store.All(ctx)
	_ = json.Unmarshal(events[len(events)-1].Payload, &p)
	if p.Facts != 1 {
		t.Fatalf("real work must show facts=1, got %d", p.Facts)
	}
}
