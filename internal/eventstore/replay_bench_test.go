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
// Run: go test -tags=passport -run='^$' -bench=BenchmarkPassport -benchmem ./internal/eventstore
// The 1M case is heavy by design (it is the point — measuring the ADR-002
// "tens of millions of events" growth path) and skips under -short.

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
)

const replayPack = `
pack replaybench
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1

entity Ticket:
    title:  string required
    status: enum[New, InProgress, Done] default=New

workflow Ticket on status:
    New        -> InProgress: start
    InProgress -> Done:       finish

roles:
    Admin

permissions:
    Admin:
        full [Ticket]
`

var replayActor = eventstore.Actor{Type: eventstore.ActorSystem, ID: "replay-gen", Role: "Admin"}

// replayCreatedPayload / replayActionPayload mirror the JSON shape of
// engine's unexported createdPayload/actionPayload (internal/engine/engine.go,
// internal/engine/workflow.go). They can't be imported (unexported, and this
// package cannot import engine's internals anyway) so the shape is
// duplicated deliberately — this is a synthetic journal generator standing
// in for the real Engine.Create/Act, which is the whole point: P3 measures
// replay of an ALREADY-WRITTEN journal, not the cost of writing one.
type replayCreatedPayload struct {
	Values map[string]any `json:"values"`
}

type replayActionPayload struct {
	Action string `json:"action"`
	From   string `json:"from"`
	To     string `json:"to"`
}

func replayModel(tb testing.TB) *dsl.Model {
	tb.Helper()
	model, errs := dsl.Compile(map[string]string{"replay.dsl": replayPack})
	if len(errs) > 0 {
		tb.Fatalf("replay pack must compile: %v", errs[0])
	}
	return model
}

// seedReplayJournal appends n records' worth of history directly to a
// MemStore: one record.created + one record.action (New -> InProgress) per
// record, so total events = 2*n. Bypasses engine.Create/Act on purpose.
func seedReplayJournal(tb testing.TB, n int) eventstore.Store {
	tb.Helper()
	ctx := context.Background()
	store := eventstore.NewMemStore(nil)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("t-%d", i)
		created, err := json.Marshal(replayCreatedPayload{
			Values: map[string]any{"title": fmt.Sprintf("ticket %d", i), "status": "New"},
		})
		if err != nil {
			tb.Fatal(err)
		}
		if _, err := store.Append(ctx, eventstore.AppendInput{
			Actor: replayActor, Kind: eventstore.RecordCreated,
			Subject: eventstore.Subject{Entity: "Ticket", RecordID: id},
			Payload: created,
		}); err != nil {
			tb.Fatalf("seed create %d: %v", i, err)
		}
		action, err := json.Marshal(replayActionPayload{Action: "start", From: "New", To: "InProgress"})
		if err != nil {
			tb.Fatal(err)
		}
		if _, err := store.Append(ctx, eventstore.AppendInput{
			Actor: replayActor, Kind: eventstore.RecordAction,
			Subject: eventstore.Subject{Entity: "Ticket", RecordID: id},
			Payload: action,
		}); err != nil {
			tb.Fatalf("seed action %d: %v", i, err)
		}
	}
	return store
}

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
func BenchmarkPassportP3Replay1M(b *testing.B) {
	if testing.Short() {
		b.Skip("1M-event replay is heavy; run without -short (see `make passport`)")
	}
	benchmarkPassportReplay(b, 500_000)
}
