package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// Destructive migrations (V1-GATE): the change pipeline only ever applies
// additive diffs; renames and removals go through the manual era procedure
// fixed by docs/lld/LLD-03-upgrade-eras.md §4 ("эры: деструктивные миграции
// как процедура"):
//
//	kalita export --out era.jsonl   (old node, stopped) -> era.jsonl
//	operator transforms era.jsonl with a script (jq rename, type table, drop key)
//	fresh journal + new genesis pack -> kalita import --in era.jsonl --pack <new>
//	kalita era verify --old <old dsn|dump> --new <new dsn>
//
// The old journal is archived intact (pg_dump, cold storage) and its node is
// stopped forever: history is never destroyed, an era boundary is recorded on
// both sides — era.genesis in the new journal chains back to the old
// journal's final, sealed checkpoint (LLD-03 §4: "история доказуема через
// разрушение").

// eraRowLine and eraManifestLine are the two JSON shapes multiplexed onto
// era.jsonl's lines (LLD-03 §4: "строки {entity, id, values, state_since} +
// завершающий manifest"). The manifest is always the LAST line; every
// preceding line is a row. peekIsManifest distinguishes them: only the
// manifest carries era_id.
type eraProbe struct {
	EraID string `json:"era_id,omitempty"`
}

// readEraFile reads an era.jsonl produced by exportCmd: zero or more
// EraRow lines followed by exactly one trailing EraManifest line.
func readEraFile(path string) ([]eventstore.EraRow, eventstore.EraManifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, eventstore.EraManifest{}, err
	}
	defer f.Close()

	var lines [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024) // values/blobs can be large
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		cp := append([]byte(nil), line...)
		lines = append(lines, cp)
	}
	if err := sc.Err(); err != nil {
		return nil, eventstore.EraManifest{}, err
	}
	if len(lines) == 0 {
		return nil, eventstore.EraManifest{}, fmt.Errorf("%s is empty", path)
	}

	var manifest eventstore.EraManifest
	last := lines[len(lines)-1]
	var probe eraProbe
	if err := json.Unmarshal(last, &probe); err != nil || probe.EraID == "" {
		return nil, eventstore.EraManifest{}, fmt.Errorf("%s: last line is not a valid era manifest (must carry era_id)", path)
	}
	if err := json.Unmarshal(last, &manifest); err != nil {
		return nil, eventstore.EraManifest{}, fmt.Errorf("%s: manifest line: %w", path, err)
	}

	rows := make([]eventstore.EraRow, 0, len(lines)-1)
	for _, line := range lines[:len(lines)-1] {
		var row eventstore.EraRow
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, eventstore.EraManifest{}, fmt.Errorf("%s: row line: %w", path, err)
		}
		rows = append(rows, row)
	}
	return rows, manifest, nil
}

func exportCmd(args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	pack := fs.String("pack", "", "pack directory matching the journal")
	out := fs.String("out", "era.jsonl", "output file (LLD-03 §4 era.jsonl format)")
	dataDir := fs.String("data-dir", "kalita-data", "node key directory (node.key/node.pub) — sealing the export needs the node's signing key")
	_ = fs.Parse(args)
	if *pack == "" {
		log.Fatal("--pack is required")
	}
	model, errs := loadPack(*pack)
	if len(errs) > 0 {
		log.Fatalf("pack does not compile: %v", errs[0])
	}
	ctx := context.Background()
	store := mustPG(ctx)
	defer store.Close()
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		log.Fatal(err)
	}

	// Seal a FRESH checkpoint right before exporting (LLD-03 §4: "Перед
	// export — Seal() (свежий checkpoint), его hash и уходит в manifest") —
	// its own (seq, hash) becomes the anchor the new era's genesis chains to.
	nodeKey, err := identity.LoadOrCreateNodeKey(*dataDir)
	if err != nil {
		log.Fatalf("node key: %v", err)
	}
	ckpt, err := eventstore.Seal(ctx, store, "node", nodeKey)
	if err != nil {
		log.Fatalf("could not seal a fresh checkpoint before export: %v", err)
	}

	manifest := eventstore.EraManifest{
		EraID:                     uuid.NewString(),
		SourceFinalSeq:            ckpt.Seq,
		SourceFinalCheckpointHash: hex.EncodeToString(ckpt.Hash),
		DefVersion:                eng.DefVersion(),
		NodeID:                    hex.EncodeToString(nodeKey.Public().(ed25519.PublicKey)),
		ExportedAt:                time.Now().UTC(),
	}

	f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)

	total := 0
	for _, name := range eng.Model().Order {
		for _, rec := range eng.Export(name) {
			since, _ := eng.StateSince(name, rec.ID)
			row := eventstore.EraRow{Entity: name, ID: rec.ID, Values: rec.Values, StateSince: since}
			line, err := json.Marshal(row)
			if err != nil {
				log.Fatal(err)
			}
			if _, err := w.Write(append(line, '\n')); err != nil {
				log.Fatal(err)
			}
			total++
		}
	}
	line, err := json.Marshal(manifest)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := w.Write(append(line, '\n')); err != nil {
		log.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("exported %d records to %s\n", total, *out)
	fmt.Printf("era_id %s, sealed at seq %d (checkpoint hash %s)\n", manifest.EraID, manifest.SourceFinalSeq, manifest.SourceFinalCheckpointHash)
	fmt.Println("archive this journal (pg_dump) to cold storage and stop this node PERMANENTLY before anyone imports era.jsonl into a new one — an era is a one-way door.")
}

