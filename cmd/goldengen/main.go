// Command goldengen (re)generates one testdata/golden/<ver>/ fixture: it runs
// the fixed scenario in scenario.go against a fresh MemStore, then dumps the
// resulting journal and projections to disk. See docs/lld/LLD-03-upgrade-eras.md
// §3/§7 (PR-0) and testdata/golden/README.md.
//
// Production code under internal/ is never imported for its side effects here
// beyond the same public API any node/tool uses (engine.New, Engine.Create/
// Act/..., identity.Registry, dsl.Compile) — nothing in internal/ was changed
// to build this tool.
//
// Usage:
//
//	go run ./cmd/goldengen                  # regenerate testdata/golden/v0.1
//	go run ./cmd/goldengen -ver v0.2         # a future version, new scenario
//	go run ./cmd/goldengen -force            # overwrite an existing version
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/avangerus/kalita/cmd/goldengen/goldenfmt"
	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

func main() {
	root := flag.String("root", ".", "repository root (testdata/golden is written under here)")
	ver := flag.String("ver", "v0.1", "golden version directory to (re)generate")
	force := flag.Bool("force", false, "overwrite an existing golden version directory (golden dirs are frozen history — use with care)")
	flag.Parse()

	if err := run(*root, *ver, *force); err != nil {
		log.Fatalf("goldengen: %v", err)
	}
}

func run(root, ver string, force bool) error {
	ctx := context.Background()

	dir := filepath.Join(root, "testdata", "golden", ver)
	journalPath := filepath.Join(dir, "journal.jsonl")
	if _, err := os.Stat(journalPath); err == nil && !force {
		return fmt.Errorf("%s already exists — golden dirs are frozen history once committed; pass -force to regenerate it intentionally", dir)
	}

	res, err := buildScenario(ctx)
	if err != nil {
		return fmt.Errorf("scenario: %w", err)
	}

	events, err := res.Store.All(ctx)
	if err != nil {
		return fmt.Errorf("read generated journal: %w", err)
	}
	if len(events) == 0 {
		return fmt.Errorf("scenario produced zero events")
	}
	if err := eventstore.VerifyChain(events); err != nil {
		return fmt.Errorf("generated journal fails its own hash chain (this would be a goldengen bug): %w", err)
	}

	if err := goldenfmt.WritePack(filepath.Join(dir, "pack"), map[string]string{"pack.dsl": packV1Source}); err != nil {
		return fmt.Errorf("write pack: %w", err)
	}
	if err := goldenfmt.WriteJournal(journalPath, events); err != nil {
		return fmt.Errorf("write journal: %w", err)
	}

	// expected/*.json is dumped from a REPLAYED engine (fold-only, via the same
	// FrozenStore the gate itself uses), not from res.Engine — the engine that
	// actually ran the live scenario. This is not a stylistic choice: at least
	// one Engine method (ReportProgress) updates its in-memory projection using
	// wall-clock time.Now() instead of the injected clock for a field with no
	// corresponding payload value (NoteAt) — harmless in production (it is a
	// display-only "last seen" timestamp on a live node), but it means the
	// LIVE projection and the FOLDED one can legitimately disagree the instant
	// after the call returns. Dumping the live engine would bake that instant's
	// wall-clock value into expected/*.json as if it were reproducible, and the
	// replay gate would then fail forever (it always folds, so it always
	// derives the field from event.TS). Dumping a replay instead makes
	// expected/ exactly what the gate checks, and — as a bonus — makes
	// goldengen immune to this whole class of live-vs-fold drift without
	// touching internal/. See agent/kalita/BUGS.md and agent/kalita/
	// QUESTIONS-LLD.md for the underlying engine bug, which PR-0 intentionally
	// leaves unfixed (out of scope: "Прод-код (internal/) НЕ менять").
	replayModel, cerrs := dsl.Compile(map[string]string{"pack.dsl": packV1Source})
	if len(cerrs) > 0 {
		return fmt.Errorf("re-compiling the genesis pack for the self-replay pass failed: %v", cerrs[0])
	}
	replayStore := goldenfmt.NewFrozenStore(events)
	replayEng, err := engine.New(ctx, replayModel, replayStore)
	if err != nil {
		return fmt.Errorf("self-replay engine.New: %w", err)
	}
	replayReg := identity.NewRegistry(replayStore)

	order := goldenfmt.BuildOrder(events)
	snap, err := goldenfmt.Dump(ctx, replayEng, replayReg, order)
	if err != nil {
		return fmt.Errorf("dump replayed projections: %w", err)
	}
	if err := goldenfmt.WriteExpected(filepath.Join(dir, "expected"), snap, order); err != nil {
		return fmt.Errorf("write expected: %w", err)
	}

	fmt.Printf("goldengen: wrote %s (%d events, %d normalized ids, %d record entities)\n",
		dir, len(events), len(order), len(snap.Records))
	return nil
}
