package eventstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// PG tests run only when a database is provided:
//
//	KALITA_TEST_PG_DSN=postgres://user:pass@host:port/db go test ./...
//
// They use a dedicated schema `kalita_test`, dropped and recreated per run.
func newTestPG(t *testing.T) *PGStore {
	t.Helper()
	dsn := os.Getenv("KALITA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("KALITA_TEST_PG_DSN not set")
	}
	ctx := context.Background()

	boot, err := NewPGStore(ctx, dsn, "kalita_test_boot", nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := boot.pool.Exec(ctx, `DROP SCHEMA IF EXISTS kalita_test CASCADE`); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	boot.Close()

	s, err := NewPGStore(ctx, dsn, "kalita_test", testClock())
	if err != nil {
		t.Fatalf("init store: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func pgAppendN(t *testing.T, s *PGStore, n int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		payload, _ := json.Marshal(map[string]any{"field": "debt", "old": i, "new": i + 1})
		_, err := s.Append(ctx, AppendInput{
			Actor:      Actor{Type: ActorAgent, ID: "collector-1", Role: "Collector"},
			Kind:       RecordUpdated,
			Subject:    Subject{Entity: "Debtor", RecordID: fmt.Sprintf("d-%d", i%7)},
			Payload:    payload,
			Basis:      &Basis{Type: "rule", ID: "overdue-reminder"},
			DefVersion: 1,
		})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
}

// EVENT-STORE-v0 §7.1 against the real database, including the write-read
// round trip: events read back from PostgreSQL must hash identically.
func TestPGRoundTripVerify(t *testing.T) {
	s := newTestPG(t)
	pgAppendN(t, s, 200)
	if err := s.Verify(context.Background()); err != nil {
		t.Fatalf("round-trip chain must verify: %v", err)
	}
	events, err := s.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if events[0].Seq != 1 || events[len(events)-1].Seq != 200 {
		t.Fatalf("gapless 1..200 expected, got %d..%d", events[0].Seq, events[len(events)-1].Seq)
	}
}

// EVENT-STORE-v0 §0.1: the database itself rejects UPDATE and DELETE on the
// journal, regardless of who connects.
func TestPGImmutability(t *testing.T) {
	s := newTestPG(t)
	pgAppendN(t, s, 5)
	ctx := context.Background()

	_, err := s.pool.Exec(ctx, `UPDATE events SET payload = '"tampered"' WHERE seq = 3`)
	if err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("UPDATE must be rejected by trigger, got: %v", err)
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM events WHERE seq = 3`)
	if err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("DELETE must be rejected by trigger, got: %v", err)
	}
	if err := s.Verify(ctx); err != nil {
		t.Fatalf("chain intact after rejected mutations: %v", err)
	}
}

// EVENT-STORE-v0 §7.4 against the real database.
func TestPGIdempotency(t *testing.T) {
	s := newTestPG(t)
	ctx := context.Background()
	in := AppendInput{
		Actor:          Actor{Type: ActorAgent, ID: "collector-1", Role: "Collector"},
		Kind:           RecordCreated,
		Subject:        Subject{Entity: "Debtor", RecordID: "d-1"},
		Payload:        json.RawMessage(`{"company":"Vector LLC"}`),
		Basis:          &Basis{Type: "task", ID: "t-1"},
		DefVersion:     1,
		IdempotencyKey: "create-d-1",
	}
	first, err := s.Append(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Append(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if first.EventID != second.EventID {
		t.Fatal("same idempotency key must return the original event")
	}
	events, err := s.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("journal must contain exactly 1 event, got %d", len(events))
	}
}

// Concurrent appends must serialize without gaps or chain breaks.
func TestPGConcurrentAppends(t *testing.T) {
	s := newTestPG(t)
	// Real clock here: the deterministic test clock is not safe across goroutines.
	s.now = func() time.Time { return time.Now() }
	ctx := context.Background()

	const workers, each = 8, 25
	errc := make(chan error, workers)
	for w := 0; w < workers; w++ {
		go func(w int) {
			for i := 0; i < each; i++ {
				_, err := s.Append(ctx, AppendInput{
					Actor:      Actor{Type: ActorAgent, ID: fmt.Sprintf("agent-%d", w), Role: "Collector"},
					Kind:       RecordCreated,
					Subject:    Subject{Entity: "Debtor", RecordID: fmt.Sprintf("c-%d-%d", w, i)},
					DefVersion: 1,
				})
				if err != nil {
					errc <- err
					return
				}
			}
			errc <- nil
		}(w)
	}
	for w := 0; w < workers; w++ {
		if err := <-errc; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Verify(ctx); err != nil {
		t.Fatalf("chain must verify after concurrent appends: %v", err)
	}
	events, _ := s.All(ctx)
	if len(events) != workers*each {
		t.Fatalf("want %d events, got %d", workers*each, len(events))
	}
}

// TestPGRestartReplay — a record survives a node restart by pure journal
// replay: write a journal, close the store (node stops), reopen a fresh store
// over the same schema (node starts), and the chain verifies with no gaps.
func TestPGRestartReplay(t *testing.T) {
	dsn := os.Getenv("KALITA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("KALITA_TEST_PG_DSN not set")
	}
	ctx := context.Background()

	boot, err := NewPGStore(ctx, dsn, "kalita_restart_boot", nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := boot.pool.Exec(ctx, `DROP SCHEMA IF EXISTS kalita_restart CASCADE`); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	boot.Close()

	s1, err := NewPGStore(ctx, dsn, "kalita_restart", testClock())
	if err != nil {
		t.Fatalf("init store: %v", err)
	}
	pgAppendN(t, s1, 100)
	s1.Close() // the node stops

	// a fresh store over the same journal is the node restarting.
	s2, err := NewPGStore(ctx, dsn, "kalita_restart", testClock())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer s2.Close()
	if err := s2.Verify(ctx); err != nil {
		t.Fatalf("chain must verify after restart: %v", err)
	}
	events, err := s2.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 100 {
		t.Fatalf("replayed journal must have 100 events, got %d", len(events))
	}
	if events[0].Seq != 1 || events[99].Seq != 100 {
		t.Fatalf("gapless seq after restart, got %d..%d", events[0].Seq, events[99].Seq)
	}
}

// LIM-3: ForRecord returns exactly the events of one record, in order, and
// matches MemStore (the semantics oracle) and a filter over All.
func TestPGForRecordMatchesMemStore(t *testing.T) {
	s := newTestPG(t)
	ctx := context.Background()
	pgAppendN(t, s, 30)
	// extra subject keys still belong to the record, exactly as in a filter over All
	if _, err := s.Append(ctx, AppendInput{
		Actor: Actor{Type: ActorAgent, ID: "a", Role: "R"}, Kind: RecordUpdated,
		Subject: Subject{Entity: "Debtor", RecordID: "d-10", TaskID: "t-1"}, Payload: []byte(`{}`), DefVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}
	mem := NewMemStore(testClock())
	all, err := s.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range all {
		if _, err := mem.Append(ctx, AppendInput{Actor: ev.Actor, Kind: ev.Kind, Subject: ev.Subject,
			Payload: ev.Payload, Basis: ev.Basis, DefVersion: ev.DefVersion}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"d-0", "d-3", "d-10", "missing"} {
		got, err := s.ForRecord(ctx, "Debtor", id)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := mem.ForRecord(ctx, "Debtor", id)
		if len(got) != len(want) {
			t.Fatalf("%s: pg %d events, mem %d", id, len(got), len(want))
		}
		var last uint64
		for i, ev := range got {
			if ev.Subject.RecordID != id || ev.Seq <= last || ev.Seq != want[i].Seq {
				t.Fatalf("%s: event %d out of place: %+v", id, i, ev.Subject)
			}
			last = ev.Seq
		}
	}
}
