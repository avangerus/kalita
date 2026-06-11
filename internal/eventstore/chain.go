package eventstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// hashEnvelope is the canonical form of an event for hashing: every field that
// is part of the chain, in fixed declaration order, excluding Hash and
// Signature themselves. encoding/json marshals struct fields in declaration
// order, which makes the encoding deterministic for a fixed struct.
type hashEnvelope struct {
	EventID        string          `json:"event_id"`
	Seq            uint64          `json:"seq"`
	TS             string          `json:"ts"` // RFC3339Nano, UTC — normalized explicitly
	Actor          Actor           `json:"actor"`
	Kind           Kind            `json:"kind"`
	Subject        Subject         `json:"subject"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	Basis          *Basis          `json:"basis,omitempty"`
	DefVersion     uint64          `json:"def_version"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
}

// canonicalJSON returns the deterministic byte form of the event used for hashing.
func canonicalJSON(e *Event) ([]byte, error) {
	env := hashEnvelope{
		EventID:        e.EventID,
		Seq:            e.Seq,
		TS:             e.TS.UTC().Format("2006-01-02T15:04:05.000000000Z07:00"),
		Actor:          e.Actor,
		Kind:           e.Kind,
		Subject:        e.Subject,
		Payload:        e.Payload,
		Basis:          e.Basis,
		DefVersion:     e.DefVersion,
		IdempotencyKey: e.IdempotencyKey,
	}
	return json.Marshal(env)
}

// computeHash returns SHA-256(prevHash || canonicalJSON(event)).
func computeHash(prevHash []byte, e *Event) ([]byte, error) {
	canon, err := canonicalJSON(e)
	if err != nil {
		return nil, fmt.Errorf("canonical json: %w", err)
	}
	h := sha256.New()
	h.Write(prevHash)
	h.Write(canon)
	return h.Sum(nil), nil
}

// genesisHash is the prev_hash of the very first event.
var genesisHash = make([]byte, sha256.Size)

// ChainBreakError reports the first event at which the hash chain fails to verify.
type ChainBreakError struct {
	Seq    uint64
	Reason string
}

func (e *ChainBreakError) Error() string {
	return fmt.Sprintf("event chain broken at seq %d: %s", e.Seq, e.Reason)
}

// VerifyChain re-computes the hash chain over events (which must be ordered by
// Seq, starting at 1 with no gaps) and returns a ChainBreakError pointing at
// the first tampered or out-of-order event, or nil if the chain is intact.
func VerifyChain(events []*Event) error {
	prev := genesisHash
	var prevSeq uint64
	for _, e := range events {
		if e.Seq != prevSeq+1 {
			return &ChainBreakError{Seq: e.Seq, Reason: fmt.Sprintf("sequence gap: expected %d", prevSeq+1)}
		}
		if !bytes.Equal(e.PrevHash, prev) {
			return &ChainBreakError{Seq: e.Seq, Reason: "prev_hash mismatch"}
		}
		want, err := computeHash(prev, e)
		if err != nil {
			return &ChainBreakError{Seq: e.Seq, Reason: err.Error()}
		}
		if !bytes.Equal(e.Hash, want) {
			return &ChainBreakError{Seq: e.Seq, Reason: "hash mismatch (event content altered)"}
		}
		prev = e.Hash
		prevSeq = e.Seq
	}
	return nil
}
