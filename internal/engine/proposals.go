package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

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
	CreatedAt      time.Time         `json:"created_at"`           // when it was proposed (journal TS)
	DecidedAt      *time.Time        `json:"decided_at,omitempty"` // when the human decided (journal TS)
}

type proposalPayload struct {
	Description    string            `json:"description,omitempty"`
	BaseDefVersion uint64            `json:"base_def_version,omitempty"`
	Files          map[string]string `json:"files,omitempty"`
	Plan           []string          `json:"plan,omitempty"`
	Reason         string            `json:"reason,omitempty"`
	ProposalID     string            `json:"proposal_id,omitempty"`
	// WebAuthn-era additive fields on definition decisions (mirror of the
	// approval.decided human_signature, LLD-02 §1/§5): the assertion the
	// human produced when signing this definition change, journaled so an
	// offline verifier can redo every check.
	HumanSignature json.RawMessage `json:"human_signature,omitempty"`
	RPID           string          `json:"rp_id,omitempty"`
	HeadSeq        uint64          `json:"head_seq,omitempty"`
	// Signature is the legacy raw Ed25519 signature over DefinitionMessage,
	// journaled when a verifier accepted it (pre-LLD-02 path).
	Signature []byte `json:"signature,omitempty"`
}

const definitionDomain = "kalita-definition-v1"

// DefinitionMessage is what the approver signs.
func DefinitionMessage(proposalID, decision string) []byte {
	return []byte(definitionDomain + "|" + proposalID + "|" + decision)
}

// ProposeChange validates new pack sources against the running definition and
// parks them for a human signature. Compile errors are returned as data (the
// agent's self-correction loop), not as a failure.
func (e *Engine) ProposeChange(ctx context.Context, actor eventstore.Actor, files map[string]string, baseDefVersion uint64, description string, basis *eventstore.Basis, idemKey string) (*Proposal, []*dsl.Error, error) {
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
	// Security Pro MAJOR-2: bound the parked-proposal set (journal/storage
	// DoS via ungated propose). Proposing is harmless per-proposal — this
	// caps the OPEN backlog (decided proposals are not counted) AND the
	// total source size of one proposal.
	pending := 0
	for _, p := range e.proposals {
		if p.Status == ProposalPending {
			pending++
		}
	}
	if pending >= 20 {
		return nil, nil, &Err{Code: CodeConflict,
			Message: "too many pending definition proposals (>= 20)",
			FixHint: "decide or reject existing proposals before proposing more"}
	}
	totalBytes := 0
	for _, src := range files {
		totalBytes += len(src)
	}
	const maxProposalBytes = 4 << 20 // 4 MiB of definition sources
	if totalBytes > maxProposalBytes {
		return nil, nil, &Err{Code: CodeValidation,
			Message: fmt.Sprintf("proposal sources too large: %d bytes (max %d)", totalBytes, maxProposalBytes),
			FixHint: "split the change or remove generated bloat"}
	}
	next, errs := dsl.CompileMulti(files)
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
		CreatedAt: e.now(),
	}
	payload, _ := json.Marshal(proposalPayload{
		Description: description, BaseDefVersion: baseDefVersion, Files: files, Plan: p.Plan,
	})
	if ev, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:          actor,
		Kind:           eventstore.DefinitionProposed,
		Subject:        eventstore.Subject{ProposalID: p.ID},
		Payload:        payload,
		Basis:          basis,
		DefVersion:     e.defVersion,
		IdempotencyKey: idemKey,
	}); err != nil {
		return nil, nil, err
	} else if !ev.TS.IsZero() {
		p.CreatedAt = ev.TS // the journal's own timestamp is the truth
	}
	e.proposals[p.ID] = p
	return p, nil, nil
}

// DecideProposalOption customizes a DecideProposal call with the WebAuthn
// era's additive human_signature (LLD-02). Omitting every option reproduces
// the pre-LLD-02 behavior exactly (I-1) — a trailing variadic, so every
// existing call site compiles and behaves unchanged.
type DecideProposalOption func(*decideProposalOpts)

type decideProposalOpts struct {
	humanSig json.RawMessage // pre-verified by the caller — see WithProposalHumanSignature
	rpID     string
	headSeq  uint64
}

