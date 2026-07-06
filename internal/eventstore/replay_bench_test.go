//go:build passport

// Package eventstore_test (external/black-box) so this file can call
// engine.New() — the real startup replay path — without an import cycle:
// internal/engine already imports internal/eventstore, so a replay
// benchmark that wants the genuine replay code path must sit on the far
// side of that edge, not inside package eventstore itself.
package eventstore_test

// Node performance passport — baseline v0, profile P3 "journal growth"
// (docs/HLD-V1.md §H10, docs/RELEASE-1.0.md N9: "replay при старте замерен
// только на 100k (1.07 с)... replay-бенчмарк 1M/10M синтетики в CI").
//
// Gated behind the `passport` build tag — see internal/engine/passport_bench_test.go
// for why (keeps it out of the PR-gate bench smoke step without touching its
// regex). P1/P2/P4 live there; P3 lives here because it is specifically about
// engine.New()'s replay of an already-written journal.
//
// The synthetic-journal generator (seedReplayJournal et al.) lives in
// replay_helpers_test.go, WITHOUT this build tag: TestReplayBudget1M
// (replay_budget_test.go) is the hard pass/fail gate on the same 1M-event
// scale and must run in the ordinary `go test ./...` PR gate, so it cannot
// depend on anything hidden behind -tags=passport.
//
// Run: go test -tags=passport -run='^$' -bench=BenchmarkPassport -benchmem ./internal/eventstore
// The 1M case is heavy by design (it is the point — measuring the ADR-002
// "tens of millions of events" growth path) and skips under -short.

import (
	"context"
	"testing"

	"github.com/avangerus/kalita/internal/engine"
)

// benchmarkPassportReplay seeds records*2 events ONCE, then times (a) the raw
// journal fetch and (b) the full engine.New() replay (fetch + fold) over the
// SAME static journal — the store_all/engine_new split isolates where replay
// time goes, the way BenchmarkPermittedSet contrasts scoped vs full-scan cost
// over the same table.
func benchmarkPassportReplay(b *testing.B, records int) {
	ctx := context.Background()
	model := replayModel(b)
	store := seedReplayJournal(b, records)
	totalEvents := float64(records * 2)

	b.Run("store_all", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := store.All(ctx); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(totalEvents/(b.Elapsed().Seconds()/float64(b.N)), "events/s")
	})

	b.Run("engine_new", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := engine.New(ctx, model, store); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(totalEvents/(b.Elapsed().Seconds()/float64(b.N)), "events/s")
	})
}

// BenchmarkPassportP3Replay100k: 50k records * 2 events = 100k events — the
// exact scale N9 cites today ("1.07 с").
func BenchmarkPassportP3Replay100k(b *testing.B) {
	benchmarkPassportReplay(b, 50_000)
}

// BenchmarkPassportP3Replay1M: 500k records * 2 events = 1M events — the
// ADR-002 "tens of millions" growth path, one order of magnitude in. Building
// and replaying a 1M-event journal is deliberately heavy; -short skips it.
// TestReplayBudget1M (replay_budget_test.go) is the pass/fail gate at this
// same scale; this benchmark is the informational ns/op companion.
func BenchmarkPassportP3Replay1M(b *testing.B) {
	if testing.Short() {
		b.Skip("1M-event replay is heavy; run without -short (see `make passport`)")
	}
	benchmarkPassportReplay(b, 500_000)
}
