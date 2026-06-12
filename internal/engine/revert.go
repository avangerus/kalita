package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Revert — "откат любого действия", the founder's headline guarantee, done the
// only way an append-only journal allows: not by erasing history (that would
// break the hash chain) but by appending a COMPENSATING event that restores the
// prior state. The undo is itself a signed, hash-chained event — auditable, and
// reversible in turn (revert again to redo). It undoes the record's most recent
// mutation:
//
//   - a field edit  -> a record.updated that swaps each change new->old back
//   - a transition  -> a record.action that moves the state field back
//
// This is the one move a normal update cannot make: it can walk a workflow
// BACKWARD across a one-way transition. So it is a supervisory capability —
// gated on `full` permission over the entity, which an agent role (read/update/
// act + deny) never holds. Agents act forward; humans correct.
func (e *Engine) Revert(ctx context.Context, actor eventstore.Actor, entity, id string, basis *eventstore.Basis) (*Record, error) {
	if basis == nil {
		return nil, &Err{Code: CodeBasisRequired, Message: "mutation without basis",
			FixHint: "pass the human instruction this revert is based on"}
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, errr := e.entityOrErr(entity); errr != nil {
		return nil, errr
	}
	rec, ok := e.records[entity][id]
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: entity + " " + id + " not found"}
	}
	if !e.hasFull(actor.Role, entity) {
		return nil, &Err{Code: CodePermissionDenied,
			Message:  "role " + actor.Role + " may not revert " + entity,
			FixHint:  "revert undoes a change and can move the workflow backward; it needs `full` on the entity"}
	}

	last, err := e.lastMutation(ctx, entity, id)
	if err != nil {
		return nil, err
	}
	if last == nil || last.Kind == eventstore.RecordCreated {
		return nil, &Err{Code: CodeConflict,
			Message: "nothing to revert on " + entity + " " + id,
			FixHint: "the record has no change to undo (a bare creation is not revertible in v0)"}
	}

	switch last.Kind {
	case eventstore.RecordUpdated:
		var p updatedPayload
		if json.Unmarshal(last.Payload, &p) != nil {
			return nil, &Err{Code: CodeConflict, Message: "cannot read the change to revert"}
		}
		undo := make([]change, 0, len(p.Changes))
		for _, ch := range p.Changes {
			undo = append(undo, change{Field: ch.Field, Old: ch.New, New: ch.Old})
		}
		payload, _ := json.Marshal(updatedPayload{Changes: undo})
		if _, err := e.store.Append(ctx, eventstore.AppendInput{
			Actor: actor, Kind: eventstore.RecordUpdated,
			Subject:    eventstore.Subject{Entity: entity, RecordID: id},
			Payload:    payload,
			Basis:      &eventstore.Basis{Type: "revert", ID: fmt.Sprint(last.Seq)},
			DefVersion: e.defVersion,
		}); err != nil {
			return nil, err
		}
		for _, ch := range undo {
			rec.Values[ch.Field] = ch.New
		}

	case eventstore.RecordAction:
		var p actionPayload
		if json.Unmarshal(last.Payload, &p) != nil {
			return nil, &Err{Code: CodeConflict, Message: "cannot read the transition to revert"}
		}
		// move the state field back: a kernel override, not a declared transition,
		// so it can undo even a one-way move.
		payload, _ := json.Marshal(actionPayload{Action: "revert_" + p.Action, From: p.To, To: p.From})
		if _, err := e.store.Append(ctx, eventstore.AppendInput{
			Actor: actor, Kind: eventstore.RecordAction,
			Subject:    eventstore.Subject{Entity: entity, RecordID: id},
			Payload:    payload,
			Basis:      &eventstore.Basis{Type: "revert", ID: fmt.Sprint(last.Seq)},
			DefVersion: e.defVersion,
		}); err != nil {
			return nil, err
		}
		if wf, ok := e.model.Workflows[entity]; ok {
			rec.Values[wf.Field] = p.From
		}

	default:
		return nil, &Err{Code: CodeConflict, Message: "the last event is not a revertible mutation"}
	}

	e.dropIndex(entity)
	decl, _ := e.entityOrErr(entity)
	full := e.withComputed(decl, rec.ID, rec.Values)
	return &Record{ID: rec.ID, Entity: entity, Values: e.maskFields(actor, entity, full)}, nil
}

// lastMutation returns the most recent create/update/action event for a record,
// or nil if it has none. Scans the journal — revert is a rare supervisory act,
// not a hot path.
func (e *Engine) lastMutation(ctx context.Context, entity, id string) (*eventstore.Event, error) {
	events, err := e.store.All(ctx)
	if err != nil {
		return nil, err
	}
	var last *eventstore.Event
	for _, ev := range events {
		if ev.Subject.Entity != entity || ev.Subject.RecordID != id {
			continue
		}
		switch ev.Kind {
		case eventstore.RecordCreated, eventstore.RecordUpdated, eventstore.RecordAction:
			last = ev
		}
	}
	return last, nil
}

// hasFull reports whether a role holds an unconditional `full` grant over the
// entity — the "owns this entity" capability that revert requires.
func (e *Engine) hasFull(role, entity string) bool {
	pb, ok := e.model.Perms[role]
	if !ok {
		return false
	}
	for _, rule := range pb.Rules {
		if rule.Verb != "full" {
			continue
		}
		for _, item := range rule.Items {
			if (item.All || item.Entity == entity) && item.Where == "" {
				return true
			}
		}
	}
	return false
}
