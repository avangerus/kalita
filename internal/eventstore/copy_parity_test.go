package eventstore

// §7.13 / LLD-12 §3,§5(3): MemStore must return COPIES of events from every
// read path, not pointers aliasing its own internal slice — otherwise a
// caller mutating a returned Event silently corrupts the journal, a bug
// PGStore can never exhibit (it always deserializes a fresh Event per query,
// sharing memory with nothing). assertMutationIsolated is the one scenario
// run against BOTH stores: same setup, same mutations, same "nothing moved"
// assertions — the Mem/PG parity LLD-12 test 3 asks for.

import (
	"bytes"
	"encoding/json"
	"testing"
)

func seedOneMutableEvent(t *testing.T, s Store) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"field": "debt", "old": 1, "new": 2})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Append(ctx, AppendInput{
		Actor:          Actor{Type: ActorAgent, ID: "collector-1", Role: "Collector", Attrs: map[string]any{"region": "eu"}},
		Kind:           RecordUpdated,
		Subject:        Subject{Entity: "Debtor", RecordID: "d-1"},
		Payload:        payload,
		Basis:          &Basis{Type: "rule", ID: "overdue-reminder"},
		DefVersion:     1,
		IdempotencyKey: "seed-1",
	})
	if err != nil {
		t.Fatal(err)
	}
}

// assertMutationIsolated mutates every reference field (Payload, Hash, Basis,
// Actor.Attrs) of Events returned by All, Since and ByIdemKey, plus the hash
// slice returned by Head, then re-reads the store and checks nothing moved.
// Shared by TestMemStoreReturnsCopies and the PG parity test below so both
// stores are held to literally the same check.
func assertMutationIsolated(t *testing.T, s Store) {
	t.Helper()

	before, err := s.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) == 0 {
		t.Fatal("test setup: expected at least one event")
	}
	wantPayload := append([]byte(nil), before[0].Payload...)
	wantHash := append([]byte(nil), before[0].Hash...)
	wantBasisID := before[0].Basis.ID

	mutate := func(e *Event) {
		if len(e.Payload) > 0 {
			e.Payload[0] ^= 0xFF
		}
		if len(e.Hash) > 0 {
			e.Hash[0] ^= 0xFF
		}
		if e.Basis != nil {
			e.Basis.ID = "MUTATED"
		}
		if e.Actor.Attrs != nil {
			e.Actor.Attrs["region"] = "MUTATED"
		}
	}

	all, err := s.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mutate(all[0])

	since, err := s.Since(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	mutate(since[0])

	byIdem, err := s.ByIdemKey(ctx, "seed-1")
	if err != nil {
		t.Fatal(err)
	}
	if byIdem == nil {
		t.Fatal("test setup: expected ByIdemKey(\"seed-1\") to find the seeded event")
	}
	mutate(byIdem)

	_, headHash, err := s.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(headHash) > 0 {
		headHash[0] ^= 0xFF
	}

	after, err := s.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after[0].Payload, wantPayload) {
		t.Fatalf("mutating a returned event's Payload corrupted the store: got %x, want %x", after[0].Payload, wantPayload)
	}
	if !bytes.Equal(after[0].Hash, wantHash) {
		t.Fatalf("mutating a returned event's Hash corrupted the store: got %x, want %x", after[0].Hash, wantHash)
	}
	if after[0].Basis == nil || after[0].Basis.ID != wantBasisID {
		t.Fatalf("mutating a returned event's Basis corrupted the store: got %+v, want ID=%q", after[0].Basis, wantBasisID)
	}
	if after[0].Actor.Attrs["region"] != "eu" {
		t.Fatalf("mutating a returned event's Actor.Attrs corrupted the store: got %v", after[0].Actor.Attrs)
	}
	afterIdem, err := s.ByIdemKey(ctx, "seed-1")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterIdem.Payload, wantPayload) {
		t.Fatalf("mutating the event returned by ByIdemKey corrupted the store: got %x, want %x", afterIdem.Payload, wantPayload)
	}
	_, headHash2, err := s.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(headHash2, wantHash) {
		t.Fatalf("mutating the hash returned by Head corrupted the store: got %x, want %x", headHash2, wantHash)
	}
}

// TestMemStoreReturnsCopies is the MemStore half of LLD-12 §5 test 3.
func TestMemStoreReturnsCopies(t *testing.T) {
	s := NewMemStore(testClock())
	seedOneMutableEvent(t, s)
	assertMutationIsolated(t, s)
}

// TestPGStoreReturnsCopies is the PGStore half: runs only when
// KALITA_TEST_PG_DSN is set (see newTestPG in pgstore_test.go). PGStore was
// never expected to fail this — every read deserializes a fresh Event from
// the row — but running the IDENTICAL scenario against both stores is the
// parity LLD-12 test 3 explicitly asks for ("один и тот же сценарий даёт
// идентичные проекции на MemStore и PGStore"), not just an assertion taken
// on faith for PG.
func TestPGStoreReturnsCopies(t *testing.T) {
	s := newTestPG(t)
	seedOneMutableEvent(t, s)
	assertMutationIsolated(t, s)
}
