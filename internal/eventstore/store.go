package eventstore

import "context"

// Store is the journal contract. MemStore is the reference implementation and
// the semantics oracle; PGStore must behave identically (shared test suite).
type Store interface {
	// Append adds an event. Same idempotency key returns the original event.
	Append(ctx context.Context, in AppendInput) (*Event, error)
	// All returns the full journal ordered by seq.
	All(ctx context.Context) ([]*Event, error)
	// Head returns the seq and hash of the last event (0, nil on empty journal).
	Head(ctx context.Context) (uint64, []byte, error)
	// Verify re-reads the journal and checks the hash chain.
	Verify(ctx context.Context) error
}
