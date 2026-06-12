package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/avangerus/kalita/internal/dsl"
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

// RewindTo restores a record to its state as of journal event `toSeq` — the
// multi-step generalization of Revert: "rewind this record to before the agent
// touched it", any number of edits and transitions ago. It replays the record's
// own history up to toSeq to reconstruct the target state, then appends at most
// two compensating events (one record.updated for the field diff, one
// record.action if the state field moved) to walk current -> target. History is
// preserved; the rewind is itself journaled and reversible. Supervisory: gated
// on `full`, same as Revert.
func (e *Engine) RewindTo(ctx context.Context, actor eventstore.Actor, entity, id string, toSeq uint64, basis *eventstore.Basis) (*Record, error) {
	if basis == nil {
		return nil, &Err{Code: CodeBasisRequired, Message: "mutation without basis",
			FixHint: "pass the human instruction this rewind is based on"}
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
	if !e.hasFull(actor.Role, entity) {
		return nil, &Err{Code: CodePermissionDenied,
			Message: "role " + actor.Role + " may not rewind " + entity,
			FixHint: "rewind restores a past state and can move the workflow backward; it needs `full` on the entity"}
	}

	events, err := e.store.All(ctx)
	if err != nil {
		return nil, err
	}
	target, existed := e.recordStateAsOf(events, entity, id, toSeq)
	if !existed {
		return nil, &Err{Code: CodeConflict,
			Message: fmt.Sprintf("seq %d is before %s %s existed", toSeq, entity, id),
			FixHint: "pick a seq at or after the record.created event (see read_journal)"}
	}

	wfField := ""
	if wf, ok := e.model.Workflows[entity]; ok {
		wfField = wf.Field
	}
	// field diff (everything except the workflow state field)
	var changes []change
	seen := map[string]bool{}
	for k, cur := range rec.Values {
		if k == wfField {
			continue
		}
		seen[k] = true
		if tv, ok := target[k]; !equalVals(cur, tv) || !ok {
			changes = append(changes, change{Field: k, Old: cur, New: tv})
		}
	}
	for k, tv := range target {
		if k == wfField || seen[k] {
			continue
		}
		changes = append(changes, change{Field: k, Old: nil, New: tv}) // field existed then, gone now
	}

	stateMoved := wfField != "" && !equalVals(rec.Values[wfField], target[wfField])
	if len(changes) == 0 && !stateMoved {
		return e.enriched(decl, actor, rec), nil // already at that state
	}

	rb := &eventstore.Basis{Type: "rewind", ID: fmt.Sprint(toSeq)}
	if len(changes) > 0 {
		payload, _ := json.Marshal(updatedPayload{Changes: changes})
		if _, err := e.store.Append(ctx, eventstore.AppendInput{
			Actor: actor, Kind: eventstore.RecordUpdated,
			Subject: eventstore.Subject{Entity: entity, RecordID: id},
			Payload: payload, Basis: rb, DefVersion: e.defVersion,
		}); err != nil {
			return nil, err
		}
		for _, ch := range changes {
			rec.Values[ch.Field] = ch.New
		}
	}
	if stateMoved {
		payload, _ := json.Marshal(actionPayload{
			Action: "rewind", From: fmt.Sprint(rec.Values[wfField]), To: fmt.Sprint(target[wfField])})
		if _, err := e.store.Append(ctx, eventstore.AppendInput{
			Actor: actor, Kind: eventstore.RecordAction,
			Subject: eventstore.Subject{Entity: entity, RecordID: id},
			Payload: payload, Basis: rb, DefVersion: e.defVersion,
		}); err != nil {
			return nil, err
		}
		rec.Values[wfField] = target[wfField]
	}
	e.dropIndex(entity)
	return e.enriched(decl, actor, rec), nil
}

// recordStateAsOf reconstructs a record's stored values as of event toSeq by
// folding only that record's create/update/action events (journal is seq-ordered).
// existed is false if the record had no creation at or before toSeq.
func (e *Engine) recordStateAsOf(events []*eventstore.Event, entity, id string, toSeq uint64) (map[string]any, bool) {
	wfField := ""
	if wf, ok := e.model.Workflows[entity]; ok {
		wfField = wf.Field
	}
	var vals map[string]any
	for _, ev := range events {
		if ev.Seq > toSeq {
			break
		}
		if ev.Subject.Entity != entity || ev.Subject.RecordID != id {
			continue
		}
		switch ev.Kind {
		case eventstore.RecordCreated:
			var p createdPayload
			if json.Unmarshal(ev.Payload, &p) == nil {
				vals = map[string]any{}
				for k, v := range p.Values {
					vals[k] = v
				}
			}
		case eventstore.RecordUpdated:
			var p updatedPayload
			if vals != nil && json.Unmarshal(ev.Payload, &p) == nil {
				for _, ch := range p.Changes {
					vals[ch.Field] = ch.New
				}
			}
		case eventstore.RecordAction:
			var p actionPayload
			if vals != nil && wfField != "" && json.Unmarshal(ev.Payload, &p) == nil {
				vals[wfField] = p.To
			}
		}
	}
	return vals, vals != nil
}

// enriched returns the read-shape of a record (computed fields, masking).
func (e *Engine) enriched(decl *dsl.EntityDecl, actor eventstore.Actor, rec *Record) *Record {
	full := e.withComputed(decl, rec.ID, rec.Values)
	return &Record{ID: rec.ID, Entity: rec.Entity, Values: e.maskFields(actor, rec.Entity, full)}
}

// equalVals compares two stored values via their JSON-marshalled form — robust
// to the any-typed numbers and nested maps that come back from the journal.
func equalVals(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
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
