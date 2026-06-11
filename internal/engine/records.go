package engine

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/avangerus/kalita/internal/eventstore"
)

// CRUD over the projection. Every mutation: permission check → validation →
// journal append → projection update, under one lock. No basis, no mutation.

// Create inserts a record on behalf of actor.
func (e *Engine) Create(ctx context.Context, actor eventstore.Actor, entity string, values map[string]any, basis *eventstore.Basis, idemKey string) (*Record, error) {
	if basis == nil {
		return nil, &Err{Code: CodeBasisRequired, Message: "mutation without basis",
			FixHint: "pass the task, rule, ADR or human instruction this action is based on"}
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	// idempotent retry short-circuits ahead of validation: the original
	// mutation already passed every check
	if idemKey != "" {
		if prior, err := e.store.ByIdemKey(ctx, idemKey); err != nil {
			return nil, err
		} else if prior != nil {
			if rec, ok := e.records[prior.Subject.Entity][prior.Subject.RecordID]; ok {
				return rec, nil
			}
		}
	}

	decl, errr := e.entityOrErr(entity)
	if errr != nil {
		return nil, errr
	}
	if d := e.can(actor.Role, "create", entity, "", nil, actor.ID); !d.allowed {
		return nil, denied(actor.Role, "create", entity, d.rule)
	}
	if decl.Singleton && len(e.records[entity]) > 0 {
		return nil, &Err{Code: CodeConflict,
			Message: entity + " is a singleton and already exists",
			FixHint: "update the existing record instead of creating another"}
	}
	if err := e.checkWorkflowField(entity, values); err != nil {
		return nil, err
	}
	vals := make(map[string]any, len(values))
	for k, v := range values {
		vals[k] = v
	}
	e.assignSerials(decl, vals) // document numbers before validation
	if err := e.validateValues(decl, vals, false, actor.ID); err != nil {
		return nil, err
	}
	if err := e.checkFieldWrites(actor.Role, "create", entity, vals, nil, actor.ID); err != nil {
		return nil, err
	}
	if err := e.checkRefsExist(decl, vals); err != nil {
		return nil, err
	}
	if err := e.checkUnique(decl, vals, ""); err != nil {
		return nil, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(createdPayload{Values: vals})
	ev, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:          actor,
		Kind:           eventstore.RecordCreated,
		Subject:        eventstore.Subject{Entity: entity, RecordID: id.String()},
		Payload:        payload,
		Basis:          basis,
		DefVersion:     e.defVersion,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		return nil, err
	}
	// idempotent replay: the original event may carry a different record id
	rec := &Record{ID: ev.Subject.RecordID, Entity: entity, Values: vals}
	if existing, ok := e.records[entity][ev.Subject.RecordID]; ok {
		return existing, nil
	}
	if e.records[entity] == nil {
		e.records[entity] = map[string]*Record{}
	}
	e.records[entity][rec.ID] = rec
	e.setStateSince(entity, rec.ID, ev.TS)
	e.runAutoTransitions(ctx, entity, rec.ID)
	e.runEventTriggers(ctx, "create", entity, rec.ID)
	return rec, nil
}

// checkWorkflowField rejects direct writes to a workflow-governed field:
// state changes exist only as transitions.
func (e *Engine) checkWorkflowField(entity string, values map[string]any) *Err {
	wf, ok := e.model.Workflows[entity]
	if !ok {
		return nil
	}
	if _, touched := values[wf.Field]; touched {
		return invalid(wf.Field, entity+"."+wf.Field+" is governed by the workflow",
			"use act(...) with a transition action; the state field cannot be written directly")
	}
	return nil
}

// Update applies a partial change to a record.
func (e *Engine) Update(ctx context.Context, actor eventstore.Actor, entity, id string, values map[string]any, basis *eventstore.Basis, idemKey string) (*Record, error) {
	if basis == nil {
		return nil, &Err{Code: CodeBasisRequired, Message: "mutation without basis",
			FixHint: "pass the task, rule, ADR or human instruction this action is based on"}
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
	if err := e.checkWorkflowField(entity, values); err != nil {
		return nil, err
	}
	if d := e.can(actor.Role, "update", entity, "", rec.Values, actor.ID); !d.allowed {
		return nil, denied(actor.Role, "update", entity, d.rule)
	}
	if err := e.checkFieldWrites(actor.Role, "update", entity, values, rec.Values, actor.ID); err != nil {
		return nil, err
	}
	if err := e.validateValues(decl, values, true, actor.ID); err != nil {
		return nil, err
	}
	if err := e.checkRefsExist(decl, values); err != nil {
		return nil, err
	}
	merged := make(map[string]any, len(rec.Values)+len(values))
	for k, v := range rec.Values {
		merged[k] = v
	}
	for k, v := range values {
		merged[k] = v
	}
	if err := e.checkUnique(decl, merged, id); err != nil {
		return nil, err
	}

	var changes []change
	for k, v := range values {
		if rec.Values[k] != v {
			changes = append(changes, change{Field: k, Old: rec.Values[k], New: v})
		}
	}
	if len(changes) == 0 {
		return rec, nil
	}
	payload, _ := json.Marshal(updatedPayload{Changes: changes})
	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:          actor,
		Kind:           eventstore.RecordUpdated,
		Subject:        eventstore.Subject{Entity: entity, RecordID: id},
		Payload:        payload,
		Basis:          basis,
		DefVersion:     e.defVersion,
		IdempotencyKey: idemKey,
	}); err != nil {
		return nil, err
	}
	for _, ch := range changes {
		rec.Values[ch.Field] = ch.New
	}
	e.runAutoTransitions(ctx, entity, id)
	e.runEventTriggers(ctx, "update", entity, id)
	return rec, nil
}

