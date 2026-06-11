package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Destructive migrations (V1-GATE): the change pipeline only ever applies
// additive diffs; renames and removals go through the manual era procedure:
//
//	kalita export  (old node, stopped)  -> records.json
//	operator transforms the JSON        -> records-new.json
//	fresh journal + new genesis pack    -> kalita import
//
// The old journal is archived intact: history is never destroyed, an era
// boundary is recorded on both sides.

type exportFile struct {
	Pack       string                          `json:"pack"`
	DefVersion uint64                          `json:"def_version"`
	Records    map[string][]exportRecord       `json:"records"`
}

type exportRecord struct {
	ID     string         `json:"id"`
	Values map[string]any `json:"values"`
}

func exportCmd(args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	pack := fs.String("pack", "", "pack directory matching the journal")
	out := fs.String("out", "kalita-export.json", "output file")
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

	dump := exportFile{DefVersion: eng.DefVersion(), Records: map[string][]exportRecord{}}
	if m := eng.Model().Manifest; m != nil {
		dump.Pack = m.Name
	}
	total := 0
	for _, name := range eng.Model().Order {
		for _, rec := range eng.Export(name) {
			dump.Records[name] = append(dump.Records[name], exportRecord{ID: rec.ID, Values: rec.Values})
			total++
		}
	}
	raw, _ := json.MarshalIndent(dump, "", "  ")
	if err := os.WriteFile(*out, raw, 0o600); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("exported %d records of %d entities to %s\n", total, len(dump.Records), *out)
}

func importCmd(args []string) {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	pack := fs.String("pack", "", "NEW genesis pack directory")
	in := fs.String("in", "kalita-export.json", "input file")
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

	raw, err := os.ReadFile(*in)
	if err != nil {
		log.Fatal(err)
	}
	var dump exportFile
	if err := json.Unmarshal(raw, &dump); err != nil {
		log.Fatal(err)
	}
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		log.Fatal(err)
	}

	migrator := eventstore.Actor{Type: eventstore.ActorHuman, ID: "migration"}
	basis := &eventstore.Basis{Type: "human", ID: "migration:era-import"}
	total, failed := 0, 0
	for _, name := range eng.Model().Order { // declaration order keeps refs sane
		for _, rec := range dump.Records[name] {
			if err := eng.ImportRecord(ctx, migrator, name, rec.ID, rec.Values, basis); err != nil {
				failed++
				fmt.Printf("SKIP %s/%s: %v\n", name, rec.ID, err)
				continue
			}
			total++
		}
	}
	fmt.Printf("imported %d records (%d skipped) — re-register actors with kalita user|agent add\n", total, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

func mustPG(ctx context.Context) *eventstore.PGStore {
	dsn := os.Getenv("KALITA_PG_DSN")
	if dsn == "" {
		log.Fatal("KALITA_PG_DSN is required")
	}
	store, err := eventstore.NewPGStore(ctx, dsn, "", nil)
	if err != nil {
		log.Fatal(err)
	}
	return store
}
