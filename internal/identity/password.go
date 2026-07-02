package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Password as an actor credential (LLD-05 §3, PK-4; HLD-V1 §H7.4): a UX
// wrapper for a human who will never carry a bearer token around — NOT a new
// identity model, NOT consumer-scale self-signup (that stays an external IdP,
// N7). actor.password_set journals only the bcrypt hash (I-4: the node never
// persists the raw password); /api/login exchanges login+password for an
// ordinary bearer token via the SAME mechanism actor.key_rotated already
// uses. Offline-provable (I-5): the event is a plain journal entry, hashed
// and chained exactly like every other event — `kalita verify` needs no
// special case for it.

// passwordPayload is the payload of actor.password_set.
type passwordPayload struct {
	BcryptHash []byte `json:"bcrypt_hash"`
}

var (
	ErrNoPassword  = errors.New("identity: actor has no password set")
	ErrBadPassword = errors.New("identity: password does not match")
)

const minPasswordLen = 8

// SetPassword journals actor.password_set for an existing actor. Humans only
// set passwords (mirrors Register/Disable's human-only rule) — this is an
// admin/self-service action over a governed actor, not open registration.
func (r *Registry) SetPassword(ctx context.Context, setter eventstore.Actor, id, password string, basis *eventstore.Basis) error {
	if setter.Type != eventstore.ActorHuman {
		return fmt.Errorf("identity: only humans set passwords")
	}
	if _, err := r.Get(ctx, id); err != nil {
		return err
	}
	if len(password) < minPasswordLen {
		return fmt.Errorf("identity: password must be at least %d characters", minPasswordLen)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(passwordPayload{BcryptHash: hash})
	if err != nil {
		return err
	}
	_, err = r.store.Append(ctx, eventstore.AppendInput{
		Actor: setter,
		Kind:  eventstore.ActorPasswordSet,
		// New kind minted in the v1 envelope era carries CurrentSchemaV — the
		// LLD-01 convention for every new event (event.go: "events introduced
		// by LLD-01 are written with SchemaV 1"). The legacy actor.* kinds read
		// SchemaV 0 only because they predate the field; a brand-new actor.*
		// kind must NOT copy that — it is not a pre-schema_v event.
		SchemaV: eventstore.CurrentSchemaV,
		Subject: eventstore.Subject{ActorID: id},
		Payload: payload,
		Basis:   basis,
	})
	return err
}

// LoginWithPassword verifies id/password against the journaled bcrypt hash
// and, on success, issues a FRESH bearer token via an actor.key_rotated event
// (the exact mechanism EnsureActor/RotateKey already use) — password login is
// an alternate on-ramp to the same token, not a parallel credential store.
func (r *Registry) LoginWithPassword(ctx context.Context, id, password string) (string, error) {
	info, err := r.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if info.Disabled {
		return "", ErrActorDisabled
	}
	if len(info.PasswordHash) == 0 {
		return "", ErrNoPassword
	}
	if bcrypt.CompareHashAndPassword(info.PasswordHash, []byte(password)) != nil {
		return "", ErrBadPassword
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	payload, err := json.Marshal(actorPayload{
		ActorType: info.Type, Role: info.Role, PublicKey: info.PublicKey,
		TokenHash: hash[:], Meta: info.Meta,
	})
	if err != nil {
		return "", err
	}
	self := eventstore.Actor{Type: eventstore.ActorHuman, ID: id, Role: info.Role}
	if _, err := r.store.Append(ctx, eventstore.AppendInput{
		Actor:   self,
		Kind:    eventstore.ActorKeyRotated,
		Subject: eventstore.Subject{ActorID: id},
		Payload: payload,
		Basis:   &eventstore.Basis{Type: "human", ID: id},
	}); err != nil {
		return "", err
	}
	return token, nil
}
