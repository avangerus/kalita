package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/google/uuid"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Workflow execution. The state field is owned by the workflow: it changes
// only through transitions — there is no API to write it directly. A
// transition with `requires approval` does not exist until a human signs.

type actionPayload struct {
	Action string `json:"action"`
	From   string `json:"from"`
	To     string `json:"to"`
	Guard  string `json:"guard,omitempty"` // recorded guard text (determinism)
}

type approvalPayload struct {
	Entity   string `json:"entity"`
	RecordID string `json:"record_id"`
	Action   string `json:"action"`
	From     string `json:"from"`
	To       string `json:"to"`
	Role     string `json:"role"`
	Reason   string `json:"reason,omitempty"`
	// HumanSignature/RPID/HeadSeq are ADDITIVE fields on approval.decided
	// (LLD-02 §1, §5): the WebAuthn assertion a human signed the decision
	// with, the relying-party id, and the head_seq captured when its
	// challenge was issued — everything `kalita verify` needs to rebuild the
	// EXACT canonical string offline (task_id/entity/record_id/transition/
	// decision/basis are all otherwise reconstructable from the journal
	// itself; node_id comes from node.pub; head_seq is NOT reconstructable
	// after the fact from anything else, so it must be journaled here —
	// LLD-02 §5, "самодостаточная" offline verification). Absence of
	// HumanSignature means a token-only decision (dev-mode) — every event
	// written before LLD-02 is valid exactly as it was (I-1). The engine
	// does not parse or verify this payload itself: by the time Decide sees
	// it, the API layer (internal/api, which owns the credential registry
	// and canonical-string reconstruction) has already verified it — see
	// DecideOption/WithHumanSignature below.
	HumanSignature json.RawMessage `json:"human_signature,omitempty"`
	RPID           string          `json:"rp_id,omitempty"`
	HeadSeq        uint64          `json:"head_seq,omitempty"`
}

type ApprovalStatus string

const (
	ApprovalPending        ApprovalStatus = "pending"
	ApprovalGrantedStatus  ApprovalStatus = "granted"
	ApprovalRejectedStatus ApprovalStatus = "rejected"
)

// Approval is a pending human decision: the HITL gate.
type Approval struct {
	ID          string           `json:"id"`
	Entity      string           `json:"entity"`
	RecordID    string           `json:"record_id"`
	Action      string           `json:"action"`
	From        string           `json:"from"`
	To          string           `json:"to"`
	Role        string           `json:"role"`
	RequestedBy eventstore.Actor `json:"requested_by"`
	Status      ApprovalStatus   `json:"status"`
}

// ActResult reports what happened: applied, or parked for a signature.
type ActResult struct {
	Status     string  `json:"status"` // applied | pending_approval
	ApprovalID string  `json:"approval_id,omitempty"`
	Record     *Record `json:"record,omitempty"`
}

const approvalDomain = "kalita-approval-v1"

// ApprovalMessage is the byte string a human signs when deciding an approval.
// It is reproducible offline: domain || id || decision.
func ApprovalMessage(approvalID, decision string) []byte {
	return []byte(approvalDomain + "|" + approvalID + "|" + decision)
}

