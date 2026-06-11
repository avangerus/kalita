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
