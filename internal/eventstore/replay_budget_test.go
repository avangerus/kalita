// Package eventstore_test (external/black-box), same reason as
// replay_helpers_test.go: engine.New() is the real replay path and importing
// it from inside package eventstore would be a cycle.
package eventstore_test

// TestReplayBudget1M — RELEASE-1.0 N9 / LLD-12 §1,§5(4): "измеримые пороги
// вместо веры", not a benchmark someone has to remember to eyeball. ADR-002
// promises "tens of millions of events"; the only measurement on record
// before this release was 100k (1.07s, on an unspecified machine). This test
// puts a THIRD number on record — 1M — and turns it into a hard, CI-gating
// budget instead of another benchmark line nobody is required to read.
//
// Unlike BenchmarkPassportP3Replay1M (replay_bench_test.go, behind
// -tags=passport, nightly-only, continue-on-error), this file carries NO
// build tag: it is a plain `go test`, so it runs in the ordinary
// `go test ./...` PR gate (.github/workflows/ci.yml) on every push and PR. A
// regression here is a build failure, not an informational line in a job
// that is allowed to go red without blocking anything.
//
// Threshold: replayBudget1M = 5s, taken directly from LLD-12 §4's own stated
// target ("P3 replay-бюджет держится (<5с в паспорте)") — the same number
// that gates whether tiered storage / snapshot-projections get unfrozen
// (RELEASE-1.0 §5). Measured baseline on the development machine (13th Gen
// Intel Core i7-13700KF) at this exact scale: engine.New() replays the
// 1,000,000-event journal in ~1.15s — roughly 4x headroom under the 5s
// budget, which is deliberate: CI runners are typically slower/noisier than
// a dedicated desktop CPU (shared vCPUs, no turbo headroom), and a threshold
// with no margin flaps on ordinary machine variance instead of catching a
// real regression. 5s is not "wherever the number happened to land" — it is
// LLD-12's own pre-committed number, chosen before this test existed.
//
// A failure here means one of two things: (a) an actual performance
// regression in the replay path (fix it), or (b) the node has organically
// grown past the point a naive full-refold-on-every-start replay can serve
// in the budget (the tiered-storage/snapshot-projection trigger LLD-12 §4
// describes — "триггер — по факту достижения, не по вере"). Either way, the
// right response is investigation, never raising the threshold to make the
// red go away.
import (
	"context"
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/engine"
)

const replayBudget1M = 5 * time.Second

func TestReplayBudget1M(t *testing.T) {
	if testing.Short() {
		t.Skip("1M-event replay is heavy (seeds + replays a 1,000,000-event journal); run without -short")
	}
	ctx := context.Background()
	model := replayModel(t)
	store := seedReplayJournal(t, 500_000) // 500k records * 2 events (created+action) = 1,000,000 events

	start := time.Now()
	if _, err := engine.New(ctx, model, store); err != nil {
		t.Fatalf("engine.New() must successfully replay a 1M-event journal: %v", err)
	}
	elapsed := time.Since(start)
	t.Logf("engine.New() replay of 1,000,000 events took %s (budget %s)", elapsed, replayBudget1M)

	if elapsed > replayBudget1M {
		t.Fatalf("replay budget exceeded: 1M-event engine.New() replay took %s, budget is %s "+
			"(LLD-12 §4 / RELEASE-1.0 N9) — this is the tiered-storage/snapshot-projection "+
			"unfreeze trigger, investigate before raising the threshold", elapsed, replayBudget1M)
	}
}
