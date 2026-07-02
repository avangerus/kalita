// Package goldenfmt is shared, non-production tooling for the golden-journal
// fold-compatibility gate (docs/lld/LLD-03-upgrade-eras.md §3/§7 PR-0). It is
// imported by cmd/goldengen (the generator) and by tests/golden_replay_test.go
// (the gate) so both sides read, write, dump and normalize golden fixtures the
// same way. It lives under cmd/goldengen rather than internal/ on purpose:
// PR-0's contract is that production code under internal/ is not touched, and
// everything here is reachable purely through internal/engine, internal/
// eventstore, internal/dsl and internal/identity's existing public API.
package goldenfmt

import (
	"context"
	"errors"

	"github.com/avangerus/kalita/internal/eventstore"
)

// FrozenStore is a read-only eventstore.Store backed by a fixed, pre-parsed
// event slice: seq, hash, prev_hash and event_id are exactly as given, never
// reassigned. It exists to replay a frozen testdata/golden/<ver>/journal.jsonl
// file through the CURRENT binary's fold logic.
//
// eventstore.MemStore cannot be reused for this: MemStore.Append always mints
// a fresh EventID/Seq/Hash for whatever AppendInput it receives (see
// eventstore's buildEvent) — there is no public way to make it accept
// pre-built events with their original identity intact, and the internal
// event slice is unexported. Proving fold-compatibility means feeding the
// CURRENT binary a journal recorded by a PAST run and checking the resulting
// projections, which requires the replayed events to keep that original
// identity. A small purpose-built Store outside internal/ is the minimal way
// to do that without exporting a loader out of eventstore's private state.
type FrozenStore struct {
	events    []*eventstore.Event
	byIdemKey map[string]*eventstore.Event
}

var _ eventstore.Store = (*FrozenStore)(nil)

// NewFrozenStore wraps events (already ordered by Seq, e.g. from ReadJournal)
// for replay. The slice is copied; callers may reuse or discard their own.
func NewFrozenStore(events []*eventstore.Event) *FrozenStore {
	cp := make([]*eventstore.Event, len(events))
	copy(cp, events)
	byIdemKey := make(map[string]*eventstore.Event, len(events))
	for _, e := range cp {
		if e.IdempotencyKey != "" {
			byIdemKey[e.IdempotencyKey] = e
		}
	}
	return &FrozenStore{events: cp, byIdemKey: byIdemKey}
}

// All returns the frozen journal in order.
func (s *FrozenStore) All(_ context.Context) ([]*eventstore.Event, error) {
	out := make([]*eventstore.Event, len(s.events))
	copy(out, s.events)
	return out, nil
}

// Since returns events with seq > afterSeq — used by identity.Registry's
// incremental fold, exactly as it would run against a live store.
func (s *FrozenStore) Since(_ context.Context, afterSeq uint64) ([]*eventstore.Event, error) {
	var out []*eventstore.Event
	for _, e := range s.events {
		if e.Seq > afterSeq {
			out = append(out, e)
		}
	}
	return out, nil
}

// Head returns the seq and hash of the last event (0, nil on an empty journal).
func (s *FrozenStore) Head(_ context.Context) (uint64, []byte, error) {
	if len(s.events) == 0 {
		return 0, nil, nil
	}
	last := s.events[len(s.events)-1]
	return last.Seq, last.Hash, nil
}

// ByIdemKey returns the event stored under key, or nil.
func (s *FrozenStore) ByIdemKey(_ context.Context, key string) (*eventstore.Event, error) {
	return s.byIdemKey[key], nil
}

// Verify re-checks the frozen chain — a sanity gate that the committed
// journal.jsonl itself was not hand-edited into an inconsistent state.
func (s *FrozenStore) Verify(ctx context.Context) error {
	events, _ := s.All(ctx)
	return eventstore.VerifyChain(events)
}

// Append always fails: FrozenStore replays history, it does not extend it.
// engine.New only calls All at construction time, so this is never exercised
// by the replay gate; it exists solely to satisfy the Store interface.
func (s *FrozenStore) Append(context.Context, eventstore.AppendInput) (*eventstore.Event, error) {
	return nil, errors.New("goldenfmt: FrozenStore is read-only (replay of a frozen golden journal); it does not accept new events")
}