// Get returns a record with unreadable fields masked. A record the role may
// not read at all reports NOT_FOUND, not PERMISSION_DENIED: what you cannot
// see does not exist (MCP-CONTRACT-v0 §3).
func (e *Engine) Get(ctx context.Context, actor eventstore.Actor, entity, id string) (*Record, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	decl, errr := e.entityOrErr(entity)
	if errr != nil {
		return nil, errr
	}
	rec, ok := e.records[entity][id]
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: entity + " " + id + " not found"}
	}
	if d := e.can(actor.Role, "read", entity, "", rec.Values, actor.ID); !d.allowed {
		return nil, &Err{Code: CodeNotFound, Message: entity + " " + id + " not found"}
	}
	full := e.withComputed(decl, rec.ID, rec.Values)
	return &Record{ID: rec.ID, Entity: entity, Values: e.maskFields(actor.Role, entity, full, actor.ID)}, nil
}

// QueryOpts are v0 query parameters: equality filters, limit, offset cursor.
type QueryOpts struct {
	Filter map[string]any
	Limit  int
	Offset int
}

// Query lists records the role can see, fields masked, deterministic order.
func (e *Engine) Query(ctx context.Context, actor eventstore.Actor, entity string, opts QueryOpts) ([]*Record, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	decl, errr := e.entityOrErr(entity)
	if errr != nil {
		return nil, errr
	}
	var out []*Record
	for _, id := range sortedIDs(e.records[entity]) {
		rec := e.records[entity][id]
		if d := e.can(actor.Role, "read", entity, "", rec.Values, actor.ID); !d.allowed {
			continue
		}
		full := e.withComputed(decl, rec.ID, rec.Values)
		match := true
		for k, want := range opts.Filter {
			if full[k] != want {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		out = append(out, &Record{ID: rec.ID, Entity: entity, Values: e.maskFields(actor.Role, entity, full, actor.ID)})
		// deterministic id order makes early exit safe: collected enough for
		// the page plus the has-next probe — stop scanning
		if opts.Limit > 0 && len(out) > opts.Offset+opts.Limit {
			break
		}
	}
	if opts.Offset > 0 {
		if opts.Offset >= len(out) {
			return nil, nil
		}
		out = out[opts.Offset:]
	}
	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
}
