package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Op-brackets (LLD-01 §2): a multi-event operation — rewind that appends more
// than one compensating event, or an automation cascade (fireActions) with at
// least one action — is wrapped in op.begin ... op.end. A crash between them
// leaves an unclosed bracket; startup recovery (recoverUnclosedOps) turns that
// into an explicit, journaled fact (op.aborted) instead of a silent
// desync (HLD I-3, H2: "никакой авто-магии доигрывания").

// maxCascadeDepth bounds a chain of secondary cascades sharing one op (LLD-01
// §2: "Глубина цепочки каскада ограничена 8"). Nothing in PR-1..PR-3 actually
// nests a cascade inside another yet (that arrives with PR-4b's create/update
// actions, which can trigger further on-create automation) — this is the
// counter PR-4b hooks into. See agent/kalita/QUESTIONS-LLD.md for the
// discrepancy with runAutoTransitions' separate, pre-existing 10-hop limit.
// D-012: maxCascadeDepth=8 vs auto-hops<10 — осознанное расхождение, см. docs/forge/DECISIONS.md.
const maxCascadeDepth = 8

type opInfo struct {
	Entity   string
	RecordID string
}

// opContext is the ONE cascade the engine is currently inside (LLD-01 §2:
// nesting is forbidden — a secondary cascade reuses it instead of opening a
// new bracket). Lives under e.mu like every other engine field.
type opContext struct {
	ID       string
	Kind     string // "rewind" | "cascade"
	Entity   string
	RecordID string
	Depth    int // secondary cascades sharing this op increment this
}

type opBeginPayload struct {
	OpID     string `json:"op_id"`
	OpKind   string `json:"op_kind"` // "rewind" | "cascade"
	Entity   string `json:"entity"`
	RecordID string `json:"record_id"`
}

type opEndPayload struct {
	OpID string `json:"op_id"`
}

type opAbortedPayload struct {
	OpID   string `json:"op_id"`
	Reason string `json:"reason"`
}

// opBasis is the provenance recorded on op.begin/op.end/op.aborted: these are
// kernel bookkeeping events, not a domain actor's decision.
var opBasis = &eventstore.Basis{Type: "rule", ID: "op-bracket"}

// beginOp opens an op-bracket for a multi-event operation unless the engine is
// already inside one, in which case the enclosing op is reused (no nesting).
// The returned bool is true only when THIS call opened the bracket — only the
// opener may close it.
func (e *Engine) beginOp(ctx context.Context, kind, entity, recordID string) (bool, error) {
	if e.currentOp != nil {
		e.currentOp.Depth++
		if e.currentOp.Depth > maxCascadeDepth {
			return false, fmt.Errorf("engine: automation cascade exceeded max depth %d (op %s)", maxCascadeDepth, e.currentOp.ID)
		}
		return false, nil
	}
	id, err := uuid.NewV7()
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(opBeginPayload{OpID: id.String(), OpKind: kind, Entity: entity, RecordID: recordID})
	if err != nil {
		return false, err
	}
	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:      autoActor,
		Kind:       eventstore.OpBegin,
		Subject:    eventstore.Subject{Entity: entity, RecordID: recordID},
		Payload:    payload,
		Basis:      opBasis,
		DefVersion: e.defVersion,
		SchemaV:    1,
	}); err != nil {
		return false, err
	}
	oc := &opContext{ID: id.String(), Kind: kind, Entity: entity, RecordID: recordID}
	e.currentOp = oc
	e.openOps[oc.ID] = opInfo{Entity: entity, RecordID: recordID}
	return true, nil
}

// endOp closes the op-bracket this call opened. Callers must only invoke it
// when their own beginOp returned true.
func (e *Engine) endOp(ctx context.Context) error {
	if e.currentOp == nil {
		return nil
	}
	id := e.currentOp.ID
	payload, err := json.Marshal(opEndPayload{OpID: id})
	if err != nil {
		return err
	}
	_, err = e.store.Append(ctx, eventstore.AppendInput{
		Actor:      autoActor,
		Kind:       eventstore.OpEnd,
		Subject:    eventstore.Subject{Entity: e.currentOp.Entity, RecordID: e.currentOp.RecordID},
		Payload:    payload,
		Basis:      opBasis,
		DefVersion: e.defVersion,
		SchemaV:    1,
	})
	delete(e.openOps, id)
	e.currentOp = nil
	return err
}

// applyOpBegin/End/Aborted fold op-bracket events into openOps and, for
// op.aborted, the Inconsistent projection. Called both for historical events
// (ordinary replay) and for the op.aborted this engine itself appends during
// recoverUnclosedOps — same code path, same semantics either time.
func (e *Engine) applyOpBegin(ev *eventstore.Event) {
	var p opBeginPayload
	if json.Unmarshal(ev.Payload, &p) != nil {
		return
	}
	e.openOps[p.OpID] = opInfo{Entity: p.Entity, RecordID: p.RecordID}
}

func (e *Engine) applyOpEnd(ev *eventstore.Event) {
	var p opEndPayload
	if json.Unmarshal(ev.Payload, &p) != nil {
		return
	}
	delete(e.openOps, p.OpID)
}