// Act executes a named workflow transition on a record.
func (e *Engine) Act(ctx context.Context, actor eventstore.Actor, entity, id, action string, basis *eventstore.Basis, idemKey string) (*ActResult, error) {
	if basis == nil {
		return nil, &Err{Code: CodeBasisRequired, Message: "mutation without basis",
			FixHint: "pass the task, rule, approval or human instruction this action is based on"}
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	decl, errr := e.entityOrErr(entity)
	if errr != nil {
		return nil, errr
	}
	wf, ok := e.model.Workflows[entity]
	if !ok {
		return nil, &Err{Code: CodeValidation, Message: entity + " has no workflow",
			FixHint: "declare a workflow block for the entity"}
	}
	rec, ok := e.records[entity][id]
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: entity + " " + id + " not found"}
	}
	if err := checkConsistent(rec); err != nil {
		return nil, err
	}
	if !e.canAct(actor.Role, action) {
		return nil, &Err{Code: CodePermissionDenied,
			Message: fmt.Sprintf("role %s may not act %s", actor.Role, action),
			Rule:    "act permissions list the allowed transition actions"}
	}

	current, _ := rec.Values[wf.Field].(string)
	tr := findTransition(wf, action, current)
	if tr == nil {
		return nil, &Err{Code: CodeValidation,
			Message: fmt.Sprintf("no transition %s from state %s", action, current),
			FixHint: "check the workflow: the action may not exist or the record is in another state"}
	}
	if tr.When != "" {
		full := e.withComputed(decl, rec.ID, rec.Values)
		if !evalWhere(tr.When, e.ctxFor(rec.ID, actor, full)) {
			return nil, &Err{Code: CodeGuardFailed,
				Message: fmt.Sprintf("guard `%s` is false for %s %s", tr.When, entity, id),
				FixHint: "the transition exists but its condition does not hold for this record"}
		}
	}

	if tr.ApprovalRole != "" {
		aid, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		payload, _ := json.Marshal(approvalPayload{
			Entity: entity, RecordID: id, Action: action, From: current, To: tr.To, Role: tr.ApprovalRole,
		})
		if _, err := e.store.Append(ctx, eventstore.AppendInput{
			Actor:          actor,
			Kind:           eventstore.ApprovalRequested,
			Subject:        eventstore.Subject{ApprovalID: aid.String()},
			Payload:        payload,
			Basis:          basis,
			DefVersion:     e.defVersion,
			IdempotencyKey: idemKey,
		}); err != nil {
			return nil, err
		}
		e.approvals[aid.String()] = &Approval{
			ID: aid.String(), Entity: entity, RecordID: id, Action: action,
			From: current, To: tr.To, Role: tr.ApprovalRole, RequestedBy: actor, Status: ApprovalPending,
		}
		return &ActResult{Status: "pending_approval", ApprovalID: aid.String()}, nil
	}

	if err := e.applyTransitionAndEmit(ctx, actor, entity, id, tr, current, basis, idemKey); err != nil {
		return nil, err
	}
	e.runAutoTransitions(ctx, entity, id)
	return &ActResult{Status: "applied", Record: e.records[entity][id]}, nil
}

// DecideOption customizes a Decide call with the WebAuthn era's additive
// human_signature (LLD-02). Omitting every option reproduces the pre-LLD-02
// behavior exactly (I-1) — this is why it is a trailing variadic, not a new
// positional parameter: every existing call site across the codebase (mcp
// dogfood tests, engine tests) compiles and behaves unchanged.
type DecideOption func(*decideOpts)

type decideOpts struct {
	humanSig json.RawMessage // pre-verified by the caller — see WithHumanSignature
	rpID     string
	headSeq  uint64
}

// WithHumanSignature attaches a WebAuthn assertion to a decision. The caller
// (internal/api, which owns the credential registry and canonical-string
// reconstruction, LLD-02 §2-3) MUST have already verified the assertion
// before calling Decide — Decide only journals it additively on
// approval.decided, it does not re-verify crypto. rpID and headSeq are
// journaled alongside it (LLD-02 §5, offline re-verification: node_id comes
// from node.pub and the rest of the canonical from the journal itself, but
// head_seq — the head captured when the challenge was issued — cannot be
// reconstructed after the fact from anything else).
func WithHumanSignature(raw json.RawMessage, rpID string, headSeq uint64) DecideOption {
	return func(o *decideOpts) { o.humanSig = raw; o.rpID = rpID; o.headSeq = headSeq }
}

// GetApproval returns a pending or decided approval by id, read-only. LLD-02
// §4 needs this: the challenge-issuance endpoint must read the approval's
// entity/record/action/role to build the canonical string BEFORE any
// verification happens, without mutating engine state.
func (e *Engine) GetApproval(id string) (*Approval, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	a, ok := e.approvals[id]
	if !ok {
		return nil, false
	}
	cp := *a
	return &cp, true
}

// CanApprove exported wrapper over canApprove (LLD-02 §4): the challenge
// endpoint gates issuance on the same role check Decide itself enforces, so
// a role that cannot decide an approval cannot even fetch its challenge.
func (e *Engine) CanApprove(role, action string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.canApprove(role, action)
}

