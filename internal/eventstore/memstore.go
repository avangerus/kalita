package eventstore

import (
	"errors"
	"sync"
	"time"
)

// AppendInput is what callers provide; the store assigns EventID, Seq, TS,
// PrevHash and Hash. Signature handling arrives with the identity subsystem.
type AppendInput struct {
	Actor          Actor
	Kind           Kind
	Subject        Subject
	Payload        []byte
	Basis          *Basis
	DefVersion     uint64
	IdempotencyKey string
}

var ErrEmptyActorID = errors.New("eventstore: actor id must not be empty")

// MemStore is the in-memory reference implementation of the journal. It defines
// the semantics the PostgreSQL store must reproduce exactly; it also backs unit
// tests and the future `simulate` replay.
type MemStore struct {
	mu         sync.Mutex
	events     []*Event
	byIdemKey  map[string]*Event
	now        func() time.Time
}

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
func (s *MemStore) Append(in AppendInput) (*Event, error) {
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
func (s *MemStore) All() []*Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Event, len(s.events))
	copy(out, s.events)
	return out
}

// Verify checks the integrity of the whole chain.
func (s *MemStore) Verify() error {
	return VerifyChain(s.All())
}
