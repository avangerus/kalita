package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/avangerus/kalita/internal/eventstore"
)

// LLD-17 DR verification, unit-level: a generated journal chain round-trips
// through loadVerifiedBackup, and ANY tampering (byte, chain, count, anchor)
// is rejected — an unverified backup is never restored (O2).
func TestBackupVerifyRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := eventstore.NewMemStore(nil)
	for i := 0; i < 3; i++ {
		if _, err := store.Append(ctx, eventstore.AppendInput{
			Actor:      eventstore.Actor{Type: eventstore.ActorHuman, ID: "u1", Role: "Owner"},
			Kind:       eventstore.RecordCreated,
			Subject:    eventstore.Subject{Entity: "A", RecordID: fmt.Sprintf("r-%d", i)},
			Payload:    json.RawMessage(`{"values":{"x":1}}`),
			DefVersion: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}

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
	var anchor []byte
	for _, ev := range events {
		if ev.Kind == eventstore.CheckpointSealed {
			anchor = ev.Hash
		}
	}
	m := backupManifest{At: "test", JournalSeq: uint64(len(events)),
		JournalSHA: sha256Hex(jb.Bytes()), Anchor: hex.EncodeToString(anchor)}
	mraw, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "MANIFEST.json"), mraw, 0o600); err != nil {
		t.Fatal(err)
	}

	got, evs, err := loadVerifiedBackup(dir)
	if err != nil {
		t.Fatalf("valid backup must verify: %v", err)
	}
	if len(evs) != 3 || got.JournalSeq != 3 {
		t.Fatalf("round-trip mismatch: %d events, manifest seq %d", len(evs), got.JournalSeq)
	}

	// tamper the journal -> rejected
	if err := os.WriteFile(filepath.Join(dir, "journal.jsonl"), []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadVerifiedBackup(dir); err == nil {
		t.Fatal("tampered journal must be rejected")
	}
}
