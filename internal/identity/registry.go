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
	"sort"
	"sync"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Registry is the actor directory: a projection over actor.* events in the
// journal. Registration, key rotation and disabling are themselves journal
// events — who created which agent with which key is part of the audit trail.
//
// The projection is incremental: a seq watermark + cache, refreshed via
// Store.Since. Authentication is on the hot path of every request — it must
// not rescan history (the event storm the platform itself generates).
type Registry struct {
	store eventstore.Store

	mu        sync.Mutex
	watermark uint64
	cache     map[string]*ActorInfo
}

func NewRegistry(store eventstore.Store) *Registry {
	return &Registry{store: store, cache: map[string]*ActorInfo{}}
}

// ActorMeta describes what stands behind an actor — for agents: which model,
// whose endpoint, who answers for it. Lands in the journal at registration:
// "which model acted" is part of provenance.
type ActorMeta struct {
	Model       string `json:"model,omitempty"`
	Endpoint    string `json:"endpoint,omitempty"`
	Owner       string `json:"owner,omitempty"`
	Description string `json:"description,omitempty"`
}

// actorPayload is the payload of actor.registered / actor.key_rotated events.
// TokenHash is sha256 of the bearer token used for MCP authentication; the
// token itself never enters the journal.
type actorPayload struct {
	ActorType eventstore.ActorType `json:"actor_type"`
	Role      string               `json:"role,omitempty"`
	PublicKey []byte               `json:"public_key,omitempty"`
	TokenHash []byte               `json:"token_hash,omitempty"`
	Meta      *ActorMeta           `json:"meta,omitempty"`
}

// ActorInfo is the current state of an actor derived from the journal.
type ActorInfo struct {
	ID        string               `json:"id"`
	Type      eventstore.ActorType `json:"type"`
	Role      string               `json:"role"`
	PublicKey ed25519.PublicKey    `json:"-"`
	TokenHash []byte               `json:"-"`
	Meta      *ActorMeta           `json:"meta,omitempty"`
	Disabled  bool                 `json:"disabled"`
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
	_, err := r.register(ctx, registrar, id, typ, role, pub, nil, nil, basis)
	return err
}

// RegisterWithToken registers an actor and issues a bearer token for MCP
// authentication. The token is returned ONCE; only its hash is journaled.
// meta may be nil; for agents it should say which model stands behind them.
func (r *Registry) RegisterWithToken(ctx context.Context, registrar eventstore.Actor, id string, typ eventstore.ActorType, role string, pub ed25519.PublicKey, meta *ActorMeta, basis *eventstore.Basis) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	if _, err := r.register(ctx, registrar, id, typ, role, pub, hash[:], meta, basis); err != nil {
		return "", err
	}
	return token, nil
}

func (r *Registry) register(ctx context.Context, registrar eventstore.Actor, id string, typ eventstore.ActorType, role string, pub ed25519.PublicKey, tokenHash []byte, meta *ActorMeta, basis *eventstore.Basis) (*eventstore.Event, error) {
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
	payload, err := json.Marshal(actorPayload{ActorType: typ, Role: role, PublicKey: pub, TokenHash: tokenHash, Meta: meta})
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

// EnsureActor registers an actor idempotently and returns a token. Used by
// bootstrap: a worker presenting the node's shared secret gets a stable
// identity on first start, the same token slot on restarts (re-issued). If the
// actor already exists, a fresh token is issued and the old one rotated out.
func (r *Registry) EnsureActor(ctx context.Context, id string, typ eventstore.ActorType, role string, meta *ActorMeta) (string, error) {
	existing, err := r.lookup(ctx, id)
	if err != nil {
		return "", err
	}
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "bootstrap", Role: "Owner"}
	basis := &eventstore.Basis{Type: "human", ID: "bootstrap"}
	if existing == nil {
		return r.RegisterWithToken(ctx, registrar, id, typ, role, nil, meta, basis)
	}
	// already registered: rotate its token (re-issue), keep role/meta
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	payload, _ := json.Marshal(actorPayload{ActorType: existing.Type, Role: existing.Role,
		PublicKey: existing.PublicKey, TokenHash: hash[:], Meta: existing.Meta})
	if _, err := r.store.Append(ctx, eventstore.AppendInput{
		Actor:   registrar,
		Kind:    eventstore.ActorKeyRotated,
		Subject: eventstore.Subject{ActorID: id},
		Payload: payload,
		Basis:   basis,
	}); err != nil {
		return "", err
	}
	return token, nil
}

// List returns all actors (the directory behind the Agents screen).
func (r *Registry) List(ctx context.Context) ([]*ActorInfo, error) {
	infos, err := r.all(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*ActorInfo, 0, len(infos))
	for _, info := range infos {
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
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

// all returns the directory, folding only events newer than the watermark.
func (r *Registry) all(ctx context.Context) (map[string]*ActorInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	events, err := r.store.Since(ctx, r.watermark)
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		r.watermark = e.Seq
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
			info := r.cache[id]
			if info == nil {
				info = &ActorInfo{ID: id}
				r.cache[id] = info
			}
			info.Type, info.Role, info.PublicKey = p.ActorType, p.Role, p.PublicKey
			if p.TokenHash != nil {
				info.TokenHash = p.TokenHash
			}
			if p.Meta != nil {
				info.Meta = p.Meta
			}
		case eventstore.ActorDisabled:
			if info, ok := r.cache[id]; ok {
				info.Disabled = true
			}
		}
	}
	// snapshot copy: callers iterate without holding the registry lock
	out := make(map[string]*ActorInfo, len(r.cache))
	for k, v := range r.cache {
		cp := *v
		out[k] = &cp
	}
	return out, nil
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

// Disable appends actor.disabled; the actor's token and signatures stop
// working immediately. Only humans revoke.
func (r *Registry) Disable(ctx context.Context, registrar eventstore.Actor, id string, basis *eventstore.Basis) error {
	if registrar.Type != eventstore.ActorHuman {
		return fmt.Errorf("identity: only humans disable actors")
	}
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

// lookup resolves one actor through the incremental cache.
func (r *Registry) lookup(ctx context.Context, id string) (*ActorInfo, error) {
	infos, err := r.all(ctx)
	if err != nil {
		return nil, err
	}
	return infos[id], nil
}
