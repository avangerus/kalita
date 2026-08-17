package eventstore

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// AppendInput is what callers provide; the store assigns EventID, Seq, TS,
// PrevHash and Hash. Signature, when present, is the actor's signature over
// the domain-specific message (e.g. a checkpoint seal); it is stored verbatim
// and excluded from the hash envelope.
type AppendInput struct {
	Actor          Actor
	Kind           Kind
	Subject        Subject
	Payload        []byte
	Basis          *Basis
	DefVersion     uint64
	IdempotencyKey string
	Signature      []byte
	// SchemaV, when 0, is omitted from the hash envelope entirely (LLD-01
	// §1.1): existing callers that never set it keep producing byte-identical
	// events. Callers writing an LLD-01 kind set this to 1.
	SchemaV int
}

var ErrEmptyActorID = errors.New("eventstore: actor id must not be empty")

// MemStore is the in-memory reference implementation of the journal. It defines
// the semantics the PostgreSQL store must reproduce exactly; it also backs unit
// tests and the future `simulate` replay.
type MemStore struct {
	mu        sync.Mutex
	events    []*Event
	byIdemKey map[string]*Event
	now       func() time.Time
}

var _ Store = (*MemStore)(nil)

// NewMemStore creates an empty journal. nowFn may be nil (defaults to time.Now);
// tests inject a deterministic clock.
func NewMemStore(nowFn func() time.Time) *MemStore {
	if nowFn == nil {
		nowFn = time.Now
	}
	return &MemStore{
		byIdemKey: make(map[string]*Event),
		now:       nowFn,
	}
}

// Append adds an event to the journal. If in.IdempotencyKey was seen before,
// the previously stored event is returned and nothing is appended.
func (s *MemStore) Append(_ context.Context, in AppendInput) (*Event, error) {
	if in.Actor.ID == "" {
		return nil, ErrEmptyActorID
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if in.IdempotencyKey != "" {
		if prior, ok := s.byIdemKey[in.IdempotencyKey]; ok {
			return prior, nil
		}
	}

	prev := genesisHash
	if n := len(s.events); n > 0 {
		prev = s.events[n-1].Hash
	}

	e, err := buildEvent(in, uint64(len(s.events))+1, prev, s.now)
	if err != nil {
		return nil, err
	}

	s.events = append(s.events, e)
	if in.IdempotencyKey != "" {
		s.byIdemKey[in.IdempotencyKey] = e
	}
	return e, nil
}

// All returns the journal in order. Every Event is a deep copy (§7.13):
// mutating a returned Event, or a slice/map field on it, never reaches back
// into the store. This is what makes MemStore behave like PGStore, which
// always deserializes a brand-new Event per query and shares memory with
// nothing — without the copy, MemStore would hand out *Event pointers
// aliasing its own internal slice, so a caller mutating one would corrupt
// the journal (and silently diverge from PGStore, which could never exhibit
// that bug by construction). See cloneEvent.
func (s *MemStore) All(_ context.Context) ([]*Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Event, len(s.events))
	for i, e := range s.events {
		out[i] = cloneEvent(e)
	}
	return out, nil
}

// ByIdemKey returns a deep copy of the event stored under the key, or nil
// (§7.13 — same aliasing hazard as All: byIdemKey points at the same *Event
// as the events slice, so this must not hand out the shared pointer either).
func (s *MemStore) ByIdemKey(_ context.Context, key string) (*Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.byIdemKey[key]; ok {
		return cloneEvent(e), nil
	}
	return nil, nil
}

// Since returns deep copies of events with seq > afterSeq (§7.13, same
// reasoning as All).
func (s *MemStore) Since(_ context.Context, afterSeq uint64) ([]*Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if afterSeq >= uint64(len(s.events)) {
		return nil, nil
	}
	tail := s.events[afterSeq:]
	out := make([]*Event, len(tail))
	for i, e := range tail {
		out[i] = cloneEvent(e)
	}
	return out, nil
}

// Head returns the seq and hash of the last event. The hash is copied
// (§7.13): last.Hash is a slice into the stored Event, so returning it
// directly would let a caller mutating the returned []byte corrupt the
// journal's own chain — the same aliasing hazard All/Since/ByIdemKey have,
// just for one field instead of a whole Event.
func (s *MemStore) Head(_ context.Context) (uint64, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) == 0 {
		return 0, nil, nil
	}
	last := s.events[len(s.events)-1]
	return last.Seq, append([]byte(nil), last.Hash...), nil
}

// cloneEvent returns a deep copy of e: every mutable reference field
// (Payload, PrevHash, Hash, Signature, Basis, Actor.Attrs) is duplicated, not
// shared. MemStore stores *Event pointers directly in its append-only slice
// (no serialize/deserialize round-trip the way PGStore has by construction),
// so every read path that hands one of those pointers to a caller must clone
// it first — otherwise a caller mutating a returned Event reaches straight
// into the store's own state, and the same journal behaves differently
// depending on which Store backs it (§7.13).
func cloneEvent(e *Event) *Event {
	if e == nil {
		return nil
	}
	cp := *e // EventID/Seq/TS/Kind/Subject/DefVersion/IdempotencyKey/SchemaV are plain values — copied for free
	if e.Actor.Attrs != nil {
		attrs := make(map[string]any, len(e.Actor.Attrs))
		for k, v := range e.Actor.Attrs {
			attrs[k] = v
		}
		cp.Actor.Attrs = attrs
	}
	if e.Payload != nil {
		cp.Payload = append(json.RawMessage(nil), e.Payload...)
	}
	if e.Basis != nil {
		b := *e.Basis
		cp.Basis = &b
	}
	if e.PrevHash != nil {
		cp.PrevHash = append([]byte(nil), e.PrevHash...)
	}
	if e.Hash != nil {
		cp.Hash = append([]byte(nil), e.Hash...)
	}
	if e.Signature != nil {
		cp.Signature = append([]byte(nil), e.Signature...)
	}
	return &cp
}

// Verify checks the integrity of the whole chain.
func (s *MemStore) Verify(ctx context.Context) error {
	events, err := s.All(ctx)
	if err != nil {
		return err
	}
	return VerifyChain(events)
}

// Ping reports the in-memory journal as always reachable.
func (s *MemStore) Ping(ctx context.Context) error { return nil }
