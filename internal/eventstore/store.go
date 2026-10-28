package eventstore

import "context"

// Store is the journal contract. MemStore is the reference implementation and
// the semantics oracle; PGStore must behave identically (shared test suite).
type Store interface {
	// Append adds an event. Same idempotency key returns the original event.
	Append(ctx context.Context, in AppendInput) (*Event, error)
	// All returns the full journal ordered by seq.
	All(ctx context.Context) ([]*Event, error)
	// Since returns events with seq > afterSeq, ordered. The incremental-read
	// primitive: projections keep a watermark instead of rescanning history.
	Since(ctx context.Context, afterSeq uint64) ([]*Event, error)
	// Head returns the seq and hash of the last event (0, nil on empty journal).
	Head(ctx context.Context) (uint64, []byte, error)
	// ByIdemKey returns the event stored under the idempotency key, or nil.
	// Callers check it BEFORE validation: a retry must short-circuit ahead of
	// any state-dependent checks (uniqueness would otherwise reject the retry).
	ByIdemKey(ctx context.Context, key string) (*Event, error)
	// Verify re-reads the journal and checks the hash chain.
	Verify(ctx context.Context) error
	// Ping reports whether the journal backend is reachable (liveness of the
	// storage itself) — the readiness probe's journal half (LLD-09 §3).
	Ping(ctx context.Context) error
}

// RecordReader is an optional Store capability: the events of one record
// (Subject.Entity + Subject.RecordID), ordered by seq, read without scanning
// the whole journal. Callers fall back to All when a store lacks it.
type RecordReader interface {
	ForRecord(ctx context.Context, entity, recordID string) ([]*Event, error)
}
