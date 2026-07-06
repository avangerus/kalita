package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Shadow mode (LLD-09 §1). Shadow is a runtime overlay on a ROLE, not a new
// execution path or a second write branch (I-2): the Permission Engine is the
// ONLY interception point. A shadow actor's create/update/act/emit is fully
// permission-checked and validated exactly as if it were going to be applied
// — then, at the point it would be journaled, it is redirected into a
// MutationProposal ("what I would have done", the full requested payload)
// instead. Approving the proposal applies that SAME validated intent through
// the ordinary path (createCore/updateCore/actCore/emitCore with
// checkShadow=false) — so any HITL gate the mutation itself carries (e.g. an
// act's `requires approval(Role)`) still applies at that point: shadow defers
// a decision, it never collapses one into another (test 2).
//
// Reads (Get/Query/EmittedEvents/…) are never touched — shadow only
// intercepts the four mutation entry points, each at the moment its own
// Permission Engine check has already granted it.

// MutationKind is the shape of mutation a MutationProposal carries.
type MutationKind string

const (
	MutationCreate MutationKind = "create"
	MutationUpdate MutationKind = "update"
	MutationAct    MutationKind = "act"
	MutationEmit   MutationKind = "emit"
)

// MutationProposal is a shadow-redirected mutation awaiting a human decision.
// It reuses ProposalStatus (proposals.go) so both proposal kinds — definition
// changes and record mutations — speak the same pending/approved/rejected/
// applied vocabulary.
type MutationProposal struct {
	ID       string           `json:"id"`
	Kind     MutationKind     `json:"kind"`
	Role     string           `json:"role"`
	Actor    eventstore.Actor `json:"actor"`
	Entity   string           `json:"entity,omitempty"`
	RecordID string           `json:"record_id,omitempty"`
	Action   string           `json:"action,omitempty"` // kind=act
	Event    string           `json:"event,omitempty"`  // kind=emit: canonical event name
	Values   map[string]any   `json:"values,omitempty"` // kind=create/update: fields; kind=emit: payload
	IdemKey  string           `json:"idempotency_key,omitempty"`
	Status   ProposalStatus   `json:"status"`
	Reason   string           `json:"reason,omitempty"`
}

type shadowRolePayload struct {
	Role string `json:"role"`
}