// WithProposalHumanSignature attaches a WebAuthn assertion to a definition
// decision. The caller (internal/api, which owns the credential registry and
// canonical reconstruction) MUST have already verified the assertion against
// BuildDefinitionCanonical — DecideProposal only journals it additively on
// the definition decision event.
func WithProposalHumanSignature(raw json.RawMessage, rpID string, headSeq uint64) DecideProposalOption {
	return func(o *decideProposalOpts) { o.humanSig = raw; o.rpID = rpID; o.headSeq = headSeq }
}

// DecideProposal applies or rejects a pending definition change. Only a human
// with the approver role decides; signature policy mirrors workflow Decide
// (workflow.go:300-328): a pre-verified WebAuthn assertion is journaled
// additively; with --require-human-signature a human without one is rejected
// outright; a provided raw signature is verified over DefinitionMessage when
// a verifier is wired.
func (e *Engine) DecideProposal(ctx context.Context, actor eventstore.Actor, proposalID string, grant bool, signature []byte, basis *eventstore.Basis, opts ...DecideProposalOption) (*Proposal, error) {
	var do decideProposalOpts
	for _, o := range opts {
		o(&do)
	}
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
	// Signature policy — the same ladder as workflow Decide (workflow.go:
	// 300-328): (1) a WebAuthn assertion the API already verified is accepted
	// and journaled; (2) --require-human-signature rejects a human without
	// one; (3) a raw signature over DefinitionMessage is verified when a
	// verifier is wired; (4) --require-signatures (test-only) rejects when
	// nothing verified. Until any flag is set, a token-authenticated decision
	// is allowed and journaled unchanged (dev-mode, I-1).
	switch {
	case do.humanSig != nil:
		// already verified by the caller before invoking DecideProposal.
	case e.requireHumanSig && actor.Type == eventstore.ActorHuman:
		return nil, &Err{Code: CodeSignatureRequired,
			Message: "this node requires a WebAuthn human signature (passkey) on definition decisions",
			FixHint: "decide from the UI, which signs with a registered passkey — token-only decisions are not accepted"}
	case signature != nil && e.verify != nil:
		if err := e.verify(ctx, actor.ID, DefinitionMessage(proposalID, decision), signature); err != nil {
			return nil, &Err{Code: CodePermissionDenied,
				Message: "definition signature is invalid: " + err.Error(),
				Rule:    "a provided signature must verify"}
		}
	case e.requireSig:
		return nil, &Err{Code: CodePermissionDenied,
			Message: "this node requires a signed definition decision (passkey)",
			Rule:    "signatures are mandatory on definition decisions"}
	}
	payload, _ := json.Marshal(proposalPayload{
		ProposalID: proposalID, HumanSignature: do.humanSig,
		RPID: do.rpID, HeadSeq: do.headSeq, Signature: signature,
	})
	if ev, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor: actor, Kind: kind,
		Subject:    eventstore.Subject{ProposalID: proposalID},
		Payload:    payload,
		Basis:      basis,
		DefVersion: e.defVersion,
		Signature:  signature,
	}); err != nil {
		return nil, err
	} else if !ev.TS.IsZero() {
		ts := ev.TS
		p.DecidedAt = &ts
	}
	if !grant {
		p.Status = ProposalRejected
		return p, nil
	}
	p.Status = ProposalApproved

	next, errs := dsl.CompileMulti(p.Files)
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
	e.files = cloneFiles(p.Files)
	e.defVersion++
	p.Status = ProposalApplied
	return p, nil
}

// DefinitionContentHash is the stable content hash of a proposal's files —
// sorted keys, "<key>\n<len>\n<content>" framing. It binds a human signature
// to the EXACT definition content being signed (Security Pro MAJOR-1: the
// WebAuthn canonical must include it, so a signature can never be replayed
// onto different content under the same proposal id).
func DefinitionContentHash(files map[string]string) string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%s\n%d\n%s\n", k, len(files[k]), files[k])
	}
	return hex.EncodeToString(h.Sum(nil))
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

