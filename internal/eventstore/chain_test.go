package eventstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

func testClock() func() time.Time {
	t := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	return func() time.Time {
		t = t.Add(time.Second)
		return t
	}
}

func appendN(t *testing.T, s *MemStore, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		payload, _ := json.Marshal(map[string]any{"field": "debt", "old": i, "new": i + 1})
		_, err := s.Append(AppendInput{
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

// EVENT-STORE-v0 §7.1: the chain over a long mixed journal verifies clean.
func TestChainAppendAndVerify(t *testing.T) {
	s := NewMemStore(testClock())
	appendN(t, s, 1000)
	if err := s.Verify(); err != nil {
		t.Fatalf("chain must verify: %v", err)
	}
	events := s.All()
	if events[0].Seq != 1 || events[999].Seq != 1000 {
		t.Fatalf("seq must be gapless 1..1000, got %d..%d", events[0].Seq, events[999].Seq)
	}
}

// EVENT-STORE-v0 §7.2: altering any event in the middle breaks verification
// at exactly that event.
func TestTamperDetection(t *testing.T) {
	s := NewMemStore(testClock())
	appendN(t, s, 100)
	events := s.All()

	events[41].Payload = json.RawMessage(`{"field":"debt","old":0,"new":999999}`)

	err := VerifyChain(events)
	if err == nil {
		t.Fatal("tampered chain must not verify")
	}
	var brk *ChainBreakError
	if !errors.As(err, &brk) {
		t.Fatalf("want ChainBreakError, got %T: %v", err, err)
	}
	if brk.Seq != 42 {
		t.Fatalf("break must point at seq 42 (the altered event), got %d", brk.Seq)
	}
}

// Deleting an event from the middle is detected as a sequence/hash break.
func TestDeletionDetection(t *testing.T) {
	s := NewMemStore(testClock())
	appendN(t, s, 10)
	events := s.All()
	cut := append(events[:4:4], events[5:]...) // drop seq 5

	if err := VerifyChain(cut); err == nil {
		t.Fatal("chain with a deleted event must not verify")
	}
}

// EVENT-STORE-v0 §7.4: repeating a mutation with the same idempotency key
// returns the original event and appends nothing.
func TestIdempotency(t *testing.T) {
	s := NewMemStore(testClock())
	in := AppendInput{
		Actor:          Actor{Type: ActorAgent, ID: "collector-1", Role: "Collector"},
		Kind:           RecordCreated,
		Subject:        Subject{Entity: "Debtor", RecordID: "d-1"},
		Payload:        json.RawMessage(`{"company":"Vector LLC"}`),
		Basis:          &Basis{Type: "task", ID: "t-1"},
		DefVersion:     1,
		IdempotencyKey: "create-d-1",
	}
	first, err := s.Append(in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Append(in)
	if err != nil {
		t.Fatal(err)
	}
	if first.EventID != second.EventID {
		t.Fatal("same idempotency key must return the original event")
	}
	if got := len(s.All()); got != 1 {
		t.Fatalf("journal must contain exactly 1 event, got %d", got)
	}
	if err := s.Verify(); err != nil {
		t.Fatal(err)
	}
}

// Anonymous actors do not exist (MCP-CONTRACT-v0 §0.1).
func TestEmptyActorRejected(t *testing.T) {
	s := NewMemStore(testClock())
	_, err := s.Append(AppendInput{Kind: NodeStarted})
	if !errors.Is(err, ErrEmptyActorID) {
		t.Fatalf("want ErrEmptyActorID, got %v", err)
	}
}
