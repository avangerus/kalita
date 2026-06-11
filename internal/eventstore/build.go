package eventstore

import (
	"time"

	"github.com/google/uuid"
)

// buildEvent constructs and hashes an event. Both stores go through this
// single constructor so their semantics cannot drift apart.
func buildEvent(in AppendInput, seq uint64, prevHash []byte, now func() time.Time) (*Event, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	e := &Event{
		EventID: id.String(),
		Seq:     seq,
		// Truncated to microseconds: PostgreSQL timestamptz precision. The hash
		// must survive a write-read round trip through any backing store.
		TS:             now().UTC().Truncate(time.Microsecond),
		Actor:          in.Actor,
		Kind:           in.Kind,
		Subject:        in.Subject,
		Payload:        in.Payload,
		Basis:          in.Basis,
		DefVersion:     in.DefVersion,
		IdempotencyKey: in.IdempotencyKey,
		PrevHash:       prevHash,
		Signature:      in.Signature,
	}
	e.Hash, err = computeHash(prevHash, e)
	if err != nil {
		return nil, err
	}
	return e, nil
}
