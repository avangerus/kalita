package engine

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

func TestScheduleInterval(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"every 5m", 5 * time.Minute, true},
		{"every 2h", 2 * time.Hour, true},
		{"every 1d", 24 * time.Hour, true},
		{"daily at 09:00", 0, false}, // wall-clock form: falls back to per-day
		{"every 0m", 0, false},       // zero interval rejected (no div-by-zero)
		{"", 0, false},
		{"every", 0, false},
	}
	for _, c := range cases {
		got, ok := scheduleInterval(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("scheduleInterval(%q) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// Week 5 DoD: the debtor scenario lives without humans up to the HITL point —
// overdue → reminder task to the agent → claim → stuck → escalation.

type tickEngine struct {
	*Engine
	clock *time.Time
}

func automationEngine(t *testing.T) *tickEngine {
	t.Helper()
	files := map[string]string{}
	for _, name := range []string{"pack.dsl", "collections.dsl"} {
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
	// One clock for engine AND store: stuck-detection compares engine now()
	// against event TS written by the store — split clocks make the test a
	// time bomb that starts failing once wall time passes the fixture date.
	clock := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	e, err := New(ctx, model, eventstore.NewMemStore(func() time.Time { return clock }),
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
	if _, err := e.TakeTask(ctx, collector, reminder.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.ReportProgress(ctx, collector, reminder.ID, "письмо отправлено", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteTask(ctx, collector, reminder.ID, "reminder sent", ""); err != nil {
		t.Fatal(err)
	}

	// the agent performs the workflow task: send_claim → Claim
	var claimTask *Task
	for _, task := range e.Tasks("Collector", TaskOpen) {
		if task.Action == "send_claim" {
			claimTask = task
		}
	}
	if _, err := e.TakeTask(ctx, collector, claimTask.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Act(ctx, collector, "Debtor", debtor.ID, "send_claim", basis, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteTask(ctx, collector, claimTask.ID, "claim sent", ""); err != nil {
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
	if _, err := e.TakeTask(ctx, anna, task.ID, ""); err == nil {
		t.Fatal("task is role-bound")
	}
	if _, err := e.TakeTask(ctx, collector, task.ID, ""); err != nil {
		t.Fatal(err)
	}
	// second take while leased → conflict
	other := eventstore.Actor{Type: eventstore.ActorAgent, ID: "collector-2", Role: "Collector"}
	if _, err := e.TakeTask(ctx, other, task.ID, ""); err == nil {
		t.Fatal("leased task must not be taken twice")
	}
	// lease expires → task returns to the pool, another agent picks it up
	*e.clock = e.clock.Add(31 * time.Minute)
	if _, err := e.TakeTask(ctx, other, task.ID, ""); err != nil {
		t.Fatalf("expired lease must reopen the task: %v", err)
	}
	// the original agent can no longer complete it
	if err := e.CompleteTask(ctx, collector, task.ID, "done", ""); err == nil {
		t.Fatal("losing the lease means losing the task")
	}
	if err := e.CompleteTask(ctx, other, task.ID, "done", ""); err != nil {
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
	_, _ = e.TakeTask(ctx, collector, task.ID, "")

	// report with zero actual events on the record → facts: 0 in the journal
	_ = e.ReportProgress(ctx, collector, task.ID, "всё почти готово, осталось чуть-чуть", "")
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
	_ = e.ReportProgress(ctx, collector, task.ID, "претензия отправлена", "")
	events, _ = e.store.All(ctx)
	_ = json.Unmarshal(events[len(events)-1].Payload, &p)
	if p.Facts != 1 {
		t.Fatalf("real work must show facts=1, got %d", p.Facts)
	}
}

// --- TECHDEBT "Ядро: automation / schedule" P1 -----------------------------
//
// schedPack is a minimal fixture isolated from collections.dsl: these tests
// exercise exactly the schedule mechanics under test (wall-clock gating,
// per-rule task dedup, in-memory retention) without collections.dsl's
// unrelated debtor/stuck machinery in the way.
const schedPack = `
pack schedtest
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1

entity Job:
    title:  string required
    status: enum[Active, Done] default=Active

roles:
    Admin
    Worker agent

permissions:
    Admin:
        full [Job]
    Worker:
        read [Job]
        act  [finish]
        deny [update Job.*, delete *, create *]

workflow Job on status:
    Active -> Done: finish

automation:
    on schedule every 5m for Job when status = Active:
        agent Worker: sync()
    on schedule daily at 09:00:
        agent Worker: resync()
`

var (
	schedAdmin  = eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Admin"}
	schedWorker = eventstore.Actor{Type: eventstore.ActorAgent, ID: "worker-1", Role: "Worker"}
	schedBasis  = &eventstore.Basis{Type: "human", ID: "root"}
)

func schedEngine(t *testing.T, start time.Time) (*Engine, *time.Time) {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"sched.dsl": schedPack})
	if len(errs) > 0 {
		t.Fatalf("schedPack must compile: %v", errs[0])
	}
	clock := start
	e, err := New(ctx, model, eventstore.NewMemStore(func() time.Time { return clock }),
		WithClock(func() time.Time { return clock }))
	if err != nil {
		t.Fatal(err)
	}
	return e, &clock
}

func actionTasks(e *Engine, action string) int {
	n := 0
	for _, task := range e.Tasks("Worker", "") {
		if task.Action == action {
			n++
		}
	}
	return n
}

// openActionTasks filters e.Tasks("Worker", TaskOpen) down to one action —
// schedPack's daily rule (resync) and interval rule (sync) share the Worker
// role, so a Tick landing exactly at/after 09:00 fires both.
func openActionTasks(e *Engine, action string) []*Task {
	var out []*Task
	for _, task := range e.Tasks("Worker", TaskOpen) {
		if task.Action == action {
			out = append(out, task)
		}
	}
	return out
}

// A "daily at HH:MM" schedule rule must gate on the actual wall-clock time of
// day (TECHDEBT P1 #1): before HH:MM it must not fire at all; the first Tick
// at or after HH:MM the same day fires it exactly once; a later Tick the same
// day must not duplicate it; the next day it fires again.
func TestScheduleDailyAtGatesTimeOfDay(t *testing.T) {
	e, clock := schedEngine(t, time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC))

	if err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got := actionTasks(e, "resync"); got != 0 {
		t.Fatalf("before 09:00 the daily rule must not fire, got %d resync tasks", got)
	}

	*clock = time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	if err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got := actionTasks(e, "resync"); got != 1 {
		t.Fatalf("at 09:00 the daily rule must fire once, got %d resync tasks", got)
	}

	*clock = time.Date(2026, 6, 12, 18, 0, 0, 0, time.UTC)
	if err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got := actionTasks(e, "resync"); got != 1 {
		t.Fatalf("same-day tick must not duplicate the daily firing, got %d resync tasks", got)
	}

	*clock = time.Date(2026, 6, 13, 9, 30, 0, 0, time.UTC)
	if err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got := actionTasks(e, "resync"); got != 2 {
		t.Fatalf("the next day at/after 09:00 must fire again, got %d resync tasks", got)
	}
}

// A recurring "every Nm" schedule rule must not pile up a fresh task every
// bucket forever while the previous firing's task is still open (TECHDEBT P1
// #2a) — the KnowVault resync growth problem this fix targets.
func TestScheduleRuleDedupsWhileTaskOpen(t *testing.T) {
	e, clock := schedEngine(t, time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC))
	if _, err := e.Create(ctx, schedAdmin, "Job", map[string]any{"title": "seed"}, schedBasis, ""); err != nil {
		t.Fatal(err)
	}

	if err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	open := openActionTasks(e, "sync")
	if len(open) != 1 {
		t.Fatalf("first tick must create exactly one sync task, got %+v", open)
	}
	firstID := open[0].ID

	// several more interval buckets pass while the task sits untouched — must
	// NOT create a second task for the same rule/record.
	for i := 0; i < 3; i++ {
		*clock = clock.Add(5 * time.Minute)
		if err := e.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	open = openActionTasks(e, "sync")
	if len(open) != 1 || open[0].ID != firstID {
		t.Fatalf("open task from the same rule must dedup across buckets, got %+v", open)
	}

	// complete it — the rule is now free to fire a fresh one
	if _, err := e.TakeTask(ctx, schedWorker, firstID, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.CompleteTask(ctx, schedWorker, firstID, "done", ""); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(5 * time.Minute)
	if err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	open = openActionTasks(e, "sync")
	if len(open) != 1 || open[0].ID == firstID {
		t.Fatalf("once the previous task finished, the rule must fire a fresh one, got %+v", open)
	}
}

// replayStore feeds engine.New the exact frozen events captured from a live
// run. New() only ever calls All() during replay (Append/Since/Head/
// ByIdemKey are for a running engine's later mutations, never exercised
// here), so a read-only stub is enough to prove replay reproduces the same
// projection from the same journal.
type replayStore struct{ events []*eventstore.Event }

func (s *replayStore) Append(context.Context, eventstore.AppendInput) (*eventstore.Event, error) {
	panic("replayStore is read-only")
}
func (s *replayStore) All(context.Context) ([]*eventstore.Event, error) { return s.events, nil }
func (s *replayStore) Since(context.Context, uint64) ([]*eventstore.Event, error) {
	panic("replayStore.Since is unused by this test")
}
func (s *replayStore) Head(context.Context) (uint64, []byte, error) {
	panic("replayStore.Head is unused by this test")
}
func (s *replayStore) ByIdemKey(context.Context, string) (*eventstore.Event, error) {
	panic("replayStore.ByIdemKey is unused by this test")
}
func (s *replayStore) Verify(context.Context) error {
	panic("replayStore.Verify is unused by this test")
}

// Ping reports the replay journal as reachable (memory-resident).
func (s *replayStore) Ping(context.Context) error { return nil }

var _ eventstore.Store = (*replayStore)(nil)

// Finished schedule-rule tasks must be pruned from the in-memory projection
// past retention (TECHDEBT P1 #2b), and the pruning must be reproducible by
// replay: it is a function of fold ORDER, never of wall-clock time (I-6
// determinism) — rebuilding the engine from the exact same journal must
// prune the exact same task IDs.
func TestScheduleTaskRetentionIsDeterministicAcrossReplay(t *testing.T) {
	e, clock := schedEngine(t, time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC))
	if _, err := e.Create(ctx, schedAdmin, "Job", map[string]any{"title": "seed"}, schedBasis, ""); err != nil {
		t.Fatal(err)
	}

	const retention = 20 // engine default, see New()
	if e.scheduleTaskRetention != retention {
		t.Fatalf("test assumes the default retention (%d), engine has %d", retention, e.scheduleTaskRetention)
	}
	const rounds = retention + 10
	for i := 0; i < rounds; i++ {
		if err := e.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		open := openActionTasks(e, "sync")
		if len(open) != 1 {
			t.Fatalf("round %d: want exactly one open sync task, got %+v", i, open)
		}
		if _, err := e.TakeTask(ctx, schedWorker, open[0].ID, ""); err != nil {
			t.Fatal(err)
		}
		if err := e.CompleteTask(ctx, schedWorker, open[0].ID, "done", ""); err != nil {
			t.Fatal(err)
		}
		*clock = clock.Add(5 * time.Minute)
	}

	// the in-memory projection must not have grown unboundedly: strictly
	// more finished rounds (30) than the retention cap (20) ran, so pruning
	// must have happened.
	live := len(e.tasks)
	finishedSync := 0
	for _, task := range e.tasks {
		if task.Action == "sync" {
			finishedSync++
		}
	}
	if finishedSync > retention {
		t.Fatalf("projection grew past retention: %d sync tasks in memory, want <= %d", finishedSync, retention)
	}

	// replay from the identical frozen journal must reproduce the exact same
	// surviving task set.
	events, err := e.store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := New(ctx, e.Model(), &replayStore{events: events}, WithClock(func() time.Time { return *clock }))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(replayed.tasks); got != live {
		t.Fatalf("replay must reproduce the same task count: live=%d replayed=%d", live, got)
	}
	for id, want := range e.tasks {
		got, ok := replayed.tasks[id]
		if !ok {
			t.Fatalf("replay is missing task %s (id=%s) that survived live retention", want.Action, id)
		}
		if got.Status != want.Status || got.Action != want.Action {
			t.Fatalf("replay task %s mismatch: live=%+v replayed=%+v", id, want, got)
		}
	}
}
