package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/avangerus/kalita/internal/eventstore"
)

// kalita backup / kalita backup verify — LLD-17 O2/O3, day-2 DR automation.
//
// A backup is a CONSISTENT snapshot of the node's three stores:
//   - journal.jsonl  — every journal event, one per line (the immutable truth)
//   - files/         — the content-addressed blob store (optional)
//   - pii-keys.json  — the crypto-shredding key store (optional, LLD-16)
//
// plus MANIFEST.json with sha256 of every component and the ANCHOR: the last
// checkpoint.sealed event's hash. Storing the anchor off-node (client backup,
// TSA, notary) is the external proof that the journal was not rewritten
// (O3) — `backup verify` re-checks the chain and the anchor, so an untested
// backup never masquerades as a good one (O2: "непроверенный бэкап =
// отсутствующий").

type backupManifest struct {
	At         string            `json:"at"`
	JournalSeq uint64            `json:"journal_seq"`
	JournalSHA string            `json:"journal_sha256"`
	Anchor     string            `json:"checkpoint_anchor"`
	Components map[string]string `json:"components,omitempty"` // component -> sha256
}

func backupCmd(args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "verify":
			backupVerifyCmd(args[1:])
			return
		case "restore":
			backupRestoreCmd(args[1:])
			return
		}
	}
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	out := fs.String("out", "", "backup directory (required)")
	filesDir := fs.String("files", "", "blob store directory to include (optional)")
	piiKeys := fs.String("pii-keys", "", "pii key store file to include (optional)")
	_ = fs.Parse(args)
	if *out == "" {
		log.Fatal("--out is required")
	}
	dsn := resolveDSN()
	if dsn == "" {
		log.Fatal("KALITA_PG_DSN is required: a backup snapshots the journal")
	}
	ctx := context.Background()
	store, err := eventstore.NewPGStore(ctx, dsn, os.Getenv("KALITA_PG_SCHEMA"), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	events, err := store.All(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		log.Fatal(err)
	}

	var jb bytes.Buffer
	for _, ev := range events {
		line, err := json.Marshal(ev)
		if err != nil {
			log.Fatal(err)
		}
		jb.Write(line)
		jb.WriteByte('\n')
	}
	journalPath := filepath.Join(*out, "journal.jsonl")
	if err := os.WriteFile(journalPath, jb.Bytes(), 0o600); err != nil {
		log.Fatal(err)
	}

	// ANCHOR (single semantics, shared with `kalita anchor` — see
	// lastCheckpointSealed): the last checkpoint.sealed event's own chain hash.
	// An empty journal without a checkpoint yields an empty anchor, which is
	// legal for a backup but should be stored off-node only once a checkpoint
	// exists (the anchor is what proves the journal was not rewritten, O3).
	var anchor []byte
	if cp := lastCheckpointSealed(events); cp != nil {
		anchor = cp.Hash
	}

	comps := map[string]string{}
	if *filesDir != "" {
		dest := filepath.Join(*out, "files")
		if err := copyTree(*filesDir, dest); err != nil {
			log.Fatal(err)
		}
		comps["files"] = sha256Tree(dest)
	}
	if *piiKeys != "" {
		dest := filepath.Join(*out, "pii-keys.json")
		raw, err := os.ReadFile(*piiKeys)
		if err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(dest, raw, 0o600); err != nil {
			log.Fatal(err)
		}
		comps["pii-keys.json"] = sha256Hex(raw)
	}

	m := backupManifest{
		At:         time.Now().UTC().Format(time.RFC3339),
		JournalSeq: uint64(len(events)),
		JournalSHA: sha256Hex(jb.Bytes()),
		Anchor:     hex.EncodeToString(anchor),
		Components: comps,
	}
	mraw, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(*out, "MANIFEST.json"), mraw, 0o600); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("backup: %d events -> %s (journal sha256 %s)\n", len(events), *out, m.JournalSHA)
	fmt.Printf("ANCHOR — store this off-node (client backup/TSA/notary) to prove the journal was not rewritten:\n%s\n", m.Anchor)
	fmt.Printf("restore: point a fresh node at this journal.jsonl, then `kalita verify --file %s --pub <node.pub>`\n", *out)
}