func (e *Engine) applyOpAborted(ev *eventstore.Event) {
	var p opAbortedPayload
	if json.Unmarshal(ev.Payload, &p) != nil {
		return
	}
	if info, ok := e.openOps[p.OpID]; ok {
		if rec, ok := e.records[info.Entity][info.RecordID]; ok {
			rec.Inconsistent = true
		}
		delete(e.openOps, p.OpID)
	}
}

// recoverUnclosedOps runs once at the end of New()'s replay. Any op_id still
// open (op.begin with no matching op.end/op.aborted) was interrupted mid-flight
// — a crash between two compensating events, or between two cascade actions.
// LLD-01 §2 recovery: append op.aborted, mark the record Inconsistent (derived,
// not journaled), and escalate to the node's approver role. Deterministic
// order (sorted op_id — uuid v7 is time-ordered, so this is also chronological).
func (e *Engine) recoverUnclosedOps(ctx context.Context) error {
	if len(e.openOps) == 0 {
		return nil
	}
	ids := make([]string, 0, len(e.openOps))
	for id := range e.openOps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, opID := range ids {
		info, ok := e.openOps[opID]
		if !ok {
			continue // aborted by a sibling below (shouldn't happen, defensive)
		}
		payload, err := json.Marshal(opAbortedPayload{OpID: opID, Reason: "unclosed after replay"})
		if err != nil {
			return err
		}
		if _, err := e.store.Append(ctx, eventstore.AppendInput{
			Actor:      autoActor,
			Kind:       eventstore.OpAborted,
			Subject:    eventstore.Subject{Entity: info.Entity, RecordID: info.RecordID},
			Payload:    payload,
			Basis:      &eventstore.Basis{Type: "rule", ID: "op-recovery"},
			DefVersion: e.defVersion,
			SchemaV:    1,
		}); err != nil {
			return fmt.Errorf("engine: recovery could not append op.aborted for op %s: %w", opID, err)
		}
		delete(e.openOps, opID)
		if rec, ok := e.records[info.Entity][info.RecordID]; ok {
			rec.Inconsistent = true
		}
		if _, err := e.createTask(ctx, autoActor, Task{
			Kind: TaskEscalation, Role: e.defApprover, Entity: info.Entity, RecordID: info.RecordID, Action: "inconsistent",
		}, &eventstore.Basis{Type: "rule", ID: "op-recovery"}, "inconsistent|"+opID); err != nil {
			return fmt.Errorf("engine: recovery could not create escalation task for op %s: %w", opID, err)
		}
	}
	return nil
}

// checkConsistent rejects mutations on a record left Inconsistent by a crashed
// operation (LLD-01 §2). Revert/RewindTo/ConfirmConsistent are exempt — they
// are the sanctioned way out.
func checkConsistent(rec *Record) *Err {
	if rec == nil || !rec.Inconsistent {
		return nil
	}
	return &Err{Code: CodeRecordInconsistent,
		Message: "record is inconsistent: a crashed operation left it mid-flight",
		FixHint: "supervisor must rewind or confirm"}
}

// ConfirmConsistent clears the Inconsistent flag after a supervisor (the
// node's approver role) has reviewed a record flagged by op-bracket recovery
// and accepts its current state as-is (the alternative to rewinding it).
// Journaled as an ordinary record.action in the system area — a kernel-
// synthesized action name, like Revert's "revert_<action>", not a declared
// workflow transition.
func (e *Engine) ConfirmConsistent(ctx context.Context, actor eventstore.Actor, entity, id string, basis *eventstore.Basis) (*Record, error) {
	if basis == nil {
		return nil, &Err{Code: CodeBasisRequired, Message: "mutation without basis",
			FixHint: "pass the human instruction this confirmation is based on"}
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	decl, errr := e.entityOrErr(entity)
	if errr != nil {
		return nil, errr
	}
	rec, ok := e.records[entity][id]
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: entity + " " + id + " not found"}
	}
	if actor.Role != e.defApprover {
		return nil, &Err{Code: CodePermissionDenied,
			Message: "role " + actor.Role + " may not confirm_consistent",
			Rule:    "confirm_consistent is reserved for the node's approver role"}
	}
	if !rec.Inconsistent {
		return nil, &Err{Code: CodeConflict, Message: entity + " " + id + " is not marked inconsistent"}
	}

	current := ""
	if wf, ok := e.model.Workflows[entity]; ok {
		current, _ = rec.Values[wf.Field].(string)
	}
	payload, err := json.Marshal(actionPayload{Action: "confirm_consistent", From: current, To: current})
	if err != nil {
		return nil, err
	}
	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:      actor,
		Kind:       eventstore.RecordAction,
		Subject:    eventstore.Subject{Entity: entity, RecordID: id},
		Payload:    payload,
		Basis:      basis,
		DefVersion: e.defVersion,
	}); err != nil {
		return nil, err
	}
	rec.Inconsistent = false
	return e.enriched(decl, actor, rec), nil
}
