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
	// tokenIndex is a secondary projection folded in lockstep with cache,
	// under the same mu/watermark: string(tokenHash) -> actor id. Authenticate
	// is on the hot path of every request (§7.5, RELEASE-1.0 N9/LLD-12 §2) —
	// without this index it would have to linearly scan every actor's
	// TokenHash, O(actors) per call. This index changes ONLY the lookup
	// structure (a map instead of a scan); it never changes what Authenticate
	// decides — same actor or same refusal, just found in O(1) instead of
	// O(actors). Falls out of the journal exactly like cache: rebuilt by
	// replaying actor.registered/actor.key_rotated, never itself journaled.
	tokenIndex map[string]string

	// invites is the invite projection (LLD-10 PR-2, Cycle-A4): hex(code hash)
	// -> Invite, folded in lockstep with cache under the same mu/watermark.
	// findInvite is O(1) instead of a full journal scan (invites.go).
	invites map[string]*Invite

	// lockdown state (LLD-10 §3), folded from node.lockdown/node.lockdown_lifted
	// under the same mu as the actor cache — see lockdown.go.
	lockdown     bool
	lockdownHash []byte
	lockdownRole string

	// authFails (LLD-10 §4) has its own lock — see authfail.go.
	authFails authFailState

	// credState (LLD-02 §1) has its own lock and watermark — see webauthn.go.
	credState credentialState
}

func NewRegistry(store eventstore.Store) *Registry {
	return &Registry{store: store, cache: map[string]*ActorInfo{}, tokenIndex: map[string]string{}, invites: map[string]*Invite{}}
}

// ActorMeta describes what stands behind an actor — for agents: which model,
// whose endpoint, who answers for it. Lands in the journal at registration:
// "which model acted" is part of provenance.
type ActorMeta struct {
	Model       string `json:"model,omitempty"`
	Endpoint    string `json:"endpoint,omitempty"`
	Owner       string `json:"owner,omitempty"`
	Description string `json:"description,omitempty"`
	// Attrs are ABAC attributes (region, department, clearance…) carried on the
	// actor and exposed to permissions as $me.<attr>.
	Attrs map[string]any `json:"attrs,omitempty"`
	// EgressDomains is the worker's OWN declared whitelist of external domains
	// it may call (LLD-09 §3: "самодекларация воркера", not a network
	// intercept — the kernel cannot and does not inspect the worker's actual
	// traffic, T8). `kalita egress-report` reconciles this declaration
	// against the egress.called facts the worker itself journals via
	// emit_event: a call to a domain outside this list is flagged an anomaly,
	// and a worker with NO declaration here is treated as fully undeclared
	// (every one of its calls is an anomaly) — the honest "lite" boundary
	// this LLD fixes, not a guarantee against a hostile worker.
	EgressDomains []string `json:"egress_domains,omitempty"`
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
	// PasswordHash is the bcrypt hash from the most recent actor.password_set
	// event (LLD-05 §3, PK-4). Never serialized, never the raw password (I-4).
	PasswordHash []byte     `json:"-"`
	Meta         *ActorMeta `json:"meta,omitempty"`
	Disabled     bool       `json:"disabled"`
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
	// LLD-10 PR-1 (Cycle-A4): the existence check and the append are atomic —
	// the whole register body holds r.mu, so two concurrent registrations of
	// the same id can no longer both pass the nil check and double-append
	// (which, under RegisterWithToken, would mint an orphan token whose hash
	// never landed in the journal).
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.registerLocked(ctx, registrar, id, typ, role, pub, tokenHash, meta, basis)
}

