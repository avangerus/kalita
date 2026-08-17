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

// Regression: `kalita verify` on an empty journal used to panic on
// events[len(events)-1]. The check lives in verifyHeadSeq (extracted because
// verifyCmd itself needs postgres); the empty journal must produce the
// explicit error, and the head seq must come from the last event otherwise.
func TestVerifyHeadSeq(t *testing.T) {
	if _, err := verifyHeadSeq(nil); err == nil || err.Error() != "journal is empty, nothing to verify" {
		t.Fatalf("nil journal: got %v, want explicit empty-journal error", err)
	}
	if _, err := verifyHeadSeq([]*eventstore.Event{}); err == nil || err.Error() != "journal is empty, nothing to verify" {
		t.Fatalf("empty journal: got %v, want explicit empty-journal error", err)
	}

	events := []*eventstore.Event{{Seq: 1}, {Seq: 2}, {Seq: 42}}
	seq, err := verifyHeadSeq(events)
	if err != nil {
		t.Fatalf("non-empty journal: unexpected error %v", err)
	}
	if seq != 42 {
		t.Fatalf("head seq = %d, want 42 (last event)", seq)
	}
}

// writeBackupFixture builds a minimal valid backup directory (journal.jsonl +
// MANIFEST.json) the same way `kalita backup` does, so verify --file can be
// tested without postgres (OPS-2, Cycle-A3).
func writeBackupFixture(t *testing.T) string {
	t.Helper()
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
	return dir
}

// TestLoadVerifyEventsFile: `verify --file <dir>` reads a backup through
// loadVerifiedBackup (sha256 + chain + anchor), rejects a tampered journal,
// and errors cleanly (no panic) when the directory has no MANIFEST.json.
func TestLoadVerifyEventsFile(t *testing.T) {
	dir := writeBackupFixture(t)

	evs, err := loadVerifyEvents(dir)
	if err != nil {
		t.Fatalf("valid backup must load: %v", err)
	}
	if len(evs) != 3 {
		t.Fatalf("loaded %d events, want 3", len(evs))
	}

	if err := os.WriteFile(filepath.Join(dir, "journal.jsonl"), []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadVerifyEvents(dir); err == nil {
		t.Fatal("tampered journal must fail verify --file")
	}

	empty := t.TempDir()
	if _, err := loadVerifyEvents(empty); err == nil {
		t.Fatal("a directory without MANIFEST.json must fail cleanly (no panic)")
	}
}

// TestUpgradeInsecureSkipTLSVerifyDefault: SEC-3 — the TLS-verification bypass
// must default OFF; a self-signed deployment opts in explicitly.
func TestUpgradeInsecureSkipTLSVerifyDefault(t *testing.T) {
	if upgradeInsecureSkipTLSVerifyDefault {
		t.Fatal("--insecure-skip-tls-verify must default to false (SEC-3)")
	}
}
