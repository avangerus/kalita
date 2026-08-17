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
// Atomic (Cycle-A4): the invite lookup, the actor registration and the
// invite.redeemed append all happen under r.mu, so two concurrent redemptions
// of the same code can no longer both register an actor — the second sees
// Redeemed=true and fails with ErrInviteUsed.
func (r *Registry) Redeem(ctx context.Context, actorID, code string) (string, *Invite, error) {
	if actorID == "" {
		return "", nil, fmt.Errorf("identity: actor id is required")
	}
	hash := sha256.Sum256([]byte(code))
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.foldLocked(ctx); err != nil {
		return "", nil, err
	}
	inv, ok := r.invites[hex.EncodeToString(hash[:])]
	if !ok {
		return "", nil, ErrInviteUnknown
	}
	if inv.Redeemed {
		return "", nil, ErrInviteUsed
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, err
	}
	token := hex.EncodeToString(raw)
	tokenHash := sha256.Sum256([]byte(token))
	if _, err := r.registerLocked(ctx, inv.CreatedBy, actorID, eventstore.ActorHuman, inv.Role, nil,
		tokenHash[:], &ActorMeta{Description: "self-registered via invite"},
		&eventstore.Basis{Type: "human", ID: inv.CreatedBy.ID}); err != nil {
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

// foldInvite folds one invite.* event into the r.invites projection. Callers
// hold r.mu (foldLocked). Returns true when the event was consumed here.
func (r *Registry) foldInvite(e *eventstore.Event) bool {
	if e.Kind != eventstore.InviteCreated && e.Kind != eventstore.InviteRedeemed {
		return false
	}
	var inv Invite
	if json.Unmarshal(e.Payload, &inv) != nil {
		return true // unparseable invite event: consume (never matches a lookup)
	}
	key := hex.EncodeToString(inv.CodeHash)
	if e.Kind == eventstore.InviteCreated {
		cp := inv
		r.invites[key] = &cp
	} else if cur, ok := r.invites[key]; ok {
		cur.Redeemed = true
		cur.ActorID = inv.ActorID
	}
	return true
}

// findInvite resolves a code hash to its Invite via the O(1) projection
// (LLD-10 PR-2, Cycle-A4). The projection is folded in lockstep with the actor
// cache under the same mu/watermark; results are identical to the former full
// journal scan (invite.created seeds, invite.redeemed marks on the same entry).
func (r *Registry) findInvite(ctx context.Context, hash []byte) (*Invite, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.foldLocked(ctx); err != nil {
		return nil, err
	}
	if inv, ok := r.invites[hex.EncodeToString(hash)]; ok {
		cp := *inv
		return &cp, nil
	}
	return nil, ErrInviteUnknown
}
