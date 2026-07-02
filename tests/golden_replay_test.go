// Package tests holds cross-cutting gates that do not belong to one internal
// package. TestGoldenReplay is the fold-compatibility gate from
// docs/lld/LLD-03-upgrade-eras.md §3/§7 (PR-0): the CURRENT binary must be
// able to replay every recorded testdata/golden/<ver>/journal.jsonl and
// reproduce the projections committed alongside it.
package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/avangerus/kalita/cmd/goldengen/goldenfmt"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// TestGoldenReplay walks every testdata/golden/<ver>/ directory and proves
// that the current engine/eventstore/dsl/identity code folds that version's
// frozen journal.jsonl into exactly the projections recorded in expected/.
// A journal is fixed forever once committed (LLD-03 §3: "сам journal.jsonl
// фиксируется как есть") — this test never regenerates one, it only replays.
func TestGoldenReplay(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "testdata", "golden"))
	if err != nil {
		t.Fatalf("read testdata/golden: %v", err)
	}

	var dirs []string
	for _, e := range entries {
		// testdata/golden/ also holds README.md (regeneration instructions) —
		// only version directories (v0.1, ...) are golden fixtures.
		if e.IsDir() {
			dirs = append(dirs, filepath.Join("..", "testdata", "golden", e.Name()))
		}
	}
	if len(dirs) == 0 {
		t.Fatal("no testdata/golden/<ver> directories found — nothing to gate")
	}

	for _, dir := range dirs {
		dir := dir
		t.Run(filepath.Base(dir), func(t *testing.T) {
			testGoldenVersion(t, dir)
		})
	}
}

func testGoldenVersion(t *testing.T, dir string) {
	ctx := context.Background()

	model, cerrs, err := goldenfmt.LoadPack(filepath.Join(dir, "pack"))
	if err != nil {
		t.Fatalf("load genesis pack: %v", err)
	}
	if len(cerrs) > 0 {
		t.Fatalf("genesis pack does not compile under the CURRENT dsl compiler: %v", cerrs[0])
	}

	events, err := goldenfmt.ReadJournal(filepath.Join(dir, "journal.jsonl"))
	if err != nil {
		t.Fatalf("read journal.jsonl: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("journal.jsonl has zero events")
	}

	// Sanity: the committed file itself must not have bit-rotted or been
	// hand-edited into an inconsistent chain — a corrupt fixture and a real
	// fold regression are different failures worth telling apart.
	if err := eventstore.VerifyChain(events); err != nil {
		t.Fatalf("journal.jsonl fails its own hash chain (fixture corrupted, not a fold regression): %v", err)
	}

	store := goldenfmt.NewFrozenStore(events)
	reg := identity.NewRegistry(store)

	// engine.New folds All(ctx) at construction — this IS the "replay" being
	// gated: the same code path a real node runs at every boot (HLD-V1 §H3,
	// "replay = миграция аддитивных изменений бесплатно"). No options are
	// needed: applyEvent never reads the engine clock, and replay never calls
	// Decide/DecideProposal (which is the only place signatures are checked),
	// so WithClock/WithVerifier would be inert here.
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatalf("engine.New (replay): %v", err)
	}

	order := goldenfmt.BuildOrder(events)
	snap, err := goldenfmt.Dump(ctx, eng, reg, order)
	if err != nil {
		t.Fatalf("dump replayed projections: %v", err)
	}

	diffs, err := goldenfmt.CompareExpected(filepath.Join(dir, "expected"), snap, order)
	if err != nil {
		t.Fatalf("compare against expected/: %v", err)
	}
	for _, d := range diffs {
		t.Errorf("%s: replayed projection does not match expected/%s\n--- expected ---\n%s\n--- got ---\n%s",
			dir, d.File, d.Expected, d.Got)
	}
}