// Decide resolves a pending approval. When a verifier is wired, a valid
// signature over ApprovalMessage(id, decision) is mandatory — the signature
// lands in the journal and verifies offline. opts carries the WebAuthn-era
// additive human_signature (LLD-02) — see DecideOption.
func (e *Engine) Decide(ctx context.Context, actor eventstore.Actor, approvalID string, grant bool, signature []byte, basis *eventstore.Basis, opts ...DecideOption) (*Record, error) {
	var do decideOpts
	for _, o := range opts {
		o(&do)
	}
	if basis == nil {
		return nil, &Err{Code: CodeBasisRequired, Message: "decision without basis",
			FixHint: "reference the approval request"}
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	a, ok := e.approvals[approvalID]
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: "approval " + approvalID + " not found"}
	}
	if a.Status != ApprovalPending {
		return nil, &Err{Code: CodeConflict, Message: "approval already " + string(a.Status),
			FixHint: "decisions are final; request a new transition if needed"}
	}
	if !e.canApprove(actor.Role, a.Action) {
		return nil, &Err{Code: CodePermissionDenied,
			Message: fmt.Sprintf("role %s may not approve %s", actor.Role, a.Action),
			Rule:    "approve permissions list the actions a role signs"}
	}
	// checked before the decision is journaled (not after): a decision on an
	// inconsistent record must not be recorded as if it could be applied.
	if rec, ok := e.records[a.Entity][a.RecordID]; ok {
		if err := checkConsistent(rec); err != nil {
			return nil, err
		}
	}
	decision := "granted"
	kind := eventstore.ApprovalGranted
	if !grant {
		decision = "rejected"
		kind = eventstore.ApprovalRejected
	}
	// Signature policy, checked in order:
	//  1. a verified WebAuthn human_signature (LLD-02) always satisfies both
	//     enforcement flags — it is the strongest proof there is.
	//  2. --require-human-signature (LLD-02 §5): a human actor without one
	//     is rejected outright, regardless of any raw signature bytes —
	//     this is the production gate ("approve без passkey невозможно").
	//     Agents are unaffected: they never carried WebAuthn credentials to
	//     begin with (LLD-02 §HLD H1: "человек-WebAuthn / агент-Ed25519").
	//  3. the pre-LLD-02 path: verify a raw signature when one is provided
	//     and a verifier is wired.
	//  4. --require-signatures (pre-LLD-02, test-only today): reject if
	//     nothing verified.
	// Until any flag is set, a token-authenticated decision is allowed and
	// journaled unchanged — the API already proved identity (dev-mode, I-1).
	switch {
	case do.humanSig != nil:
		// already verified by the caller before invoking Decide.
	case e.requireHumanSig && actor.Type == eventstore.ActorHuman:
		return nil, &Err{Code: CodeSignatureRequired,
			Message: "this node requires a WebAuthn human signature (passkey) on HITL decisions",
			FixHint: "decide from the approval UI, which signs with a registered passkey — token/MCP decisions are not accepted"}
	case signature != nil && e.verify != nil:
		if err := e.verify(ctx, actor.ID, ApprovalMessage(approvalID, decision), signature); err != nil {
			return nil, &Err{Code: CodePermissionDenied,
				Message: "approval signature is invalid: " + err.Error(),
				Rule:    "a provided signature must verify"}
		}
	case e.requireSig:
		return nil, &Err{Code: CodePermissionDenied,
			Message: "this node requires a signed approval (passkey)",
			Rule:    "signatures are mandatory on HITL decisions"}
	}

	payload, _ := json.Marshal(approvalPayload{
		Entity: a.Entity, RecordID: a.RecordID, Action: a.Action, From: a.From, To: a.To, Role: a.Role,
		HumanSignature: do.humanSig, RPID: do.rpID, HeadSeq: do.headSeq,
	})
	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:      actor,
		Kind:       kind,
		Subject:    eventstore.Subject{ApprovalID: approvalID},
		Payload:    payload,
		Basis:      basis,
		DefVersion: e.defVersion,
		Signature:  signature,
	}); err != nil {
		return nil, err
	}

	if !grant {
		a.Status = ApprovalRejectedStatus
		return e.records[a.Entity][a.RecordID], nil
	}
	a.Status = ApprovalGrantedStatus

	rec, ok := e.records[a.Entity][a.RecordID]
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: "record vanished"}
	}
	current, _ := rec.Values[e.model.Workflows[a.Entity].Field].(string)
	if current != a.From {
		return nil, &Err{Code: CodeConflict,
			Message: fmt.Sprintf("record moved to %s while approval was pending", current),
			FixHint: "request the transition again from the current state"}
	}
	// resolve the real transition decl so its `emits` clauses fire (the approval
	// path reconstructs from/to/action but must carry the declared emits).
	tr := findTransition(e.model.Workflows[a.Entity], a.Action, current)
	if tr == nil {
		tr = &dsl.TransitionDecl{From: a.From, To: a.To, Action: a.Action}
	}
	if err := e.applyTransitionAndEmit(ctx, actor, a.Entity, a.RecordID,
		tr, current, &eventstore.Basis{Type: "approval", ID: approvalID}, ""); err != nil {
		return nil, err
	}
	e.runAutoTransitions(ctx, a.Entity, a.RecordID)
	return e.records[a.Entity][a.RecordID], nil
}

