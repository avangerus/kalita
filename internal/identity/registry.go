package identity

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Registry is the actor directory: a projection over actor.* events in the
// journal. Registration, key rotation and disabling are themselves journal
// events — who created which agent with which key is part of the audit trail.
type Registry struct {
	store eventstore.Store
}

func NewRegistry(store eventstore.Store) *Registry {
	return &Registry{store: store}
}

// actorPayload is the payload of actor.registered / actor.key_rotated events.
type actorPayload struct {
	ActorType eventstore.ActorType `json:"actor_type"`
	Role      string               `json:"role,omitempty"`
	PublicKey []byte               `json:"public_key,omitempty"`
}

// ActorInfo is the current state of an actor derived from the journal.
type ActorInfo struct {
	ID        string
	Type      eventstore.ActorType
	Role      string
	PublicKey ed25519.PublicKey
	Disabled  bool
}

var (
	ErrActorExists   = errors.New("identity: actor already registered")
	ErrActorUnknown  = errors.New("identity: actor not registered")
	ErrActorDisabled = errors.New("identity: actor is disabled")
	ErrBadSignature  = errors.New("identity: signature does not verify")
)

// Register appends actor.registered. registrar is the (human) actor performing
// the registration; agents cannot register agents in v0.
func (r *Registry) Register(ctx context.Context, registrar eventstore.Actor, id string, typ eventstore.ActorType, role string, pub ed25519.PublicKey, basis *eventstore.Basis) error {
	if registrar.Type != eventstore.ActorHuman {
		return fmt.Errorf("identity: only humans register actors in v0, got %s", registrar.Type)
	}
	existing, err := r.lookup(ctx, id)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrActorExists
	}
	payload, err := json.Marshal(actorPayload{ActorType: typ, Role: role, PublicKey: pub})
	if err != nil {
		return err
	}
	_, err = r.store.Append(ctx, eventstore.AppendInput{
		Actor:   registrar,
		Kind:    eventstore.ActorRegistered,
		Subject: eventstore.Subject{ActorID: id},
		Payload: payload,
		Basis:   basis,
	})
	return err
}

// RotateKey appends actor.key_rotated with the new public key.
func (r *Registry) RotateKey(ctx context.Context, registrar eventstore.Actor, id string, pub ed25519.PublicKey, basis *eventstore.Basis) error {
	info, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(actorPayload{ActorType: info.Type, Role: info.Role, PublicKey: pub})
	if err != nil {
		return err
	}
	_, err = r.store.Append(ctx, eventstore.AppendInput{
		Actor:   registrar,
		Kind:    eventstore.ActorKeyRotated,
		Subject: eventstore.Subject{ActorID: id},
		Payload: payload,
		Basis:   basis,
	})
	return err
}

// Disable appends actor.disabled; the actor's signatures stop verifying.
func (r *Registry) Disable(ctx context.Context, registrar eventstore.Actor, id string, basis *eventstore.Basis) error {
	if _, err := r.Get(ctx, id); err != nil {
		return err
	}
	_, err := r.store.Append(ctx, eventstore.AppendInput{
		Actor:   registrar,
		Kind:    eventstore.ActorDisabled,
		Subject: eventstore.Subject{ActorID: id},
		Basis:   basis,
	})
	return err
}

// Get returns the actor or ErrActorUnknown.
func (r *Registry) Get(ctx context.Context, id string) (*ActorInfo, error) {
	info, err := r.lookup(ctx, id)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, ErrActorUnknown
	}
	return info, nil
}

// VerifySignature checks msg/sig against the actor's current key. Disabled
// actors never verify.
func (r *Registry) VerifySignature(ctx context.Context, id string, msg, sig []byte) error {
	info, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	if info.Disabled {
		return ErrActorDisabled
	}
	if len(info.PublicKey) == 0 {
		return fmt.Errorf("identity: actor %s has no key", id)
	}
	if !ed25519.Verify(info.PublicKey, msg, sig) {
		return ErrBadSignature
	}
	return nil
}

// lookup replays actor.* events. Linear scan is fine at week-1 scale; this
// becomes a cached projection with the projection subsystem (week 3).
func (r *Registry) lookup(ctx context.Context, id string) (*ActorInfo, error) {
	events, err := r.store.All(ctx)
	if err != nil {
		return nil, err
	}
	var info *ActorInfo
	for _, e := range events {
		if e.Subject.ActorID != id {
			continue
		}
		switch e.Kind {
		case eventstore.ActorRegistered, eventstore.ActorKeyRotated:
			var p actorPayload
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				return nil, fmt.Errorf("identity: corrupt payload at seq %d: %w", e.Seq, err)
			}
			if info == nil {
				info = &ActorInfo{ID: id}
			}
			info.Type = p.ActorType
			info.Role = p.Role
			info.PublicKey = p.PublicKey
		case eventstore.ActorDisabled:
			if info != nil {
				info.Disabled = true
			}
		}
	}
	return info, nil
}
