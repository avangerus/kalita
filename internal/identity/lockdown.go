package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Lockdown (LLD-10 §3): the node's emergency brake. `node.lockdown` flips a
// curtain in front of Authenticate — while it stands, every bearer token is
// rejected EXCEPT one freshly-minted emergency admin token, shown to the
// operator once and never re-derivable from the journal (only its hash is
// written, I-4).
//
// This is deliberately NOT a mass revoke: previously issued tokens are not
// invalidated by this event, they are simply refused entry while the curtain
// is up. `node.lockdown_lifted` removes the curtain and those tokens are
// checkable again — lockdown stays a single, reversible command. A real mass
// revoke (e.g. after a confirmed key compromise) is a separate, deliberate
// operation (disable each actor), not a side effect of this one.
type lockdownPayload struct {
	EmergencyAdminTokenHash []byte    `json:"emergency_admin_token_hash"`
	Role                    string    `json:"role,omitempty"`
	At                      time.Time `json:"at"`
}

type lockdownLiftedPayload struct {
	At time.Time `json:"at"`
}

// ErrLockdown is returned by Authenticate for any non-emergency token while
// the node is in lockdown.
var ErrLockdown = fmt.Errorf("identity: node is in lockdown — only the emergency admin token is accepted")

// Lockdown appends node.lockdown and returns the emergency admin token ONCE.
// Only a human can trigger it (same rule as Register/Disable — no agent gets
// to declare its own emergency). role defaults to "Owner" when empty: the
// emergency actor is meant to be a full admin, the one living identity left
// standing while the curtain is up.
func (r *Registry) Lockdown(ctx context.Context, registrar eventstore.Actor, role string, basis *eventstore.Basis) (string, error) {
	if registrar.Type != eventstore.ActorHuman {
		return "", fmt.Errorf("identity: only humans can trigger lockdown")
	}
	if role == "" {
		role = "Owner"
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	payload, err := json.Marshal(lockdownPayload{EmergencyAdminTokenHash: hash[:], Role: role, At: time.Now().UTC()})
	if err != nil {
		return "", err
	}
	if _, err := r.store.Append(ctx, eventstore.AppendInput{
		Actor:   registrar,
		Kind:    eventstore.NodeLockdown,
		Payload: payload,
		Basis:   basis,
		SchemaV: eventstore.CurrentSchemaV,
	}); err != nil {
		return "", err
	}
	return token, nil
}

// LiftLockdown appends node.lockdown_lifted, removing the curtain. Previously
// valid tokens are accepted again — not because they are "restored", but
// because lockdown never revoked them in the first place (see Lockdown docs).
func (r *Registry) LiftLockdown(ctx context.Context, registrar eventstore.Actor, basis *eventstore.Basis) error {
	if registrar.Type != eventstore.ActorHuman {
		return fmt.Errorf("identity: only humans can lift lockdown")
	}
	payload, err := json.Marshal(lockdownLiftedPayload{At: time.Now().UTC()})
	if err != nil {
		return err
	}
	_, err = r.store.Append(ctx, eventstore.AppendInput{
		Actor:   registrar,
		Kind:    eventstore.NodeLockdownLifted,
		Payload: payload,
		Basis:   basis,
		SchemaV: eventstore.CurrentSchemaV,
	})
	return err
}

// foldLockdown applies a node.lockdown/node.lockdown_lifted event to registry
// state. Called from all() while holding r.mu, BEFORE the actor-subject
// dispatch (a lockdown event carries no Subject.ActorID, so it must not fall
// into that branch's early `id == ""` skip). Returns true if e was a lockdown
// event (handled), false otherwise.
func (r *Registry) foldLockdown(e *eventstore.Event) bool {
	switch e.Kind {
	case eventstore.NodeLockdown:
		var p lockdownPayload
		if err := json.Unmarshal(e.Payload, &p); err == nil {
			r.lockdown = true
			r.lockdownHash = p.EmergencyAdminTokenHash
			r.lockdownRole = p.Role
		}
		return true
	case eventstore.NodeLockdownLifted:
		r.lockdown = false
		r.lockdownHash = nil
		r.lockdownRole = ""
		return true
	}
	return false
}

// checkLockdown resolves a bearer token against the emergency admin only,
// while the node is in lockdown. ok reports whether lockdown is active at
// all; the caller (Authenticate) still must reject every other token when ok
// is true and info is nil.
func (r *Registry) checkLockdown(tokenHash []byte) (info *ActorInfo, active bool) {
	r.mu.Lock()
	active = r.lockdown
	hash, role := r.lockdownHash, r.lockdownRole
	r.mu.Unlock()
	if !active {
		return nil, false
	}
	if hash != nil && subtle.ConstantTimeCompare(hash, tokenHash) == 1 {
		return &ActorInfo{ID: "emergency-admin", Type: eventstore.ActorHuman, Role: role}, true
	}
	return nil, true
}
