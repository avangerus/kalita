package eventstore

import (
	"context"
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

// All returns the journal in order. Callers must not mutate the events.
func (s *MemStore) All(_ context.Context) ([]*Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Event, len(s.events))
	copy(out, s.events)
	return out, nil
}

// ByIdemKey returns the event stored under the key, or nil.
func (s *MemStore) ByIdemKey(_ context.Context, key string) (*Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.byIdemKey[key]; ok {
		return e, nil
	}
	return nil, nil
}

// Since returns events with seq > afterSeq.
func (s *MemStore) Since(_ context.Context, afterSeq uint64) ([]*Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if afterSeq >= uint64(len(s.events)) {
		return nil, nil
	}
	out := make([]*Event, len(s.events)-int(afterSeq))
	copy(out, s.events[afterSeq:])
	return out, nil
}

// Head returns the seq and hash of the last event.
func (s *MemStore) Head(_ context.Context) (uint64, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) == 0 {
		return 0, nil, nil
	}
	last := s.events[len(s.events)-1]
	return last.Seq, last.Hash, nil
}

// Verify checks the integrity of the whole chain.
func (s *MemStore) Verify(ctx context.Context) error {
	events, err := s.All(ctx)
	if err != nil {
		return err
	}
	return VerifyChain(events)
}