func importCmd(args []string) {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	pack := fs.String("pack", "", "NEW genesis pack directory")
	in := fs.String("in", "era.jsonl", "input file (LLD-03 §4 era.jsonl format)")
	_ = fs.Parse(args)
	if *pack == "" {
		log.Fatal("--pack is required")
	}
	model, errs := loadPack(*pack)
	if len(errs) > 0 {
		log.Fatalf("pack does not compile: %v", errs[0])
	}
	ctx := context.Background()
	store := mustPG(ctx)
	defer store.Close()

	// import only enters an empty journal: eras do not mix
	if seq, _, err := store.Head(ctx); err != nil {
		log.Fatal(err)
	} else if seq != 0 {
		log.Fatal("journal is not empty: import starts a NEW era — point KALITA_PG_DSN at a fresh database")
	}

	rows, manifest, err := readEraFile(*in)
	if err != nil {
		log.Fatal(err)
	}

	// era.genesis is the FIRST event of the new era's journal (LLD-03 §4):
	// it chains back to the old era's sealed final state, so the two
	// journals' history is provable even though the old one is eventually
	// destroyed/archived. Written directly to the store (not through the
	// engine, which has no generic Append passthrough) — the engine below
	// only needs to fold record.imported rows, not this provenance marker.
	genesisPayload, err := json.Marshal(eventstore.EraGenesisPayload{PrevEra: manifest})
	if err != nil {
		log.Fatal(err)
	}
	migrator := eventstore.Actor{Type: eventstore.ActorHuman, ID: "migration"}
	migrationBasis := &eventstore.Basis{Type: "human", ID: "migration:era-import"}
	if _, err := store.Append(ctx, eventstore.AppendInput{
		Actor:   migrator,
		Kind:    eventstore.EraGenesis,
		Payload: genesisPayload,
		Basis:   migrationBasis,
		SchemaV: eventstore.CurrentSchemaV,
	}); err != nil {
		log.Fatalf("append era.genesis: %v", err)
	}
	// NOTE (deferred, see report): LLD-03 §2 step 2 lists a definition.applied
	// event for the new pack as part of an era import. It is intentionally
	// NOT written here: internal/engine's DefinitionApplied fold
	// unconditionally increments defVersion on every replay, with no
	// dedicated "genesis, not a proposal" case — writing one here would make
	// a freshly-imported node report defVersion 1 live but 2 after its next
	// restart (a real replay-vs-live divergence). Fixing that fold correctly
	// is an internal/engine design decision outside this change's zone.
	// The new pack's provenance is already anchored via era.genesis's
	// prev_era.def_version plus the pack's own `version` header — the same
	// as every other genesis boot in this codebase (no explicit event
	// either).

	eng, err := engine.New(ctx, model, store)
	if err != nil {
		log.Fatal(err)
	}

	total, failed := 0, 0
	for _, row := range rows {
		origin := eventstore.RecordOrigin{EraID: manifest.EraID, ID: row.ID}
		if err := eng.ImportRecord(ctx, migrator, row.Entity, row.ID, row.Values, origin, migrationBasis); err != nil {
			failed++
			fmt.Printf("SKIP %s/%s: %v\n", row.Entity, row.ID, err)
			continue
		}
		total++
	}
	fmt.Printf("imported %d records (%d skipped) from era %s — re-register actors with kalita user|agent add\n", total, failed, manifest.EraID)
	fmt.Println("next: kalita era verify --old <old dsn or dump> --new $KALITA_PG_DSN --era " + *in)
	if failed > 0 {
		os.Exit(1)
	}
}

