package eventstore

import (
	"crypto/ed25519"
	"encoding/json"
	"strings"
	"testing"
)

func nodeKeys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// EVENT-STORE-v0 §3: a sealed head verifies offline with only the journal and
// the node public key — no access to the running node required.
func TestSealAndVerifyCheckpoints(t *testing.T) {
	pub, priv := nodeKeys(t)
	s := NewMemStore(testClock())

	appendN(t, s, 50)
	if _, err := Seal(ctx, s, "node-1", priv); err != nil {
		t.Fatal(err)
	}
	appendN(t, s, 30)
	if _, err := Seal(ctx, s, "node-1", priv); err != nil {
		t.Fatal(err)
	}

	events := mustAll(t, s)
	if err := VerifyCheckpoints(events, pub); err != nil {
		t.Fatalf("checkpoints must verify: %v", err)
	}
	if err := VerifyChain(events); err != nil {
		t.Fatalf("chain must still verify with checkpoints inside: %v", err)
	}
}

// A checkpoint signed by a different key is rejected.
func TestCheckpointWrongKey(t *testing.T) {
	_, priv := nodeKeys(t)
	otherPub, _ := nodeKeys(t)
	s := NewMemStore(testClock())
	appendN(t, s, 5)
	if _, err := Seal(ctx, s, "node-1", priv); err != nil {
		t.Fatal(err)
	}
	if err := VerifyCheckpoints(mustAll(t, s), otherPub); err == nil {
		t.Fatal("checkpoint signed by another key must not verify")
	}
}

// A checkpoint whose sealed hash no longer matches the journal is rejected —
// this is what catches a rewritten-then-rechained journal.
func TestCheckpointCatchesRewrittenJournal(t *testing.T) {
	pub, priv := nodeKeys(t)
	s := NewMemStore(testClock())
	appendN(t, s, 5)
	if _, err := Seal(ctx, s, "node-1", priv); err != nil {
		t.Fatal(err)
	}

	events := mustAll(t, s)
	var cp CheckpointPayload
	if err := json.Unmarshal(events[5].Payload, &cp); err != nil {
		t.Fatal(err)
	}
	cp.SealedHash[0] ^= 0xFF // journal rewritten: sealed hash no longer matches
	tampered, _ := json.Marshal(cp)
	events[5].Payload = tampered

	err := VerifyCheckpoints(events, pub)
	if err == nil || !strings.Contains(err.Error(), "seq") {
		t.Fatalf("rewritten journal must fail checkpoint verification, got: %v", err)
	}
}

// Sealing an empty journal is an explicit error.
func TestSealEmptyJournal(t *testing.T) {
	_, priv := nodeKeys(t)
	s := NewMemStore(testClock())
	if _, err := Seal(ctx, s, "node-1", priv); err != ErrNothingToSeal {
		t.Fatalf("want ErrNothingToSeal, got %v", err)
	}
}
