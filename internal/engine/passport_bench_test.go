//go:build passport

package engine

// Node performance passport — baseline v0 (docs/HLD-V1.md §H10, docs/RELEASE-1.0.md
// N6 "не переделывать, а измерить" / N9 "измеримые пороги вместо веры").
//
// This file holds profiles P1 (pilot mix), P2 (bulk create) and P4 (auth
// storm). P3 (journal replay) lives in internal/eventstore/replay_bench_test.go
// because it exercises engine.New() over a pre-built journal, not a live one.
//
// Gated behind the `passport` build tag on purpose: `go test -bench=Bench
// ./internal/engine` is already wired into the PR-gate smoke step
// (.github/workflows/ci.yml) as a cheap order-of-magnitude regression check
// over the EXISTING benchmarks (BenchmarkCreate, BenchmarkPermittedSet, ...).
// These profiles are heavier and deliberately informational (see `make
// passport` and the nightly `passport` CI job) — the tag keeps them out of
// that PR-gate sweep without touching its regex, matching the existing
// internal/webui/embed_*.go convention for opt-in-only code.
//
// Run: go test -tags=passport -run='^$' -bench=BenchmarkPassport -benchmem ./internal/engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// --- P1: pilot mix -----------------------------------------------------------
//
// H10 profile: "5 MCP-агентов + 10 UI-читателей + automation-тик" against a
// prepared node. Below: one sub-benchmark per operation kind (stationary —
// each isolates its op at a fixed table size, like BenchmarkPermittedSet
// isolates query cost from seed cost), plus one "combined_round" that
// interleaves them in the 5:10:1 shape H10 describes, for a single blended
// ops/sec figure ("X mutations/s on profile P1").
//
// combined_round is the one sub-benchmark whose table genuinely grows across
// b.N (it creates records; nothing else here does) — pin -benchtime=Nx when
// citing it in release notes so the workload size is reproducible across
// machines/runs, not just the wall-clock time (see `make passport`).

const passportPack = `
pack passport
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1

entity Ticket:
    title:  string required
    amount: money
    status: enum[New, InProgress, Done] default=New

roles:
    Admin
    Reader
    Bot agent

permissions:
    Admin:
        full [Ticket]
        act  [start, finish, reopen]
    Bot:
        read   [Ticket]
        create [Ticket]
        act    [start, finish, reopen]
        deny   [update Ticket.*, delete *]
    Reader:
        read [Ticket]
        deny [delete *, update Ticket.*]

workflow Ticket on status:
    New        -> InProgress: start
    InProgress -> Done:       finish
    any        -> New:        reopen

automation:
    on schedule every 5m for Ticket when status = New:
        escalate_to Admin

dashboard TicketStats "Tickets":
    tile "Open":     count Ticket where status != Done
    tile "By status": count Ticket group by status
`

// passportPilotSize is the "prepared node" steady-state table size: a small
// business pilot, not the P2 bulk-load scale.
const passportPilotSize = 1000

var (
	passportAdmin  = eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Admin"}
	passportBot    = eventstore.Actor{Type: eventstore.ActorAgent, ID: "agent-1", Role: "Bot"}
	passportReader = eventstore.Actor{Type: eventstore.ActorHuman, ID: "reader-1", Role: "Reader"}
	passportBasis  = &eventstore.Basis{Type: "human", ID: "root"}
)

func passportModel(tb testing.TB) *dsl.Model {
	tb.Helper()
	model, errs := dsl.Compile(map[string]string{"passport.dsl": passportPack})
	if len(errs) > 0 {
		tb.Fatalf("passport pack must compile: %v", errs[0])
	}
	return model
}

// seedPassportNode builds a "prepared node": passportPilotSize tickets spread
// evenly New/InProgress/Done, so list/dashboard/tick all see realistic mixed
// data rather than an all-default-state table.
func seedPassportNode(tb testing.TB) *Engine {
	tb.Helper()
	ctx := context.Background()
	eng, err := New(ctx, passportModel(tb), eventstore.NewMemStore(nil))
	if err != nil {
		tb.Fatal(err)
	}
	for i := 0; i < passportPilotSize; i++ {
		rec, err := eng.Create(ctx, passportAdmin, "Ticket", map[string]any{
			"title": fmt.Sprintf("ticket-%d", i), "amount": float64(100 + i%50),
		}, passportBasis, "")
		if err != nil {
			tb.Fatalf("seed create %d: %v", i, err)
		}
		switch i % 3 {
		case 1:
			if _, err := eng.Act(ctx, passportAdmin, "Ticket", rec.ID, "start", passportBasis, ""); err != nil {
				tb.Fatalf("seed start %d: %v", i, err)
			}
		case 2:
			if _, err := eng.Act(ctx, passportAdmin, "Ticket", rec.ID, "start", passportBasis, ""); err != nil {
				tb.Fatalf("seed start %d: %v", i, err)
			}
			if _, err := eng.Act(ctx, passportAdmin, "Ticket", rec.ID, "finish", passportBasis, ""); err != nil {
				tb.Fatalf("seed finish %d: %v", i, err)
			}
		}
	}
	return eng
}

// passportPool creates n fresh (New-state) tickets and returns their IDs, for
// sub-benchmarks that transition records without growing the table.
func passportPool(tb testing.TB, eng *Engine, n int) []string {
	tb.Helper()
	ctx := context.Background()
	pool := make([]string, n)
	for i := 0; i < n; i++ {
		rec, err := eng.Create(ctx, passportAdmin, "Ticket", map[string]any{"title": "pool"}, passportBasis, "")
		if err != nil {
			tb.Fatalf("pool create %d: %v", i, err)
		}
		pool[i] = rec.ID
	}
	return pool
}

