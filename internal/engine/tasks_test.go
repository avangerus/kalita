package engine

import (
	"bytes"
	"context"
	"errors"
	"log"
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// workflowAssigneeSrc: a minimal one-transition workflow with a human
// assignee, just enough to exercise ensureWorkflowTasks (workflow.go:494)
// without pulling in the full collections example.
const workflowAssigneeSrc = `
entity Doc:
    title: string required
    status: enum[Draft, Review] default=Draft

workflow Doc on status:
    Draft -> Review: submit assignee=Reviewer

roles:
    Owner
    Reviewer

permissions:
    Owner:
        full [Doc]
    Reviewer:
        act  [submit]
        read [Doc]
`

// failTaskCreateStore wraps a Store and makes every task.created Append fail
// while armed — simulating a real Append failure (store down / validation),
// never an idempotent duplicate (MemStore's own dedup lives below this
// wrapper and is never reached while armed).
type failTaskCreateStore struct {
	eventstore.Store
	armed bool
}

var errSimulatedStoreFailure = errors.New("simulated store failure")

func (s *failTaskCreateStore) Append(ctx context.Context, in eventstore.AppendInput) (*eventstore.Event, error) {
	if s.armed && in.Kind == eventstore.TaskCreated {
		return nil, errSimulatedStoreFailure
	}
	return s.Store.Append(ctx, in)
}

func workflowAssigneeEngine(t *testing.T, store eventstore.Store) *Engine {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"t.dsl": workflowAssigneeSrc})
	if len(errs) > 0 {
		t.Fatalf("compile: %v", errs[0])
	}
	e, err := New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// captureLog redirects the standard logger for the duration of the test and
// returns the buffer it writes into (tasks.go's failure branch uses the
// package-level log.Printf, same as automation.go's CRITICAL lines).
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return &buf
}

