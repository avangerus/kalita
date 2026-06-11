package identity

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
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
// TokenHash is sha256 of the bearer token used for MCP authentication; the
// token itself never enters the journal.
type actorPayload struct {
	ActorType eventstore.ActorType `json:"actor_type"`
	Role      string               `json:"role,omitempty"`
	PublicKey []byte               `json:"public_key,omitempty"`
	TokenHash []byte               `json:"token_hash,omitempty"`
}

// ActorInfo is the current state of an actor derived from the journal.
type ActorInfo struct {
	ID        string
	Type      eventstore.ActorType
	Role      string
	PublicKey ed25519.PublicKey
	TokenHash []byte
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
	_, err := r.register(ctx, registrar, id, typ, role, pub, nil, basis)
	return err
}

// RegisterWithToken registers an actor and issues a bearer token for MCP
// authentication. The token is returned ONCE; only its hash is journaled.
func (r *Registry) RegisterWithToken(ctx context.Context, registrar eventstore.Actor, id string, typ eventstore.ActorType, role string, pub ed25519.PublicKey, basis *eventstore.Basis) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	if _, err := r.register(ctx, registrar, id, typ, role, pub, hash[:], basis); err != nil {
		return "", err
	}
	return token, nil
}

func (r *Registry) register(ctx context.Context, registrar eventstore.Actor, id string, typ eventstore.ActorType, role string, pub ed25519.PublicKey, tokenHash []byte, basis *eventstore.Basis) (*eventstore.Event, error) {
	if registrar.Type != eventstore.ActorHuman {
		return nil, fmt.Errorf("identity: only humans register actors in v0, got %s", registrar.Type)
	}
	existing, err := r.lookup(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrActorExists
	}
	payload, err := json.Marshal(actorPayload{ActorType: typ, Role: role, PublicKey: pub, TokenHash: tokenHash})
	if err != nil {
		return nil, err
	}
	return r.store.Append(ctx, eventstore.AppendInput{
		Actor:   registrar,
		Kind:    eventstore.ActorRegistered,
		Subject: eventstore.Subject{ActorID: id},
		Payload: payload,
		Basis:   basis,
	})
}

// Authenticate resolves a bearer token to an active actor.
func (r *Registry) Authenticate(ctx context.Context, token string) (*ActorInfo, error) {
	hash := sha256.Sum256([]byte(token))
	infos, err := r.all(ctx)
	if err != nil {
		return nil, err
	}
	for _, info := range infos {
		if info.TokenHash != nil && subtle.ConstantTimeCompare(info.TokenHash, hash[:]) == 1 {
			if info.Disabled {
				return nil, ErrActorDisabled
			}
			return info, nil
		}
	}
	return nil, ErrActorUnknown
}

// all replays actor.* events into the full directory.
func (r *Registry) all(ctx context.Context) (map[string]*ActorInfo, error) {
	events, err := r.store.All(ctx)
	if err != nil {
		return nil, err
	}
	infos := map[string]*ActorInfo{}
	for _, e := range events {
		id := e.Subject.ActorID
		if id == "" {
			continue
		}
		switch e.Kind {
		case eventstore.ActorRegistered, eventstore.ActorKeyRotated:
			var p actorPayload
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				continue
			}
			info := infos[id]
			if info == nil {
				info = &ActorInfo{ID: id}
				infos[id] = info
			}
			info.Type, info.Role, info.PublicKey = p.ActorType, p.Role, p.PublicKey
			if p.TokenHash != nil {
				info.TokenHash = p.TokenHash
			}
		case eventstore.ActorDisabled:
			if info, ok := infos[id]; ok {
				info.Disabled = true
			}
		}
	}
	return infos, nil
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