// passportCycle is start->finish->reopen->start->... — the pool record at
// slot i%len(pool) is always in the state that action expects, for ANY i.
var passportCycle = []string{"start", "finish", "reopen"}

func BenchmarkPassportP1PilotMix(b *testing.B) {
	ctx := context.Background()

	b.Run("create", func(b *testing.B) {
		eng := seedPassportNode(b)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := eng.Create(ctx, passportBot, "Ticket", map[string]any{
				"title": fmt.Sprintf("mix-create-%d", i), "amount": 42.0,
			}, passportBasis, ""); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("transition", func(b *testing.B) {
		eng := seedPassportNode(b)
		const poolSize = 60
		pool := passportPool(b, eng, poolSize)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			id := pool[i%poolSize]
			action := passportCycle[(i/poolSize)%3]
			if _, err := eng.Act(ctx, passportBot, "Ticket", id, action, passportBasis, ""); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("list_read", func(b *testing.B) {
		eng := seedPassportNode(b)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := eng.Query(ctx, passportReader, "Ticket", QueryOpts{Limit: 25}); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("dashboard", func(b *testing.B) {
		eng := seedPassportNode(b)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := eng.Dashboard(ctx, passportReader, "TicketStats"); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("tick", func(b *testing.B) {
		eng := seedPassportNode(b)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err := eng.Tick(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})

	// combined_round: one interleaved unit shaped like the H10 profile — 5
	// MCP-agent creates+transitions : 10 UI-reader list/dashboard reads : 1
	// automation tick — for a single "X mutations/s on profile P1" figure.
	b.Run("combined_round", func(b *testing.B) {
		eng := seedPassportNode(b)
		const poolSize = 30
		pool := passportPool(b, eng, poolSize)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for a := 0; a < 5; a++ { // 5 MCP agents: one create + one transition each
				if _, err := eng.Create(ctx, passportBot, "Ticket", map[string]any{"title": "mix"}, passportBasis, ""); err != nil {
					b.Fatal(err)
				}
				slot := i*5 + a
				id := pool[slot%poolSize]
				action := passportCycle[(slot/poolSize)%3]
				if _, err := eng.Act(ctx, passportBot, "Ticket", id, action, passportBasis, ""); err != nil {
					b.Fatal(err)
				}
			}
			for r := 0; r < 10; r++ { // 10 UI readers: one list query each
				if _, err := eng.Query(ctx, passportReader, "Ticket", QueryOpts{Limit: 25}); err != nil {
					b.Fatal(err)
				}
			}
			if _, err := eng.Dashboard(ctx, passportReader, "TicketStats"); err != nil {
				b.Fatal(err)
			}
			if err := eng.Tick(ctx); err != nil { // the automation tick
				b.Fatal(err)
			}
		}
		// 21 engine calls/round (5 create + 5 act + 10 query + 1 dashboard + 1
		// tick): a supplementary headline number alongside the standard ns/op.
		b.ReportMetric(float64(21*b.N)/b.Elapsed().Seconds(), "ops/s")
	})
}

// --- P2: bulk create -----------------------------------------------------
//
// "10k create of one entity" as a named, deterministic scale: each b.N
// iteration builds a FRESH node and times exactly 10,000 Create calls into
// it — table size at the start of every iteration is 0, so iterations stay
// comparable regardless of how many b.N the run does (fresh-per-iteration is
// also just more obviously correct than relying on Create's cost being
// table-size-independent for this pack, even though it happens to be).

const passportBulkN = 10_000

func BenchmarkPassportP2BulkCreate10k(b *testing.B) {
	ctx := context.Background()
	model := passportModel(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		eng, err := New(ctx, model, eventstore.NewMemStore(nil))
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		for j := 0; j < passportBulkN; j++ {
			if _, err := eng.Create(ctx, passportAdmin, "Ticket", map[string]any{
				"title": fmt.Sprintf("bulk-%d", j), "amount": float64(j),
			}, passportBasis, ""); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ReportMetric(float64(passportBulkN*b.N)/b.Elapsed().Seconds(), "records/s")
}

// --- P4: auth storm --------------------------------------------------------
//
// N6/N9 flag Authenticate as O(actors) per request under a single mutex
// (internal/identity/registry.go Registry.all — a linear scan guarded by
// sync.Mutex, no RWMutex, no hash index). This does not fix that (N6: "не
// переделывать, а измерить") — it measures it under concurrency at two
// registry sizes, so a future hash-index change has a baseline to beat.

func passportRegistry(tb testing.TB, n int) (*identity.Registry, []string) {
	tb.Helper()
	ctx := context.Background()
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	tokens := make([]string, n)
	for i := 0; i < n; i++ {
		token, err := reg.RegisterWithToken(ctx, registrar, fmt.Sprintf("actor-%d", i),
			eventstore.ActorAgent, "Agent", nil, nil, nil)
		if err != nil {
			tb.Fatalf("register actor %d: %v", i, err)
		}
		tokens[i] = token
	}
	return reg, tokens
}

func benchmarkPassportAuth(b *testing.B, actors int) {
	ctx := context.Background()
	reg, tokens := passportRegistry(b, actors)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i int
		for pb.Next() {
			tok := tokens[i%len(tokens)]
			if _, err := reg.Authenticate(ctx, tok); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}

func BenchmarkPassportP4Auth100(b *testing.B)  { benchmarkPassportAuth(b, 100) }
func BenchmarkPassportP4Auth1000(b *testing.B) { benchmarkPassportAuth(b, 1000) }