type mutationProposalPayload struct {
	Kind       MutationKind   `json:"kind,omitempty"`
	Entity     string         `json:"entity,omitempty"`
	RecordID   string         `json:"record_id,omitempty"`
	Action     string         `json:"action,omitempty"`
	Event      string         `json:"event,omitempty"`
	Values     map[string]any `json:"values,omitempty"`
	IdemKey    string         `json:"idempotency_key,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	ProposalID string         `json:"proposal_id,omitempty"` // mutation.approved/rejected only
}

// applyShadowRoleEvent folds role.shadow_enabled/role.shadow_disabled.
func (e *Engine) applyShadowRoleEvent(ev *eventstore.Event, enable bool) {
	var p shadowRolePayload
	if json.Unmarshal(ev.Payload, &p) != nil || p.Role == "" {
		return
	}
	if enable {
		e.shadowRoles[p.Role] = true
	} else {
		delete(e.shadowRoles, p.Role)
	}
}

// applyMutationProposed folds mutation.proposed into the projection.
func (e *Engine) applyMutationProposed(ev *eventstore.Event) {
	var p mutationProposalPayload
	if json.Unmarshal(ev.Payload, &p) != nil {
		return
	}
	id := ev.Subject.ProposalID
	if id == "" {
		return
	}
	e.mutationProposals[id] = &MutationProposal{
		ID: id, Kind: p.Kind, Role: ev.Actor.Role, Actor: ev.Actor,
		Entity: p.Entity, RecordID: p.RecordID, Action: p.Action, Event: p.Event,
		Values: p.Values, IdemKey: p.IdemKey, Status: ProposalPending,
	}
}

// applyMutationDecided folds mutation.approved/mutation.rejected.
func (e *Engine) applyMutationDecided(ev *eventstore.Event, status ProposalStatus) {
	var p mutationProposalPayload
	if json.Unmarshal(ev.Payload, &p) != nil {
		return
	}
	if mp, ok := e.mutationProposals[p.ProposalID]; ok {
		mp.Status = status
		mp.Reason = p.Reason
	}
}

// isShadow reports whether role is currently in the shadow set. Caller holds
// e.mu (read or write) — this is a plain map read, the Permission Engine's
// own choke point for the redirect, checked by Create/Update/Act/EmitEvent
// immediately after their own permission grant, before the real mutation
// would be journaled.
func (e *Engine) isShadow(role string) bool {
	return e.shadowRoles[role]
}

// IsShadowRole is the read-only, lock-safe accessor for callers outside the
// package (CLI, tests) that just need to know current shadow membership.
func (e *Engine) IsShadowRole(role string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.shadowRoles[role]
}

// ShadowRoles lists the roles currently in shadow mode, sorted.
func (e *Engine) ShadowRoles() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]string, 0, len(e.shadowRoles))
	for r := range e.shadowRoles {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// EnableShadow puts role into shadow mode: every subsequent create/update/
// act/emit by an actor holding role is redirected into a MutationProposal
// instead of being applied (LLD-09 §1). Reversible with DisableShadow, both a
// single journaled event. Only a human with the node's definition-approver
// role may toggle it — this is a node-wide policy change over the Permission
// Engine's choke point, the same authority level as approving a pack change.
func (e *Engine) EnableShadow(ctx context.Context, actor eventstore.Actor, role string, basis *eventstore.Basis) error {
	return e.setShadow(ctx, actor, role, true, basis)
}

// DisableShadow turns shadow mode off for role: the very next mutation from
// that role applies directly again (LLD-09 §1 — "выключение shadow = одно
// событие").
func (e *Engine) DisableShadow(ctx context.Context, actor eventstore.Actor, role string, basis *eventstore.Basis) error {
	return e.setShadow(ctx, actor, role, false, basis)
}

func (e *Engine) setShadow(ctx context.Context, actor eventstore.Actor, role string, enable bool, basis *eventstore.Basis) error {
	if basis == nil {
		return &Err{Code: CodeBasisRequired, Message: "shadow toggle without basis"}
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, ok := e.model.Roles[role]; !ok {
		return &Err{Code: CodeNotFound, Message: "unknown role " + role,
			FixHint: "call describe_system for the declared roles"}
	}
	if actor.Type != eventstore.ActorHuman || actor.Role != e.defApprover {
		return &Err{Code: CodePermissionDenied,
			Message: fmt.Sprintf("shadow mode is toggled by a human with role %s", e.defApprover),
			Rule:    "shadow toggle approver"}
	}
	kind := eventstore.RoleShadowDisabled
	if enable {
		kind = eventstore.RoleShadowEnabled
	}
	payload, _ := json.Marshal(shadowRolePayload{Role: role})
	ev, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:      actor,
		Kind:       kind,
		Payload:    payload,
		Basis:      basis,
		DefVersion: e.defVersion,
		SchemaV:    eventstore.CurrentSchemaV,
	})
	if err != nil {
		return err
	}
	e.applyShadowRoleEvent(ev, enable)
	return nil
}

// proposeMutation redirects an already-permission-granted mutation into a
// MutationProposal (LLD-09 §1). Called from inside Create/Update/Act/
// EmitEvent, under e.mu, at the exact point the real record.created/updated/
// action/event.emitted would otherwise be journaled — values is the RAW
// payload the actor submitted (pre-serial-assignment for create, the raw
// partial patch for update, nil for act, the raw emit payload for emit), so
// re-applying it later (DecideMutationProposal) re-validates and re-computes
// against whatever the state is BY THEN, rather than replaying a
// possibly-stale precomputed result (e.g. a serial number that another
// mutation already consumed in the meantime).
func (e *Engine) proposeMutation(ctx context.Context, actor eventstore.Actor, kind MutationKind, entity, recordID, action, event string, values map[string]any, basis *eventstore.Basis, idemKey string) (*MutationProposal, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(mutationProposalPayload{
		Kind: kind, Entity: entity, RecordID: recordID, Action: action, Event: event,
		Values: values, IdemKey: idemKey,
	})
	ev, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:          actor,
		Kind:           eventstore.MutationProposed,
		Subject:        eventstore.Subject{ProposalID: id.String(), Entity: entity, RecordID: recordID},
		Payload:        payload,
		Basis:          basis,
		DefVersion:     e.defVersion,
		SchemaV:        eventstore.CurrentSchemaV,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		return nil, err
	}
	// idempotent replay: Append returned the ORIGINAL event for a repeated key
	// — the proposal it already created, not a fresh one.
	if existing, ok := e.mutationProposals[ev.Subject.ProposalID]; ok {
		return existing, nil
	}
	mp := &MutationProposal{
		ID: ev.Subject.ProposalID, Kind: kind, Role: actor.Role, Actor: actor,
		Entity: entity, RecordID: recordID, Action: action, Event: event,
		Values: values, IdemKey: idemKey, Status: ProposalPending,
	}
	e.mutationProposals[mp.ID] = mp
	return mp, nil
}

// GetMutationProposal returns a shadow proposal by id.
func (e *Engine) GetMutationProposal(id string) (*MutationProposal, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	mp, ok := e.mutationProposals[id]
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: "mutation proposal " + id + " not found"}
	}
	return mp, nil
}

// PendingMutationProposals lists open shadow proposals (the "what the agent
// would have done" inbox — LLD-09 §1's shadow lane, before its own UI lands
// in PR-4: the core exposes this list, no new query surface).
func (e *Engine) PendingMutationProposals() []*MutationProposal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var out []*MutationProposal
	for _, mp := range e.mutationProposals {
		if mp.Status == ProposalPending {
			out = append(out, mp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// DecideMutationProposal grants or rejects a pending shadow proposal. Grant
// applies the SAME validated mutation through the ordinary path — the
// permission-free "…Core" functions with checkShadow=false, called directly
// while e.mu is already held (mirrors DecideProposal's own pattern: it
// applies via the low-level store.Append + projection update, never by
// recursively calling the public, lock-taking ProposeChange/Create/Update/
// Act/EmitEvent). Applying uses a DERIVED idempotency key
// ("<idemKey>|shadow-applied:<proposalID>") — reusing the ORIGINAL key would
// hit Store's idem-dedup and return the mutation.proposed event itself
// instead of ever journaling the real mutation (Store.Append's idempotency is
// keyed on the string alone, not (key, kind)).
func (e *Engine) DecideMutationProposal(ctx context.Context, actor eventstore.Actor, proposalID string, grant bool, basis *eventstore.Basis) (*MutationProposal, error) {
	if basis == nil {
		return nil, &Err{Code: CodeBasisRequired, Message: "decision without basis"}
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	mp, ok := e.mutationProposals[proposalID]
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: "mutation proposal " + proposalID + " not found"}
	}
	if mp.Status != ProposalPending {
		return nil, &Err{Code: CodeConflict, Message: "mutation proposal already " + string(mp.Status)}
	}
	if actor.Type != eventstore.ActorHuman || actor.Role != e.defApprover {
		return nil, &Err{Code: CodePermissionDenied,
			Message: fmt.Sprintf("shadow mutation proposals are decided by a human with role %s", e.defApprover),
			Rule:    "shadow proposal approver"}
	}

	kind := eventstore.MutationApproved
	if !grant {
		kind = eventstore.MutationRejected
	}
	payload, _ := json.Marshal(mutationProposalPayload{ProposalID: proposalID})
	ev, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:      actor,
		Kind:       kind,
		Subject:    eventstore.Subject{ProposalID: proposalID},
		Payload:    payload,
		Basis:      basis,
		DefVersion: e.defVersion,
		SchemaV:    eventstore.CurrentSchemaV,
	})
	if err != nil {
		return nil, err
	}
	if !grant {
		e.applyMutationDecided(ev, ProposalRejected)
		return mp, nil
	}

	applyBasis := &eventstore.Basis{Type: "approval", ID: proposalID}
	appliedIdemKey := ""
	if mp.IdemKey != "" {
		appliedIdemKey = mp.IdemKey + "|shadow-applied:" + proposalID
	}
	var applyErr error
	switch mp.Kind {
	case MutationCreate:
		_, applyErr = e.createCore(ctx, mp.Actor, mp.Entity, mp.Values, applyBasis, appliedIdemKey, false)
	case MutationUpdate:
		_, applyErr = e.updateCore(ctx, mp.Actor, mp.Entity, mp.RecordID, mp.Values, applyBasis, appliedIdemKey, false)
	case MutationAct:
		_, applyErr = e.actCore(ctx, mp.Actor, mp.Entity, mp.RecordID, mp.Action, applyBasis, appliedIdemKey, false)
	case MutationEmit:
		_, applyErr = e.emitCore(ctx, mp.Actor, mp.Event, mp.Values, appliedIdemKey, false)
	default:
		applyErr = &Err{Code: CodeValidation, Message: "unknown mutation proposal kind " + string(mp.Kind)}
	}
	if applyErr != nil {
		// mutation.approved is already journaled (the human's decision stands
		// as a fact — audit trail), but the projection is NOT advanced to
		// Applied: the caller sees the real error and the underlying record
		// state is exactly as if the apply had never been attempted. This
		// mirrors DecideProposal's own defensive "cannot happen" branch.
		return nil, applyErr
	}
	e.applyMutationDecided(ev, ProposalApplied)
	return mp, nil
}
