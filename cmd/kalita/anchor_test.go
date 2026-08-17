package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Anchor semantics (single, LLD-17): the checkpoint anchor is the LAST
// checkpoint.sealed event's OWN chain hash (ev.Hash) — the value `kalita
// backup` writes into MANIFEST.json checkpoint_anchor and `backup verify`
// re-checks. NOT payload.sealed_hash (the hash of the head at seal time),
// which lives inside the payload and is itself covered by ev.Hash's chain.
// This test pins the choice: the helper returns ev.Hash, and a manifest
// carrying the sealed_hash (the old `kalita anchor` semantics) is REJECTED by
// backup verify.
func TestCheckpointAnchorSemantics(t *testing.T) {
	ctx := context.Background()
	store := eventstore.NewMemStore(nil)
	for i := 0; i < 3; i++ {
		if _, err := store.Append(ctx, eventstore.AppendInput{
			Actor:      eventstore.Actor{Type: eventstore.ActorHuman, ID: "u1", Role: "Owner"},
			Kind:       eventstore.RecordCreated,
			Subject:    eventstore.Subject{Entity: "A", RecordID: "r"},
			Payload:    json.RawMessage(`{"values":{"i":1}}`),
			DefVersion: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cp, err := eventstore.Seal(ctx, store, "node", priv)
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}

	got := lastCheckpointSealed(events)
	if got == nil || got.EventID != cp.EventID {
		t.Fatalf("lastCheckpointSealed = %v, want the last checkpoint event %s", got, cp.EventID)
	}
	var p eventstore.CheckpointPayload
	if err := json.Unmarshal(cp.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got.Hash, p.SealedHash) {
		t.Fatal("test setup broken: checkpoint event hash must differ from its payload sealed_hash")
	}
	if !bytes.Equal(got.Hash, cp.Hash) {
		t.Fatalf("anchor must be the checkpoint event's own chain hash: got %x want %x", got.Hash, cp.Hash)
	}

	// a backup manifest carrying the chosen semantics verifies...
	dir := t.TempDir()
	var jb bytes.Buffer
	for _, ev := range events {
		line, _ := json.Marshal(ev)
		jb.Write(line)
		jb.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "journal.jsonl"), jb.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	writeManifest := func(anchor string) {
		m := backupManifest{At: "test", JournalSeq: uint64(len(events)),
			JournalSHA: sha256Hex(jb.Bytes()), Anchor: anchor}
		mraw, _ := json.Marshal(m)
		if err := os.WriteFile(filepath.Join(dir, "MANIFEST.json"), mraw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest(hex.EncodeToString(got.Hash))
	if _, _, err := loadVerifiedBackup(dir); err != nil {
		t.Fatalf("manifest anchored to the checkpoint event hash must verify: %v", err)
	}

	// ...and one carrying the OLD anchor.go semantics (payload sealed_hash) is
	// rejected — the single anchor semantics is the event's own chain hash.
	writeManifest(hex.EncodeToString(p.SealedHash))
	if _, _, err := loadVerifiedBackup(dir); err == nil {
		t.Fatal("manifest anchored to payload.sealed_hash must be rejected (anchor = checkpoint event's own hash)")
	}
}
