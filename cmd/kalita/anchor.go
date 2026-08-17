package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/avangerus/kalita/internal/eventstore"
)

// kalita anchor — PROD-READINESS O3 / LLD-17: export the current checkpoint
// anchor to a file the operator stores OFF-node (client backup, RFC-3161 TSA
// timestamp, notary). Any later journal whose last checkpoint hash differs from
// the anchor was rewritten — provability that does not depend on our node.
func anchorCmd(args []string) {
	fs := flag.NewFlagSet("anchor", flag.ExitOnError)
	out := fs.String("out", "anchor.txt", "output file")
	tsaURL := fs.String("tsa", "", "RFC-3161 TSA URL — timestamp the anchor with a trusted third party (optional)")
	_ = fs.Parse(args)

	dsn := resolveDSN()
	if dsn == "" {
		log.Fatal("KALITA_PG_DSN is required: anchor reads the journal")
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
	cp := lastCheckpointSealed(events)
	if cp == nil {
		log.Fatal("no checkpoint.sealed in the journal — start the node once so it seals a checkpoint")
	}

	// Anchor semantics (single, shared with `kalita backup` / `backup verify`,
	// see lastCheckpointSealed): the anchor is the checkpoint event's OWN
	// chain hash — NOT payload.sealed_hash (the sealed HEAD hash), which lives
	// inside the payload and is itself covered by ev.Hash's chain.
	head := cp.Hash

	text := fmt.Sprintf(`kalita checkpoint anchor
seq:            %d
sealed:         %s
checkpoint hash: %x

Store this file OFF-node and timestamp it (RFC-3161 TSA / notary / client backup).
Any later journal whose last checkpoint hash differs from the above was rewritten —
that is the external, node-independent proof of journal integrity (O3).
`, cp.Seq, cp.TS.UTC().Format(time.RFC3339), head)

	if *tsaURL != "" {
		token, err := tsaTimestamp([]byte(hex.EncodeToString(head)), *tsaURL)
		if err != nil {
			log.Fatal(err)
		}
		tsrPath := *out + ".tsr"
		if err := os.WriteFile(tsrPath, token, 0o600); err != nil {
			log.Fatal(err)
		}
		text += fmt.Sprintf("\ntsa:      %s\ntoken:    %s\nverify:   openssl ts -verify -data <this file> -in %s\n", *tsaURL, tsrPath, tsrPath)
	}

	if err := os.WriteFile(*out, []byte(text), 0o600); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("anchor written to %s — store it off-node and TSA-timestamp it\n", *out)
}

// lastCheckpointSealed returns the LAST checkpoint.sealed event in the
// journal, or nil.
//
// Anchor semantics (single — the value both `kalita anchor` and `kalita
// backup` / `backup verify` commit to): the anchor is the checkpoint event's
// OWN chain hash, ev.Hash — the same bytes `kalita backup` writes into
// MANIFEST.json checkpoint_anchor and `backup verify` re-checks against the
// journal. It is NOT payload.sealed_hash: that is the hash of the head event
// at seal time, which lives inside the payload and is therefore already
// covered by ev.Hash's chain — the event hash commits to the whole prefix
// including the checkpoint itself, so it is at least as strong an off-node
// proof, and it is the value existing backup manifests already store (changing
// to sealed_hash would invalidate every manifest written so far). backup.go
// and anchor.go used to disagree here; this helper is the single source.
func lastCheckpointSealed(events []*eventstore.Event) *eventstore.Event {
	var last *eventstore.Event
	for _, ev := range events {
		if ev.Kind == eventstore.CheckpointSealed {
			last = ev
		}
	}
	return last
}
