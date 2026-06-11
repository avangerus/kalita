package eventstore

import (
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
)

// Checkpoints seal the head of the chain with the node key (EVENT-STORE-v0 §3).
// A sealed checkpoint exported off the node proves the journal was not
// rewritten after the seal — even by someone with full database access.

// CheckpointPayload is the payload of a checkpoint.sealed event.
type CheckpointPayload struct {
	SealedSeq  uint64 `json:"sealed_seq"`
	SealedHash []byte `json:"sealed_hash"`
}

const sealDomain = "kalita-checkpoint-v1"

// sealMessage is the byte string the node key signs: domain || seq || hash.
func sealMessage(seq uint64, hash []byte) []byte {
	msg := make([]byte, 0, len(sealDomain)+8+len(hash))
	msg = append(msg, sealDomain...)
	msg = binary.BigEndian.AppendUint64(msg, seq)
	msg = append(msg, hash...)
	return msg
}

var ErrNothingToSeal = errors.New("eventstore: journal is empty, nothing to seal")

// Seal signs the current head with the node key and appends a
// checkpoint.sealed event.
func Seal(ctx context.Context, s Store, nodeID string, key ed25519.PrivateKey) (*Event, error) {
	seq, hash, err := s.Head(ctx)
	if err != nil {
		return nil, err
	}
	if seq == 0 {
		return nil, ErrNothingToSeal
	}
	payload, err := json.Marshal(CheckpointPayload{SealedSeq: seq, SealedHash: hash})
	if err != nil {
		return nil, err
	}
	return s.Append(ctx, AppendInput{
		Actor:     Actor{Type: ActorSystem, ID: nodeID},
		Kind:      CheckpointSealed,
		Subject:   Subject{},
		Payload:   payload,
		Signature: ed25519.Sign(key, sealMessage(seq, hash)),
	})
}

// VerifyCheckpoints checks every checkpoint in the journal against the node's
// public key: the signature must verify and the sealed (seq, hash) must match
// the actual event at that seq. Returns nil if all checkpoints hold.
func VerifyCheckpoints(events []*Event, nodePub ed25519.PublicKey) error {
	hashBySeq := make(map[uint64][]byte, len(events))
	for _, e := range events {
		hashBySeq[e.Seq] = e.Hash
	}
	checked := 0
	for _, e := range events {
		if e.Kind != CheckpointSealed {
			continue
		}
		var p CheckpointPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("checkpoint at seq %d: bad payload: %w", e.Seq, err)
		}
		if p.SealedSeq >= e.Seq {
			return fmt.Errorf("checkpoint at seq %d seals a future seq %d", e.Seq, p.SealedSeq)
		}
		actual, ok := hashBySeq[p.SealedSeq]
		if !ok {
			return fmt.Errorf("checkpoint at seq %d seals missing seq %d", e.Seq, p.SealedSeq)
		}
		if string(actual) != string(p.SealedHash) {
			return fmt.Errorf("checkpoint at seq %d: sealed hash differs from journal at seq %d", e.Seq, p.SealedSeq)
		}
		if !ed25519.Verify(nodePub, sealMessage(p.SealedSeq, p.SealedHash), e.Signature) {
			return fmt.Errorf("checkpoint at seq %d: node signature invalid", e.Seq)
		}
		checked++
	}
	if checked == 0 {
		return errors.New("eventstore: no checkpoints in journal")
	}
	return nil
}