func backupVerifyCmd(args []string) {
	fs := flag.NewFlagSet("backup verify", flag.ExitOnError)
	dir := fs.String("dir", "", "backup directory (required)")
	_ = fs.Parse(args)
	if *dir == "" {
		log.Fatal("--dir is required")
	}

	m, events, err := loadVerifiedBackup(*dir)
	if err != nil {
		log.Fatal(err)
	}
	for name, want := range m.Components {
		if got := sha256File(filepath.Join(*dir, name)); got != want {
			log.Fatalf("component %s sha256 mismatch", name)
		}
	}
	anchorShort := m.Anchor
	if len(anchorShort) > 16 {
		anchorShort = anchorShort[:16]
	}
	fmt.Printf("backup verify OK: %d events, hash chain intact, anchor %s\n", len(events), anchorShort)
}

// backupRestoreCmd — LLD-17 DR: load a verified backup into a FRESH journal.
// The backup must verify (sha + chain + anchor) before a single event is
// restored; the target journal must be empty. After restore, starting the node
// replays the journal into its projection.
func backupRestoreCmd(args []string) {
	fs := flag.NewFlagSet("backup restore", flag.ExitOnError)
	dir := fs.String("dir", "", "backup directory (required)")
	targetDSN := fs.String("dsn", "", "target postgres DSN (defaults to KALITA_PG_DSN)")
	_ = fs.Parse(args)
	if *dir == "" {
		log.Fatal("--dir is required")
	}
	dsn := *targetDSN
	if dsn == "" {
		dsn = resolveDSN()
	}
	if dsn == "" {
		log.Fatal("KALITA_PG_DSN is required: restore targets a fresh journal")
	}

	m, events, err := loadVerifiedBackup(*dir)
	if err != nil {
		log.Fatal(err)
	}
	_ = m

	ctx := context.Background()
	store, err := eventstore.NewPGStore(ctx, dsn, os.Getenv("KALITA_PG_SCHEMA"), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Restore(ctx, events); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("restore: %d events loaded into %s — start the node; it will replay the journal\n", len(events), dsn)
}

// loadVerifiedBackup reads MANIFEST.json + journal.jsonl and verifies the
// whole backup (sha256, hash chain, checkpoint anchor, event count). An
// unverified backup is never restored (O2) — callers fail loud on the error.
func loadVerifiedBackup(dir string) (backupManifest, []*eventstore.Event, error) {
	mraw, err := os.ReadFile(filepath.Join(dir, "MANIFEST.json"))
	if err != nil {
		return backupManifest{}, nil, err
	}
	var m backupManifest
	if err := json.Unmarshal(mraw, &m); err != nil {
		return backupManifest{}, nil, fmt.Errorf("manifest: %w", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		return backupManifest{}, nil, err
	}
	if got := sha256Hex(raw); got != m.JournalSHA {
		return backupManifest{}, nil, fmt.Errorf("journal.jsonl sha256 mismatch: got %s want %s", got, m.JournalSHA)
	}

	var events []*eventstore.Event
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		var ev eventstore.Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			return backupManifest{}, nil, fmt.Errorf("journal.jsonl line: %w", err)
		}
		events = append(events, &ev)
	}
	if err := sc.Err(); err != nil {
		return backupManifest{}, nil, err
	}
	if err := eventstore.VerifyChain(events); err != nil {
		return backupManifest{}, nil, fmt.Errorf("journal hash chain BROKEN: %w", err)
	}

	// ANCHOR (single semantics, shared with `kalita anchor` — see
	// lastCheckpointSealed): the last checkpoint.sealed event's own chain hash.
	var anchor []byte
	if cp := lastCheckpointSealed(events); cp != nil {
		anchor = cp.Hash
	}
	if hex.EncodeToString(anchor) != m.Anchor {
		return backupManifest{}, nil, fmt.Errorf("checkpoint anchor mismatch: manifest %s vs journal %s", m.Anchor, hex.EncodeToString(anchor))
	}
	if len(events) != int(m.JournalSeq) {
		return backupManifest{}, nil, fmt.Errorf("event count mismatch: manifest %d vs journal %d", m.JournalSeq, len(events))
	}
	return m, events, nil
}

// --- helpers -----------------------------------------------------------------

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func sha256File(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func sha256Tree(root string) string {
	h := sha256.New()
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		h.Write([]byte(rel))
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, _ = io.Copy(h, f)
		f.Close()
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

func copyTree(src, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o600)
	})
}
