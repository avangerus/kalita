package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Change pipeline (the heart of the platform, HLD §3.5): the system definition
// changes ONLY as proposal → validate → human signature → atomic apply. The
// full pack sources live in the journal, so a node replays its definitions
// from genesis — the pack directory is only the seed.

type ProposalStatus string

const (
	ProposalPending  ProposalStatus = "pending"
	ProposalApproved ProposalStatus = "approved"
	ProposalRejected ProposalStatus = "rejected"
	ProposalApplied  ProposalStatus = "applied"
)

type Proposal struct {
	ID             string            `json:"id"`
	Description    string            `json:"description"`
	Author         eventstore.Actor  `json:"author"`
	BaseDefVersion uint64            `json:"base_def_version"`
	Files          map[string]string `json:"files"`
	Plan           []string          `json:"plan"`
	Status         ProposalStatus    `json:"status"`
	Reason         string            `json:"reason,omitempty"`
}

type proposalPayload struct {
	Description    string            `json:"description,omitempty"`
	BaseDefVersion uint64            `json:"base_def_version,omitempty"`
	Files          map[string]string `json:"files,omitempty"`
	Plan           []string          `json:"plan,omitempty"`
	Reason         string            `json:"reason,omitempty"`
	ProposalID     string            `json:"proposal_id,omitempty"`
}

const definitionDomain = "kalita-definition-v1"

// DefinitionMessage is what the approver signs.
func DefinitionMessage(proposalID, decision string) []byte {
	return []byte(definitionDomain + "|" + proposalID + "|" + decision)
}

// ProposeChange validates new pack sources against the running definition and
// parks them for a human signature. Compile errors are returned as data (the
// agent's self-correction loop), not as a failure.
func (e *Engine) ProposeChange(ctx context.Context, actor eventstore.Actor, files map[string]string, baseDefVersion uint64, description string, basis *eventstore.Basis) (*Proposal, []*dsl.Error, error) {
	if basis == nil {
		return nil, nil, &Err{Code: CodeBasisRequired, Message: "proposal without basis",
			FixHint: "reference the task or human instruction this change implements"}
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if baseDefVersion != e.defVersion {
		return nil, nil, &Err{Code: CodeStaleBase,
			Message: fmt.Sprintf("proposal is based on def_version %d, node is at %d", baseDefVersion, e.defVersion),
			FixHint: "re-read the system (describe_system) and rebase your change"}
	}
	next, errs := dsl.Compile(files)
	if len(errs) > 0 {
		return nil, errs, nil
	}
	if err := validateAdditive(e.model, next); err != nil {
		return nil, nil, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, nil, err
	}
	p := &Proposal{
		ID: id.String(), Description: description, Author: actor,
		BaseDefVersion: baseDefVersion, Files: files,
		Plan: diffPlan(e.model, next), Status: ProposalPending,
	}
	payload, _ := json.Marshal(proposalPayload{
		Description: description, BaseDefVersion: baseDefVersion, Files: files, Plan: p.Plan,
	})
	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:      actor,
		Kind:       eventstore.DefinitionProposed,
		Subject:    eventstore.Subject{ProposalID: p.ID},
		Payload:    payload,
		Basis:      basis,
		DefVersion: e.defVersion,
	}); err != nil {
		return nil, nil, err
	}
	e.proposals[p.ID] = p
	return p, nil, nil
}

