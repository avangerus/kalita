package engine

import (
	"context"
	"encoding/json"
	"fmt"

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
}

type ApprovalStatus string

const (
	ApprovalPending        ApprovalStatus = "pending"
	ApprovalGrantedStatus  ApprovalStatus = "granted"
	ApprovalRejectedStatus ApprovalStatus = "rejected"
)

// Approval is a pending human decision: the HITL gate.
type Approval struct {
	ID          string            `json:"id"`
	Entity      string            `json:"entity"`
	RecordID    string            `json:"record_id"`
	Action      string            `json:"action"`
	From        string            `json:"from"`
	To          string            `json:"to"`
	Role        string            `json:"role"`
	RequestedBy eventstore.Actor  `json:"requested_by"`
	Status      ApprovalStatus    `json:"status"`
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
		full := e.withComputed(decl, rec.Values)
		if !evalWhere(tr.When, evalCtx{values: full, actorID: actor.ID}) {
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

	if err := e.applyTransition(ctx, actor, entity, id, tr, current, basis, idemKey); err != nil {
		return nil, err
	}
	e.runAutoTransitions(ctx, entity, id)
	return &ActResult{Status: "applied", Record: e.records[entity][id]}, nil
}

// Decide resolves a pending approval. When a verifier is wired, a valid
// signature over ApprovalMessage(id, decision) is mandatory — the signature
// lands in the journal and verifies offline.
func (e *Engine) Decide(ctx context.Context, actor eventstore.Actor, approvalID string, grant bool, signature []byte, basis *eventstore.Basis) (*Record, error) {
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
	decision := "granted"
	kind := eventstore.ApprovalGranted
	if !grant {
		decision = "rejected"
		kind = eventstore.ApprovalRejected
	}
	if e.verify != nil {
		if err := e.verify(ctx, actor.ID, ApprovalMessage(approvalID, decision), signature); err != nil {
			return nil, &Err{Code: CodePermissionDenied,
				Message: "approval decision requires a valid signature: " + err.Error(),
				Rule:    "signatures are mandatory on HITL decisions"}
		}
	}

	payload, _ := json.Marshal(approvalPayload{
		Entity: a.Entity, RecordID: a.RecordID, Action: a.Action, From: a.From, To: a.To, Role: a.Role,
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
	tr := &dsl.TransitionDecl{From: a.From, To: a.To, Action: a.Action}
	if err := e.applyTransition(ctx, actor, a.Entity, a.RecordID,
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
	e.setStateSince(entity, id, ev.TS)
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
			full := e.withComputed(decl, rec.Values)
			if tr.When != "" && !evalWhere(tr.When, evalCtx{values: full, actorID: ""}) {
				continue
			}
			_ = e.applyTransition(ctx, autoActor, entity, id, tr, current,
				&eventstore.Basis{Type: "rule", ID: "auto:" + tr.From + "->" + tr.To}, "")
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
