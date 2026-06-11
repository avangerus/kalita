package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Invites: self-registration for external users (the customer-portal entry,
// PORTAL-VISION §1). A human issues a one-time code bound to a role and
// optionally to a record ("this customer"); the external user redeems it and
// becomes a regular actor — same tokens, same permissions, same journal.

type Invite struct {
	CodeHash  []byte           `json:"code_hash"`
	Role      string           `json:"role"`
	Entity    string           `json:"entity,omitempty"`    // bound record (optional)
	RecordID  string           `json:"record_id,omitempty"`
	BindField string           `json:"bind_field,omitempty"` // field set to the new actor id
	CreatedBy eventstore.Actor `json:"created_by"`
	Redeemed  bool             `json:"redeemed"`
	ActorID   string           `json:"actor_id,omitempty"` // who redeemed
}

var (
	ErrInviteUnknown = errors.New("identity: invite code does not exist")
	ErrInviteUsed    = errors.New("identity: invite already redeemed")
)

// CreateInvite issues a one-time registration code. Humans only.
func (r *Registry) CreateInvite(ctx context.Context, inviter eventstore.Actor, role, entity, recordID, bindField string, basis *eventstore.Basis) (string, error) {
	if inviter.Type != eventstore.ActorHuman {
		return "", fmt.Errorf("identity: only humans issue invites")
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	code := hex.EncodeToString(raw)
	hash := sha256.Sum256([]byte(code))
	if entity != "" && bindField == "" {
		bindField = "user" // convention: ref[core.User]-ish field linking record to actor
	}
	payload, _ := json.Marshal(Invite{
		CodeHash: hash[:], Role: role, Entity: entity, RecordID: recordID,
		BindField: bindField, CreatedBy: inviter,
	})
	if _, err := r.store.Append(ctx, eventstore.AppendInput{
		Actor:   inviter,
		Kind:    eventstore.InviteCreated,
		Payload: payload,
		Basis:   basis,
	}); err != nil {
		return "", err
	}
	return code, nil
}

// Redeem turns a valid invite into a registered actor and returns its bearer
// token (shown once) plus the invite's record binding for the API layer.
func (r *Registry) Redeem(ctx context.Context, actorID, code string) (string, *Invite, error) {
	if actorID == "" {
		return "", nil, fmt.Errorf("identity: actor id is required")
	}
	hash := sha256.Sum256([]byte(code))
	inv, err := r.findInvite(ctx, hash[:])
	if err != nil {
		return "", nil, err
	}
	if inv.Redeemed {
		return "", nil, ErrInviteUsed
	}
	token, err := r.RegisterWithToken(ctx, inv.CreatedBy, actorID, eventstore.ActorHuman, inv.Role, nil,
		&ActorMeta{Description: "self-registered via invite"},
		&eventstore.Basis{Type: "human", ID: inv.CreatedBy.ID})
	if err != nil {
		return "", nil, err
	}
	redeemed, _ := json.Marshal(Invite{CodeHash: inv.CodeHash, ActorID: actorID, Redeemed: true})
	if _, err := r.store.Append(ctx, eventstore.AppendInput{
		Actor:   eventstore.Actor{Type: eventstore.ActorHuman, ID: actorID, Role: inv.Role},
		Kind:    eventstore.InviteRedeemed,
		Subject: eventstore.Subject{ActorID: actorID},
		Payload: redeemed,
		Basis:   &eventstore.Basis{Type: "human", ID: inv.CreatedBy.ID},
	}); err != nil {
		return "", nil, err
	}
	return token, inv, nil
}

// findInvite folds invite.* events (reusing the registry watermark machinery
// would complicate the actor fold; invite volume is tiny — scan is fine, and
// it runs only on registration attempts, never on the hot path).
func (r *Registry) findInvite(ctx context.Context, hash []byte) (*Invite, error) {
	events, err := r.store.All(ctx)
	if err != nil {
		return nil, err
	}
	var found *Invite
	for _, e := range events {
		if e.Kind != eventstore.InviteCreated && e.Kind != eventstore.InviteRedeemed {
			continue
		}
		var inv Invite
		if json.Unmarshal(e.Payload, &inv) != nil {
			continue
		}
		if hex.EncodeToString(inv.CodeHash) != hex.EncodeToString(hash) {
			continue
		}
		if e.Kind == eventstore.InviteCreated {
			cp := inv
			found = &cp
		} else if found != nil {
			found.Redeemed = true
			found.ActorID = inv.ActorID
		}
	}
	if found == nil {
		return nil, ErrInviteUnknown
	}
	return found, nil
}