// PendingApprovals lists open approvals a role can decide.
func (e *Engine) PendingApprovals(role string) []*Approval {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var out []*Approval
	for _, a := range e.approvals {
		if a.Status == ApprovalPending && (a.Role == role || e.canApprove(role, a.Action)) {
			out = append(out, a)
		}
	}
	return out
}

// applyTransition appends record.action and moves the projection.
func (e *Engine) applyTransition(ctx context.Context, actor eventstore.Actor, entity, id string, tr *dsl.TransitionDecl, from string, basis *eventstore.Basis, idemKey string) error {
	payload, _ := json.Marshal(actionPayload{Action: tr.Action, From: from, To: tr.To, Guard: tr.When})
	ev, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:          actor,
		Kind:           eventstore.RecordAction,
		Subject:        eventstore.Subject{Entity: entity, RecordID: id},
		Payload:        payload,
		Basis:          basis,
		DefVersion:     e.defVersion,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		return err
	}
	e.records[entity][id].Values[e.model.Workflows[entity].Field] = tr.To
	e.dropIndex(entity)
	e.setStateSince(entity, id, ev.TS)
	return nil
}

// applyTransitionAndEmit applies a transition and, if it carries `emits`
// clauses (LLD-01 §4), journals the domain event(s) and dispatches subscribers
// inside one op-bracket. Emit payloads are evaluated and validated BEFORE the
// transition is journaled, so a payload error aborts the WHOLE transition
// atomically (nothing is appended) — LLD-01 §4.2.
func (e *Engine) applyTransitionAndEmit(ctx context.Context, actor eventstore.Actor, entity, id string, tr *dsl.TransitionDecl, from string, basis *eventstore.Basis, idemKey string) error {
	if len(tr.Emits) == 0 {
		return e.applyTransition(ctx, actor, entity, id, tr, from, basis, idemKey)
	}
	decl := e.model.Entities[entity]
	rec := e.records[entity][id]
	// simulate the post-transition state so emit payloads read the values they
	// would after the move, WITHOUT committing anything yet.
	post := make(map[string]any, len(rec.Values)+1)
	for k, v := range rec.Values {
		post[k] = v
	}
	if wf, ok := e.model.Workflows[entity]; ok {
		post[wf.Field] = tr.To
	}
	full := e.withComputed(decl, id, post)
	plans, errr := e.buildEmitPlans(id, full, tr.Emits)
	if errr != nil {
		return errr // transition aborted: nothing journaled
	}
	// transition + emit(s) + subscribers is a multi-event op (LLD-01 §2).
	opened, err := e.beginOp(ctx, "cascade", entity, id)
	if err != nil {
		return err
	}
	if err := e.applyTransition(ctx, actor, entity, id, tr, from, basis, idemKey); err != nil {
		if opened {
			if cerr := e.endOp(ctx); cerr != nil {
				log.Printf("engine: could not close op-bracket after failed transition entity=%s record=%s: %v", entity, id, cerr)
				AutomationJournalFailures.Add(1)
			}
		}
		return err
	}
	e.fireEmits(ctx, actor, entity, id, plans, basis, "")
	if opened {
		if cerr := e.endOp(ctx); cerr != nil {
			log.Printf("engine: could not close emit op-bracket entity=%s record=%s: %v", entity, id, cerr)
			AutomationJournalFailures.Add(1)
		}
	}
	return nil
}