// Proposals lists EVERY definition change (pending + decided), newest first
// — the company's model history (product mission C: «какая definition
// version активна, кто её изменил, почему, когда»). UUID v7 ids are
// time-ordered, so a descending id sort IS the change timeline.
func (e *Engine) Proposals() []*Proposal {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*Proposal, 0, len(e.proposals))
	for _, p := range e.proposals {
		cp := *p
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
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
// Security Pro MAJOR-1: it must REPORT the dangerous deltas — removed roles,
// permission-rule changes and approval-gate/guard changes — instead of
// collapsing them into "no structural changes". The signer sees exactly what
// the change does to authority before signing.
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
	for role := range old.Roles {
		if _, ok := next.Roles[role]; !ok {
			plan = append(plan, "REMOVE role "+role)
		}
	}
	// permission deltas per role (authority changes are the dangerous ones)
	for role, perm := range next.Perms {
		oldPerm, existed := old.Perms[role]
		if !existed {
			plan = append(plan, "permissions for role "+role+": "+fmt.Sprint(len(perm.Rules))+" rules (new)")
			continue
		}
		// compare rule CONTENT, not just counts: act [approve, reject] vs
		// act [approve] must be reported even when the rule count is equal
		oldSet, nextSet := map[string]bool{}, map[string]bool{}
		for _, r := range oldPerm.Rules {
			oldSet[renderRule(r)] = true
		}
		for _, r := range perm.Rules {
			nextSet[renderRule(r)] = true
		}
		changed := len(oldSet) != len(nextSet)
		if !changed {
			for k := range oldSet {
				if !nextSet[k] {
					changed = true
					break
				}
			}
		}
		if changed {
			plan = append(plan, fmt.Sprintf("permissions for role %s changed: %d → %d rules",
				role, len(oldPerm.Rules), len(perm.Rules)))
		}
	}
	for role := range old.Perms {
		if _, ok := next.Perms[role]; !ok {
			plan = append(plan, "REMOVE all permissions for role "+role)
		}
	}
	// workflow deltas: gates and guards
	for entity := range next.Workflows {
		if _, ok := old.Workflows[entity]; !ok {
			plan = append(plan, "add workflow for "+entity)
			continue
		}
		oldT := map[string]*dsl.TransitionDecl{}
		for _, t := range old.Workflows[entity].Transitions {
			oldT[t.Action] = &t
		}
		for _, t := range next.Workflows[entity].Transitions {
			prev, existed := oldT[t.Action]
			if !existed {
				note := "add transition " + entity + "." + t.Action
				if t.ApprovalRole != "" {
					note += " (requires approval " + t.ApprovalRole + ")"
				}
				plan = append(plan, note)
				continue
			}
			if prev.ApprovalRole != t.ApprovalRole {
				plan = append(plan, fmt.Sprintf("approval gate changed: %s.%s: %q → %q",
					entity, t.Action, prev.ApprovalRole, t.ApprovalRole))
			}
			if prev.When != t.When {
				plan = append(plan, fmt.Sprintf("guard changed: %s.%s", entity, t.Action))
			}
		}
		for _, t := range old.Workflows[entity].Transitions {
			if _, ok := nextT(next, entity, t.Action); !ok {
				plan = append(plan, "REMOVE transition "+entity+"."+t.Action)
			}
		}
	}
	if len(plan) == 0 {
		plan = []string{"no changes (identical definition)"}
	}
	return plan
}

func nextT(next *dsl.Model, entity, action string) (*dsl.TransitionDecl, bool) {
	for _, t := range next.Workflows[entity].Transitions {
		if t.Action == action {
			return &t, true
		}
	}
	return nil, false
}

// renderRule renders one permission rule canonically (verb + items) so rule
// CONTENT can be compared for the signer-visible plan.
func renderRule(r dsl.PermRule) string {
	var b strings.Builder
	b.WriteString(r.Verb)
	for _, it := range r.Items {
		b.WriteString("|")
		b.WriteString(it.Entity)
		b.WriteString(".")
		b.WriteString(it.Field)
		for _, n := range it.Names {
			b.WriteString(",")
			b.WriteString(n)
		}
		if it.Where != "" {
			b.WriteString(" where ")
			b.WriteString(it.Where)
		}
	}
	return b.String()
}
