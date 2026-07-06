package eventstore

import (
	"context"
	"encoding/hex"
	"testing"
	"time"
)

// TestEraChain is acceptance test 4 of LLD-03-upgrade-eras.md §6: export
// (here: seal + take the final chain state) then a byte-for-byte-altered old
// dump must fail era verify, while the untouched chain must pass.
func TestEraChain(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore(testClock())
	for i := 0; i < 3; i++ {
		if _, err := s.Append(ctx, AppendInput{
			Actor: Actor{Type: ActorHuman, ID: "a"}, Kind: RecordCreated,
			Subject: Subject{Entity: "Doc", RecordID: "d"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	manifest := EraManifest{
		EraID:                     "era-1",
		SourceFinalSeq:            last.Seq,
		SourceFinalCheckpointHash: hex.EncodeToString(last.Hash),
		DefVersion:                1,
		NodeID:                    "node-1",
		ExportedAt:                time.Now(),
	}

	// The untouched chain must verify cleanly against its own manifest.
	if err := VerifyEraChain(events, manifest); err != nil {
		t.Fatalf("unaltered old era must verify: %v", err)
	}

	// Flipping one byte of the OLD dump's payload anywhere in the chain must
	// make verify fail (not just at the tail).
	tampered := make([]*Event, len(events))
	for i, e := range events {
		cp := *e
		tampered[i] = &cp
	}
	mid := tampered[0]
	altered := *mid
	altered.Payload = append([]byte(nil), mid.Payload...)
	if len(altered.Payload) > 0 {
		altered.Payload[0] ^= 0xFF
	} else {
		altered.Payload = []byte(`{"x":1}`)
	}
	tampered[0] = &altered

	if err := VerifyEraChain(tampered, manifest); err == nil {
		t.Fatal("a single altered byte in the old dump must fail era verify")
	}

	// A manifest pointing at the wrong final hash must also fail, even over
	// an otherwise-valid chain.
	wrongManifest := manifest
	wrongManifest.SourceFinalCheckpointHash = hex.EncodeToString([]byte("not-the-real-hash-000000000000"))
	if err := VerifyEraChain(events, wrongManifest); err == nil {
		t.Fatal("a manifest hash mismatch must fail era verify")
	}
}

func TestEraRowCounts(t *testing.T) {
	rows := []EraRow{
		{Entity: "Doc", ID: "1"},
		{Entity: "Doc", ID: "2"},
		{Entity: "Case", ID: "3"},
	}
	counts := EraRowCounts(rows)
	if counts["Doc"] != 2 || counts["Case"] != 1 {
		t.Fatalf("wrong counts: %+v", counts)
	}
}