var autoActor = eventstore.Actor{Type: eventstore.ActorSystem, ID: "workflow-engine"}

// runAutoTransitions fires auto transitions whose guards hold, repeatedly,
// with a hard bound against cycles. Called after every state-affecting change.
func (e *Engine) runAutoTransitions(ctx context.Context, entity, id string) {
	wf, ok := e.model.Workflows[entity]
	if !ok {
		return
	}
	decl := e.model.Entities[entity]
	for hops := 0; hops < 10; hops++ {
		rec, ok := e.records[entity][id]
		if !ok {
			return
		}
		current, _ := rec.Values[wf.Field].(string)
		fired := false
		for i := range wf.Transitions {
			tr := &wf.Transitions[i]
			if !tr.Auto || (tr.From != current && tr.From != "any") || tr.To == current {
				continue
			}
			full := e.withComputed(decl, rec.ID, rec.Values)
			if tr.When != "" && !evalWhere(tr.When, e.ctxFor(rec.ID, eventstore.Actor{}, full)) {
				continue
			}
			// auto transitions can also carry `emits` (LLD-01 §4); route them
			// through the emit-aware path. A failed emit payload aborts this
			// auto move (like any transition) — we stop cascading rather than
			// pretend it fired.
			if err := e.applyTransitionAndEmit(ctx, autoActor, entity, id, tr, current,
				&eventstore.Basis{Type: "rule", ID: "auto:" + tr.From + "->" + tr.To}, ""); err != nil {
				log.Printf("engine: auto transition %s->%s on %s %s did not apply: %v", tr.From, tr.To, entity, id, err)
				break
			}
			fired = true
			break
		}
		if !fired {
			break
		}
	}
	// the record settled in a state: queue work for assignees of the
	// transitions now available from it
	e.ensureWorkflowTasks(ctx, entity, id)
}

func findTransition(wf *dsl.WorkflowDecl, action, current string) *dsl.TransitionDecl {
	for i := range wf.Transitions {
		tr := &wf.Transitions[i]
		if tr.Action == action && !tr.Auto && (tr.From == current || tr.From == "any") {
			return tr
		}
	}
	return nil
}

// canAct / canApprove: permission checks for the act/approve verbs.
func (e *Engine) canAct(role, action string) bool     { return e.canNamed(role, "act", action) }
func (e *Engine) canApprove(role, action string) bool { return e.canNamed(role, "approve", action) }

// canEmit is the Permission Engine check for the `emit` verb (LLD-01 §4.2,
// HLD I-2): an actor may emit a domain event only under a grant of
// `emit [Name]` for that event's short name.
func (e *Engine) canEmit(role, name string) bool { return e.canNamed(role, "emit", name) }

// canTail is the Permission Engine check for the `tail` verb (LLD-08 §2): a
// node-wide, unbounded read over the journal is a stronger surface than any
// per-record read grant, so it is never inferred from `read` — a role must
// explicitly hold `tail [kind]` for the kind's own eventstore.Kind name.
func (e *Engine) canTail(role, kind string) bool { return e.canNamed(role, "tail", kind) }

func (e *Engine) canNamed(role, verb, name string) bool {
	pb, ok := e.model.Perms[role]
	if !ok {
		return false
	}
	// denies first
	for _, rule := range pb.Rules {
		if rule.Verb != "deny" {
			continue
		}
		for _, item := range rule.Items {
			if item.Verb == verb && (item.All || contains(item.Names, name)) {
				return false
			}
		}
	}
	for _, rule := range pb.Rules {
		if rule.Verb != verb {
			continue
		}
		for _, item := range rule.Items {
			if item.All || contains(item.Names, name) {
				return true
			}
		}
	}
	return false
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
