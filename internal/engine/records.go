package engine

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/avangerus/kalita/internal/dsl"
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
	if d := e.can(actor, "create", entity, "", nil); !d.allowed {
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
	if err := e.checkFieldWrites(actor, "create", entity, vals, nil); err != nil {
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
	e.dropIndex(entity)
	e.setStateSince(entity, rec.ID, ev.TS)
	e.runAutoTransitions(ctx, entity, rec.ID)
	e.runEventTriggers(ctx, "create", entity, rec.ID)
	// return the same enriched shape as Get/Query: computed fields evaluated,
	// unreadable fields masked — so a caller sees its serial number and any
	// computed values immediately, not a bare echo of what it sent.
	full := e.withComputed(decl, rec.ID, rec.Values)
	return &Record{ID: rec.ID, Entity: entity, Values: e.maskFields(actor, entity, full)}, nil
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
	if d := e.can(actor, "update", entity, "", rec.Values); !d.allowed {
		return nil, denied(actor.Role, "update", entity, d.rule)
	}
	if err := e.checkFieldWrites(actor, "update", entity, values, rec.Values); err != nil {
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
	e.dropIndex(entity)
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
	if d := e.can(actor, "read", entity, "", rec.Values); !d.allowed {
		return nil, &Err{Code: CodeNotFound, Message: entity + " " + id + " not found"}
	}
	full := e.withComputed(decl, rec.ID, rec.Values)
	return &Record{ID: rec.ID, Entity: entity, Values: e.maskFields(actor, entity, full)}, nil
}

// QueryOpts are query parameters. Filter (equality map) is the simple form;
// Where is the full condition language (or/ranges/ref-paths); Sort orders by
// fields (prefix "-" = descending); Search is a full-text match over text and
// string fields.
type QueryOpts struct {
	Filter map[string]any
	Where  string
	Sort   []string
	Search string
	Limit  int
	Offset int
}

// Query lists records the role can see, fields masked. Applies permissions,
// then Filter/Where/Search, then Sort, then Offset/Limit.
func (e *Engine) Query(ctx context.Context, actor eventstore.Actor, entity string, opts QueryOpts) ([]*Record, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	decl, errr := e.entityOrErr(entity)
	if errr != nil {
		return nil, errr
	}
	search := strings.ToLower(strings.TrimSpace(opts.Search))

	// permitted-set fast path: if the actor's read scope is index-backed, iterate
	// only the candidate rows instead of every row. can() below stays the final
	// authority, so this only narrows the scan — it can never widen access.
	ids, narrowed := e.candidateIDs(entity, actor)
	if narrowed {
		sort.Strings(ids)
	} else {
		ids = sortedIDs(e.records[entity])
	}

	var out []*Record
	for _, id := range ids {
		rec := e.records[entity][id]
		if rec == nil {
			continue
		}
		if d := e.can(actor, "read", entity, "", rec.Values); !d.allowed {
			continue
		}
		full := e.withComputed(decl, rec.ID, rec.Values)

		if !matchesFilter(full, opts.Filter) {
			continue
		}
		if opts.Where != "" && !evalWhere(opts.Where, e.ctxFor(rec.ID, actor, full)) {
			continue
		}
		if search != "" && !matchesSearch(decl, full, search) {
			continue
		}
		out = append(out, &Record{ID: rec.ID, Entity: entity, Values: e.maskFields(actor, entity, full)})
	}

	sortRecords(out, opts.Sort)

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

func matchesFilter(full map[string]any, filter map[string]any) bool {
	for k, want := range filter {
		if full[k] != want {
			return false
		}
	}
	return true
}

// matchesSearch tests whether any text/string field contains the term.
func matchesSearch(decl *dsl.EntityDecl, full map[string]any, term string) bool {
	for _, f := range decl.Fields {
		if f.Type.Kind != dsl.TyScalar {
			continue
		}
		if f.Type.Scalar == "string" || f.Type.Scalar == "text" {
			if s, ok := full[f.Name].(string); ok && strings.Contains(strings.ToLower(s), term) {
				return true
			}
		}
	}
	return false
}

// sortRecords orders by the given fields; "-field" = descending. Stable.
func sortRecords(recs []*Record, sortBy []string) {
	if len(sortBy) == 0 {
		return
	}
	sort.SliceStable(recs, func(i, j int) bool {
		for _, key := range sortBy {
			desc := strings.HasPrefix(key, "-")
			field := strings.TrimPrefix(key, "-")
			c := compareValues(recs[i].Values[field], recs[j].Values[field])
			if c == 0 {
				continue
			}
			if desc {
				return c > 0
			}
			return c < 0
		}
		return false
	})
}

// compareValues orders two field values: numbers numerically, else by string.
func compareValues(a, b any) int {
	if af, ok := toFloat(a); ok {
		if bf, ok := toFloat(b); ok {
			switch {
			case af < bf:
				return -1
			case af > bf:
				return 1
			default:
				return 0
			}
		}
	}
	as, bs := toStr(a), toStr(b)
	return strings.Compare(as, bs)
}

func toStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
