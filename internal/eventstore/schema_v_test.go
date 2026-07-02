package eventstore

import (
	"context"
	"strings"
	"testing"
)

// LLD-01 §1.1: schema_v is an additive envelope field. The canonicalization
// rule is the whole point of I-1 here — it must be invisible on the wire (and
// therefore in the hash) for every event that does not set it.

// TestSchemaVOmittedWhenZero: an event with SchemaV 0 (the default — every
// event that predates this field) must not carry "schema_v" in its canonical
// form at all, so its hash is bit-identical to what it would have been before
// this field existed.
func TestSchemaVOmittedWhenZero(t *testing.T) {
	e := &Event{EventID: "e1", Kind: RecordCreated, Actor: Actor{Type: ActorHuman, ID: "a"}}
	canon, err := canonicalJSON(e)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(canon), "schema_v") {
		t.Fatalf("schema_v must be entirely absent when 0, got: %s", canon)
	}
}

// TestSchemaVPresentWhenSet: an LLD-01 event (SchemaV 1) does carry the field.
func TestSchemaVPresentWhenSet(t *testing.T) {
	e := &Event{EventID: "e1", Kind: OpBegin, SchemaV: 1, Actor: Actor{Type: ActorSystem, ID: "engine"}}
	canon, err := canonicalJSON(e)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(canon), `"schema_v":1`) {
		t.Fatalf("schema_v:1 must be present in the canonical form, got: %s", canon)
	}
}

// TestSchemaVDoesNotAffectHashWhenZero: SchemaV 0 (unset) produces the exact
// same hash as an event built before this field existed — verified by
// comparing two otherwise-identical envelopes, one with the zero value set
// explicitly and one left at its Go default; both must canonicalize (and thus
// hash) identically. This is the concrete form of I-1 for this change.
func TestSchemaVDoesNotAffectHashWhenZero(t *testing.T) {
	base := Event{EventID: "e1", Seq: 1, Kind: RecordCreated,
		Actor: Actor{Type: ActorHuman, ID: "a"}, DefVersion: 1}
	withZero := base
	withZero.SchemaV = 0

	c1, err := canonicalJSON(&base)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := canonicalJSON(&withZero)
	if err != nil {
		t.Fatal(err)
	}
	if string(c1) != string(c2) {
		t.Fatalf("explicit SchemaV:0 must canonicalize identically to the unset default:\n%s\n%s", c1, c2)
	}

	h1, err := computeHash(genesisHash, &base)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := computeHash(genesisHash, &withZero)
	if err != nil {
		t.Fatal(err)
	}
	if string(h1) != string(h2) {
		t.Fatal("hash must be identical for SchemaV 0 regardless of how it got there")
	}
}

// TestSchemaVRoundTripsThroughAppend: MemStore.Append propagates
// AppendInput.SchemaV onto the stored event and its hash, end to end — not
// just the canonicalJSON helper in isolation.
func TestSchemaVRoundTripsThroughAppend(t *testing.T) {
	s := NewMemStore(testClock())
	ev, err := s.Append(context.Background(), AppendInput{
		Actor: Actor{Type: ActorSystem, ID: "engine"},
		Kind:  AutomationFailed, SchemaV: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ev.SchemaV != 1 {
		t.Fatalf("want SchemaV 1 on the stored event, got %d", ev.SchemaV)
	}
	if err := s.Verify(context.Background()); err != nil {
		t.Fatalf("chain with a schema_v:1 event must still verify: %v", err)
	}
}

// TestSchemaVMixedChainVerifies: a chain mixing schema_v 0 and 1 events
// verifies cleanly — the field does not disturb hash-chain mechanics.
func TestSchemaVMixedChainVerifies(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore(testClock())
	for i := 0; i < 5; i++ {
		if _, err := s.Append(ctx, AppendInput{
			Actor: Actor{Type: ActorHuman, ID: "a"}, Kind: RecordCreated,
			Subject: Subject{Entity: "Doc", RecordID: "d"},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Append(ctx, AppendInput{
			Actor: Actor{Type: ActorSystem, ID: "engine"}, Kind: OpBegin, SchemaV: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Verify(ctx); err != nil {
		t.Fatalf("mixed schema_v chain must verify: %v", err)
	}
}