// registerLocked is register's body; the caller holds r.mu (used by Redeem,
// which must stay atomic across invite-check + registration + redeem-append).
func (r *Registry) registerLocked(ctx context.Context, registrar eventstore.Actor, id string, typ eventstore.ActorType, role string, pub ed25519.PublicKey, tokenHash []byte, meta *ActorMeta, basis *eventstore.Basis) (*eventstore.Event, error) {
	if err := r.foldLocked(ctx); err != nil {
		return nil, err
	}
	if _, ok := r.cache[id]; ok {
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
	// LLD-10 PR-1 (Cycle-A4): hold r.mu across the existence check and the
	// append, so a bootstrap EnsureActor cannot race a concurrent human
	// Register of the same id into a double registration.
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.foldLocked(ctx); err != nil {
		return "", err
	}
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "bootstrap", Role: "Owner"}
	basis := &eventstore.Basis{Type: "human", ID: "bootstrap"}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	existing := r.cache[id]
	if existing == nil {
		payload, _ := json.Marshal(actorPayload{ActorType: typ, Role: role, PublicKey: nil, TokenHash: hash[:], Meta: meta})
		if _, err := r.store.Append(ctx, eventstore.AppendInput{
			Actor:   registrar,
			Kind:    eventstore.ActorRegistered,
			Subject: eventstore.Subject{ActorID: id},
			Payload: payload,
			Basis:   basis,
		}); err != nil {
			return "", err
		}
		return token, nil
	}
	// already registered: rotate its token (re-issue), keep role/meta
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

// Authenticate resolves a bearer token to an active actor. While the node is
// in lockdown (LLD-10 §3), every token is checked against the emergency admin
// only — normal actors, even valid ones, are refused until lockdown lifts.
//
// Lookup is O(1) via tokenIndex (LLD-12 §2), not a scan of every actor: the
// DECISION is unchanged from the previous linear-scan implementation (same
// actor, same refusal, same constant-time comparison of the winning
// candidate) — only the path used to FIND the candidate changed. The fold
// (lookupByTokenHash's first step) still runs, and still runs BEFORE the
// lockdown check, exactly as before: a lockdown event that arrived since this
// Registry last folded must be visible to checkLockdown by the time it is
// consulted, whether or not the token being authenticated turns out to
// matter (it does not, while lockdown is active — lockdownInfo overrides).
func (r *Registry) Authenticate(ctx context.Context, token string) (*ActorInfo, error) {
	hash := sha256.Sum256([]byte(token))
	info, err := r.lookupByTokenHash(ctx, hash[:])
	if err != nil {
		return nil, err
	}
	if lockdownInfo, active := r.checkLockdown(hash[:]); active {
		if lockdownInfo != nil {
			return lockdownInfo, nil
		}
		return nil, ErrLockdown
	}
	if info == nil {
		return nil, ErrActorUnknown
	}
	if info.Disabled {
		return nil, ErrActorDisabled
	}
	return info, nil
}

// lookupByTokenHash resolves hash to an actor via tokenIndex in O(1), after
// folding any events newer than the watermark (bounded by events-since-last-
// fold, not by actor count — the same bound all()/lookup() already had).
// Returns (nil, nil) when no currently-registered actor holds this token
// hash — the same "no match" outcome the old linear scan produced, just
// reached without visiting every actor.
//
// The matched candidate is still re-checked with subtle.ConstantTimeCompare
// (LLD-12 §2: "не деградировать безопасность ради скорости") rather than
// trusted on map-key equality alone: map lookup used to find the CANDIDATE is
// not a security boundary here (Go's map hashing is per-process-randomized
// and the value compared is already a SHA-256 digest, not the secret token
// itself), but the decisive accept/reject comparison stays constant-time on
// principle, matching exactly what the previous per-actor scan did for its
// winning candidate.
func (r *Registry) lookupByTokenHash(ctx context.Context, hash []byte) (*ActorInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.foldLocked(ctx); err != nil {
		return nil, err
	}
	id, ok := r.tokenIndex[string(hash)]
	if !ok {
		return nil, nil
	}
	info, ok := r.cache[id]
	if !ok || info.TokenHash == nil || subtle.ConstantTimeCompare(info.TokenHash, hash) != 1 {
		return nil, nil
	}
	cp := *info
	return &cp, nil
}

// all returns the directory, folding only events newer than the watermark.
func (r *Registry) all(ctx context.Context) (map[string]*ActorInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.foldLocked(ctx); err != nil {
		return nil, err
	}
	// snapshot copy: callers iterate without holding the registry lock
	out := make(map[string]*ActorInfo, len(r.cache))
	for k, v := range r.cache {
		cp := *v
		out[k] = &cp
	}
	return out, nil
}

// foldLocked folds every event newer than the watermark into r.cache and
// r.tokenIndex. Callers must hold r.mu. Split out of all() so
// lookupByTokenHash can fold+index-lookup without paying for a full
// map-of-every-actor snapshot copy on every Authenticate call (LLD-12 §2).
func (r *Registry) foldLocked(ctx context.Context) error {
	events, err := r.store.Since(ctx, r.watermark)
	if err != nil {
		return err
	}
	for _, e := range events {
		r.watermark = e.Seq
		// node.lockdown/node.lockdown_lifted carry no Subject.ActorID — fold
		// them before the actor-subject skip below would otherwise drop them.
		if r.foldLockdown(e) {
			continue
		}
		// invite.* carry no Subject.ActorID either — fold before the skip.
		if r.foldInvite(e) {
			continue
		}
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
				// A re-issued token (EnsureActor re-registration,
				// LoginWithPassword, key rotation carrying a fresh token)
				// retires the OLD hash's index entry — otherwise a stale
				// entry would keep resolving a revoked/replaced token to
				// this actor forever (tokenIndex would silently diverge
				// from "the token that currently authenticates this actor").
				if info.TokenHash != nil {
					delete(r.tokenIndex, string(info.TokenHash))
				}
				info.TokenHash = p.TokenHash
				r.tokenIndex[string(p.TokenHash)] = id
			}
			if p.Meta != nil {
				info.Meta = p.Meta
			}
		case eventstore.ActorDisabled:
			if info, ok := r.cache[id]; ok {
				info.Disabled = true
			}
		case eventstore.ActorPasswordSet:
			var p passwordPayload
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				continue
			}
			info := r.cache[id]
			if info == nil {
				info = &ActorInfo{ID: id}
				r.cache[id] = info
			}
			info.PasswordHash = p.BcryptHash
		}
	}
	return nil
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