// DecideProposal applies or rejects a pending definition change. Only a human
// with the approver role decides; with a verifier wired, a valid signature
// over DefinitionMessage is mandatory.
func (e *Engine) DecideProposal(ctx context.Context, actor eventstore.Actor, proposalID string, grant bool, signature []byte, basis *eventstore.Basis) (*Proposal, error) {
	if basis == nil {
		return nil, &Err{Code: CodeBasisRequired, Message: "decision without basis"}
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	p, ok := e.proposals[proposalID]
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: "proposal " + proposalID + " not found"}
	}
	if p.Status != ProposalPending {
		return nil, &Err{Code: CodeConflict, Message: "proposal already " + string(p.Status)}
	}
	if actor.Type != eventstore.ActorHuman || actor.Role != e.defApprover {
		return nil, &Err{Code: CodePermissionDenied,
			Message: fmt.Sprintf("definitions are approved by a human with role %s", e.defApprover),
			Rule:    "definition approver"}
	}
	if p.BaseDefVersion != e.defVersion {
		return nil, &Err{Code: CodeStaleBase,
			Message: "the definition moved while the proposal was pending",
			FixHint: "reject and ask the author to rebase"}
	}
	decision := "granted"
	kind := eventstore.DefinitionApproved
	if !grant {
		decision = "rejected"
		kind = eventstore.DefinitionRejected
	}
	if e.verify != nil {
		if err := e.verify(ctx, actor.ID, DefinitionMessage(proposalID, decision), signature); err != nil {
			return nil, &Err{Code: CodePermissionDenied,
				Message: "definition decision requires a valid signature: " + err.Error(),
				Rule:    "signatures are mandatory on definition decisions"}
		}
	}
	payload, _ := json.Marshal(proposalPayload{ProposalID: proposalID})
	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor: actor, Kind: kind,
		Subject:    eventstore.Subject{ProposalID: proposalID},
		Payload:    payload,
		Basis:      basis,
		DefVersion: e.defVersion,
		Signature:  signature,
	}); err != nil {
		return nil, err
	}
	if !grant {
		p.Status = ProposalRejected
		return p, nil
	}
	p.Status = ProposalApproved

	next, errs := dsl.Compile(p.Files)
	if len(errs) > 0 { // cannot happen: validated at propose; defend anyway
		return nil, &Err{Code: CodeValidation, Message: "proposal no longer compiles: " + errs[0].Error()}
	}
	applied, _ := json.Marshal(proposalPayload{ProposalID: proposalID})
	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor: actor, Kind: eventstore.DefinitionApplied,
		Subject:    eventstore.Subject{ProposalID: proposalID},
		Payload:    applied,
		Basis:      &eventstore.Basis{Type: "approval", ID: proposalID},
		DefVersion: e.defVersion,
	}); err != nil {
		return nil, err
	}
	e.model = next
	e.defVersion++
	p.Status = ProposalApplied
	return p, nil
}

// PendingProposals lists open definition changes (the human inbox).
func (e *Engine) PendingProposals() []*Proposal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var out []*Proposal
	for _, p := range e.proposals {
		if p.Status == ProposalPending {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// GetProposal returns a proposal by id.
func (e *Engine) GetProposal(id string) (*Proposal, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	p, ok := e.proposals[id]
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: "proposal " + id + " not found"}
	}
	return p, nil
}

// validateAdditive enforces DSL-SPEC §9: nothing existing disappears or
// changes type; enum values only append.
func validateAdditive(old, next *dsl.Model) *Err {
	for name, oldEnt := range old.Entities {
		newEnt, ok := next.Entities[name]
		if !ok {
			return invalid("", "entity "+name+" removed; only additive changes are allowed in v0",
				"keep the entity; destructive migrations require the manual procedure")
		}
		newFields := map[string]dsl.TypeRef{}
		for _, f := range newEnt.Fields {
			newFields[f.Name] = f.Type
		}
		for _, f := range oldEnt.Fields {
			nf, ok := newFields[f.Name]
			if !ok {
				return invalid(f.Name, fmt.Sprintf("field %s.%s removed; only additive changes are allowed in v0", name, f.Name),
					"keep the field; removals require the manual procedure")
			}
			if nf.Kind != f.Type.Kind || nf.Scalar != f.Type.Scalar || nf.RefTarget != f.Type.RefTarget {
				return invalid(f.Name, fmt.Sprintf("field %s.%s changed type; not allowed in v0", name, f.Name),
					"add a new field instead of changing the type")
			}
			if nf.Kind == dsl.TyEnum {
				for i, v := range f.Type.EnumValues {
					if i >= len(nf.EnumValues) || nf.EnumValues[i] != v {
						return invalid(f.Name, "enum values may only be appended", "keep existing enum values in order, add new ones at the end")
					}
				}
			}
		}
	}
	return nil
}

// diffPlan is the human-readable migration plan shown at signing time.
func diffPlan(old, next *dsl.Model) []string {
	var plan []string
	for _, name := range next.Order {
		oldEnt, existed := old.Entities[name]
		if !existed {
			plan = append(plan, "add entity "+name)
			continue
		}
		oldFields := map[string]bool{}
		for _, f := range oldEnt.Fields {
			oldFields[f.Name] = true
		}
		for _, f := range next.Entities[name].Fields {
			if !oldFields[f.Name] {
				plan = append(plan, "add field "+name+"."+f.Name)
			}
		}
	}
	for role := range next.Roles {
		if _, ok := old.Roles[role]; !ok {
			plan = append(plan, "add role "+role)
		}
	}
	for entity := range next.Workflows {
		if _, ok := old.Workflows[entity]; !ok {
			plan = append(plan, "add workflow for "+entity)
		}
	}
	if len(plan) == 0 {
		plan = []string{"no structural changes (permissions/automation/ui only)"}
	}
	return plan
}
