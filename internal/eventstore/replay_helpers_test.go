// Package eventstore_test (external/black-box) so this file can call
// engine.New() — the real startup replay path — without an import cycle:
// internal/engine already imports internal/eventstore, so anything that
// wants the genuine replay code path must sit on the far side of that edge,
// not inside package eventstore itself.
package eventstore_test

// Shared synthetic-journal generator for the P3 "journal growth" passport
// profile (docs/RELEASE-1.0.md N9) AND the hard replay-budget gate
// (replay_budget_test.go). Deliberately NOT behind the `passport` build tag,
// unlike replay_bench_test.go's benchmarks: TestReplayBudget1M in
// replay_budget_test.go must compile and run in the ordinary `go test ./...`
// PR gate (no -tags=passport needed there), so the helpers it shares with the
// benchmarks live in this untagged file.

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
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
// in for the real Engine.Create/Act, which is the whole point: P3/the budget
// test measure replay of an ALREADY-WRITTEN journal, not the cost of writing
// one.
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
//
// Deterministic by construction: record ids and payload contents are derived
// solely from the loop counter i (no time.Now/rand on the data path), so the
// generated journal — and anything timed against it — is reproducible run to
// run, machine to machine.
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