// A real createTask failure inside ensureWorkflowTasks must not be swallowed:
// it has to surface as an observable signal (CRITICAL log line + the shared
// AutomationJournalFailures counter automation.go already uses for "could not
// journal" / "could not create operator task" failures), and the record
// creation itself must still succeed — this is a background task-queueing
// step, not a reason to fail the transition that already landed.
func TestEnsureWorkflowTasksSurfacesRealFailure(t *testing.T) {
	buf := captureLog(t)
	store := &failTaskCreateStore{Store: eventstore.NewMemStore(nil), armed: true}
	e := workflowAssigneeEngine(t, store)

	before := AutomationJournalFailures.Load()
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	doc, err := e.Create(ctx, owner, "Doc", map[string]any{"title": "x"}, basis, "")
	if err != nil {
		t.Fatalf("Create must still succeed even though the follow-up task creation fails: %v", err)
	}

	if got := AutomationJournalFailures.Load() - before; got != 1 {
		t.Fatalf("AutomationJournalFailures must increase by 1 on a real createTask failure, got delta %d", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte("CRITICAL")) || !bytes.Contains(buf.Bytes(), []byte("Doc")) ||
		!bytes.Contains(buf.Bytes(), []byte(doc.ID)) || !bytes.Contains(buf.Bytes(), []byte("Reviewer")) {
		t.Fatalf("expected a CRITICAL log line naming entity/record/assignee, got: %q", buf.String())
	}
	if tasks := e.Tasks("Reviewer", ""); len(tasks) != 0 {
		t.Fatalf("no task should exist after a failed createTask, got %d", len(tasks))
	}
}

// The idempotent path must stay quiet: replaying ensureWorkflowTasks for a
// record that already got its task (same current state, same "since") hits
// createTask's idempotent-dup branch (nil error, same task id — see the
// comment on createTask) and must not add noise: no new log line, no counter
// bump, no second task.
func TestEnsureWorkflowTasksIdempotentRepeatIsQuiet(t *testing.T) {
	buf := captureLog(t)
	store := eventstore.NewMemStore(nil)
	e := workflowAssigneeEngine(t, store)

	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	doc, err := e.Create(ctx, owner, "Doc", map[string]any{"title": "x"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if tasks := e.Tasks("Reviewer", ""); len(tasks) != 1 {
		t.Fatalf("want exactly 1 task after creation, got %d", len(tasks))
	}

	before := AutomationJournalFailures.Load()
	buf.Reset()

	// Same entity/id, unchanged state → same idemKey as the call already made
	// inside Create's runAutoTransitions: a genuine idempotent replay, not a
	// fresh attempt.
	e.ensureWorkflowTasks(ctx, "Doc", doc.ID)

	if got := AutomationJournalFailures.Load() - before; got != 0 {
		t.Fatalf("idempotent repeat must not bump AutomationJournalFailures, got delta %d", got)
	}
	if buf.Len() != 0 {
		t.Fatalf("idempotent repeat must not log anything, got: %q", buf.String())
	}
	if tasks := e.Tasks("Reviewer", ""); len(tasks) != 1 {
		t.Fatalf("idempotent repeat must not create a second task, got %d", len(tasks))
	}
}

// failExpireStore makes task.expired Appends fail while armed.
type failExpireStore struct {
	eventstore.Store
	armed bool
}

func (s *failExpireStore) Append(ctx context.Context, in eventstore.AppendInput) (*eventstore.Event, error) {
	if s.armed && in.Kind == eventstore.TaskExpired {
		return nil, errSimulatedStoreFailure
	}
	return s.Store.Append(ctx, in)
}

// A lease expiry that cannot be journaled must not reopen the task in memory
// (AS_IS defect 2): the take fails with the store error, the lease stays with
// its holder, Tick counts the failure, and once the store recovers the expiry
// is journaled and the task is takeable again.
func TestExpireIfDueKeepsLeaseWhenJournalFails(t *testing.T) {
	store := &failExpireStore{Store: eventstore.NewMemStore(nil)}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	model, errs := dsl.Compile(map[string]string{"t.dsl": workflowAssigneeSrc})
	if len(errs) > 0 {
		t.Fatalf("compile: %v", errs[0])
	}
	e, err := New(ctx, model, store, WithClock(func() time.Time { return now }), WithTaskTTL(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(ctx, owner, "Doc", map[string]any{"title": "x"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	tasks := e.Tasks("Reviewer", TaskOpen)
	if len(tasks) != 1 {
		t.Fatalf("want one Reviewer task, got %d", len(tasks))
	}
	tid := tasks[0].ID
	r1 := eventstore.Actor{Type: eventstore.ActorHuman, ID: "r1", Role: "Reviewer"}
	r2 := eventstore.Actor{Type: eventstore.ActorHuman, ID: "r2", Role: "Reviewer"}
	if _, err := e.TakeTask(ctx, r1, tid, "take-r1-0001"); err != nil {
		t.Fatal(err)
	}

	now = now.Add(2 * time.Minute) // the lease is overdue
	store.armed = true
	if _, err := e.TakeTask(ctx, r2, tid, "take-r2-0001"); !errors.Is(err, errSimulatedStoreFailure) {
		t.Fatalf("take over an unjournaled expiry must fail with the store error, got %v", err)
	}
	before := AutomationJournalFailures.Load()
	captureLog(t)
	if err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if AutomationJournalFailures.Load() <= before {
		t.Fatal("Tick must count the unjournaled expiry")
	}
	e.mu.RLock()
	status, holder := e.tasks[tid].Status, e.tasks[tid].TakenBy
	e.mu.RUnlock()
	if status != TaskTaken || holder != "r1" {
		t.Fatalf("memory moved without the journal: status=%s holder=%s", status, holder)
	}

	store.armed = false
	if _, err := e.TakeTask(ctx, r2, tid, "take-r2-0002"); err != nil {
		t.Fatalf("after recovery the expired task must be takeable: %v", err)
	}
}