// eraCmd implements `kalita era verify` (LLD-03 §4): proves the new era's
// genesis actually chains to the old era's sealed final state, and that the
// new journal's imported-record count matches the exported era.jsonl.
func eraCmd(args []string) {
	if len(args) < 1 || args[0] != "verify" {
		fmt.Println("usage: kalita era verify --old <old-node dsn|dump.jsonl> --new <new-node dsn> [--era era.jsonl]")
		os.Exit(1)
	}
	fs := flag.NewFlagSet("era verify", flag.ExitOnError)
	old := fs.String("old", "", "old era's source: a postgres:// DSN (live/archived DB) or a path to a journal.jsonl-shaped dump")
	newDSN := fs.String("new", "", "new era's node: a postgres:// DSN (defaults to KALITA_PG_DSN via the usual secrets chain if empty)")
	eraFile := fs.String("era", "", "the era.jsonl used for the import — cross-checks row counts against record.imported events (optional but recommended)")
	_ = fs.Parse(args[1:])
	if *old == "" {
		log.Fatal("--old is required")
	}
	ctx := context.Background()

	oldEvents, err := loadOldEraEvents(ctx, *old)
	if err != nil {
		fmt.Printf("FAIL: could not read old era: %v\n", err)
		os.Exit(1)
	}

	var newStore *eventstore.PGStore
	if *newDSN != "" {
		newStore, err = eventstore.NewPGStore(ctx, *newDSN, os.Getenv("KALITA_PG_SCHEMA"), nil)
	} else {
		newStore = mustPG(ctx)
	}
	if err != nil {
		fmt.Printf("FAIL: could not connect to new era: %v\n", err)
		os.Exit(1)
	}
	defer newStore.Close()
	newEvents, err := newStore.All(ctx)
	if err != nil {
		fmt.Printf("FAIL: reading new era journal: %v\n", err)
		os.Exit(1)
	}
	if err := eventstore.VerifyChain(newEvents); err != nil {
		fmt.Printf("FAIL: new era journal fails its own hash chain: %v\n", err)
		os.Exit(1)
	}

	var genesis *eventstore.Event
	for _, ev := range newEvents {
		if ev.Kind == eventstore.EraGenesis {
			genesis = ev
			break
		}
	}
	if genesis == nil {
		fmt.Println("FAIL: new era journal has no era.genesis event — it was not created by `kalita import`")
		os.Exit(1)
	}
	var gp eventstore.EraGenesisPayload
	if err := json.Unmarshal(genesis.Payload, &gp); err != nil {
		fmt.Printf("FAIL: era.genesis payload: %v\n", err)
		os.Exit(1)
	}

	if err := eventstore.VerifyEraChain(oldEvents, gp.PrevEra); err != nil {
		fmt.Printf("FAIL: %v\n", err)
		os.Exit(1)
	}

	imported := 0
	for _, ev := range newEvents {
		if ev.Kind == eventstore.RecordImported {
			imported++
		}
	}
	fmt.Printf("chain ok: old era %s (final seq %d) matches this journal's era.genesis\n", gp.PrevEra.EraID, gp.PrevEra.SourceFinalSeq)
	fmt.Printf("%d record.imported event(s) in the new journal\n", imported)

	if *eraFile != "" {
		rows, manifest, err := readEraFile(*eraFile)
		if err != nil {
			fmt.Printf("FAIL: reading --era file: %v\n", err)
			os.Exit(1)
		}
		if manifest.EraID != gp.PrevEra.EraID {
			fmt.Printf("FAIL: --era file's era_id %s does not match this journal's era.genesis (%s)\n", manifest.EraID, gp.PrevEra.EraID)
			os.Exit(1)
		}
		if len(rows) != imported {
			fmt.Printf("FAIL: era.jsonl has %d rows but the new journal only shows %d record.imported events\n", len(rows), imported)
			os.Exit(1)
		}
		fmt.Printf("row count ok: %d rows in %s == %d record.imported events\n", len(rows), *eraFile, imported)
	}
	fmt.Println("era verify OK")
}

// loadOldEraEvents reads the old era's full event history from either a live
// (or archived-then-restored) postgres DSN, or an offline dump file in the
// same one-JSON-object-per-line shape used by testdata/golden/*/journal.jsonl
// (an eventstore.Event per line) — the operator's own pg_dump/psql COPY of
// the events table, reshaped to that format, or a `kalita verify`-style
// export. No such exporter exists yet in this codebase (out of this change's
// scope): documented in the report as the one manual step the runbook must
// spell out for the "старый... dump" case.
func loadOldEraEvents(ctx context.Context, source string) ([]*eventstore.Event, error) {
	if strings.HasPrefix(source, "postgres://") || strings.HasPrefix(source, "postgresql://") {
		store, err := eventstore.NewPGStore(ctx, source, os.Getenv("KALITA_PG_SCHEMA_OLD"), nil)
		if err != nil {
			return nil, err
		}
		defer store.Close()
		return store.All(ctx)
	}
	f, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var events []*eventstore.Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var ev eventstore.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
		cp := ev
		events = append(events, &cp)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func mustPG(ctx context.Context) *eventstore.PGStore {
	dsn := resolveDSN()
	if dsn == "" {
		log.Fatal("KALITA_PG_DSN is required")
	}
	store, err := eventstore.NewPGStore(ctx, dsn, os.Getenv("KALITA_PG_SCHEMA"), nil)
	if err != nil {
		log.Fatal(err)
	}
	return store
}
