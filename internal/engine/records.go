package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// CRUD over the projection. Every mutation: permission check → validation →
// journal append → projection update, under one lock. No basis, no mutation.

// sealPII encrypts every declared `pii subject=…` field in values before the
// journal append, so the hash chain covers ciphertext (LLD-16 §1/§3). The
// subject is the value of the subject ref field, taken from values, else from
// the existing record (an update that does not restate the subject).
func (e *Engine) sealPII(decl *dsl.EntityDecl, values, existing map[string]any) error {
	if e.pii == nil {
		return nil
	}
	for _, f := range decl.Fields {
		if f.PII == "" {
			continue
		}
		v, present := values[f.Name]
		if !present {
			continue
		}
		subject, _ := values[f.PII].(string)
		if subject == "" && existing != nil {
			subject, _ = existing[f.PII].(string)
		}
		if subject == "" {
			return &Err{Code: CodeValidation,
				Message: fmt.Sprintf("%s.%s: pii subject %s has no value", decl.Name, f.Name, f.PII),
				FixHint: "set the subject ref field before writing a pii field"}
		}
		plain, ok := v.(string)
		if !ok {
			continue // non-string pii value: stored as-is (validation already passed)
		}
		sealed, err := e.pii.Seal(subject, decl.Name+"."+f.Name, []byte(plain))
		if err != nil {
			return err
		}
		values[f.Name] = sealed
	}
	return nil
}

// openPII decrypts sealed pii fields in a read projection; a shredded subject
// becomes an explicit tombstone (LLD-16 §2), never an error.
func (e *Engine) openPII(decl *dsl.EntityDecl, values map[string]any) {
	if e.pii == nil {
		return
	}
	for _, f := range decl.Fields {
		if f.PII == "" {
			continue
		}
		v, present := values[f.Name]
		if !present {
			continue
		}
		if pt, ok := e.pii.Open(v, decl.Name+"."+f.Name); ok {
			values[f.Name] = string(pt)
			continue
		}
		values[f.Name] = piiTombstone(v)
	}
}

func piiTombstone(v any) string {
	id := ""
	if m, ok := v.(map[string]any); ok {
		id, _ = m["key_id"].(string)
	}
	return "[shredded: " + id + "]"
}

// Create inserts a record on behalf of actor.
func (e *Engine) Create(ctx context.Context, actor eventstore.Actor, entity string, values map[string]any, basis *eventstore.Basis, idemKey string) (*Record, error) {
	if basis == nil {
		return nil, &Err{Code: CodeBasisRequired, Message: "mutation without basis",
			FixHint: "pass the task, rule, ADR or human instruction this action is based on"}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.createCore(ctx, actor, entity, values, basis, idemKey, true)
}

// createCore is Create's body, assumed to run under e.mu (held by the public
// Create, or by DecideMutationProposal re-applying an approved shadow
// proposal). checkShadow gates the LLD-09 §1 redirect: false ONLY when this
// call IS that approved application — the shadow gate was already discharged
// by the human decision, so re-checking it here would either loop forever
// (the actor's role is very likely still in the shadow set) or silently
// re-propose instead of ever applying. Every other check (permission,
// validation, refs, unique) runs exactly as it would for a direct call — I-2
// is deferred, never bypassed.
func (e *Engine) createCore(ctx context.Context, actor eventstore.Actor, entity string, values map[string]any, basis *eventstore.Basis, idemKey string, checkShadow bool) (*Record, error) {
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

	if checkShadow && e.isShadow(actor.Role) {
		mp, err := e.proposeMutation(ctx, actor, MutationCreate, entity, "", "", "", values, basis, idemKey)
		if err != nil {
			return nil, err
		}
		return &Record{Entity: entity, Values: vals, ShadowProposalID: mp.ID}, nil
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	if err := e.sealPII(decl, vals, nil); err != nil {
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
	e.runEventTriggers(ctx, "create", entity, rec.ID, nil)
	// return the same enriched shape as Get/Query: computed fields evaluated,
	// unreadable fields masked — so a caller sees its serial number and any
	// computed values immediately, not a bare echo of what it sent.
	full := e.withComputed(decl, rec.ID, rec.Values)
	masked := e.maskFields(actor, entity, full)
	e.openPII(decl, masked)
	return &Record{ID: rec.ID, Entity: entity, Values: masked, Inconsistent: rec.Inconsistent}, nil
}

// createLocked creates a record on behalf of the system (an automation `create`
// action, LLD-01 §4b). It assumes e.mu is HELD (it runs inside a cascade) and
// bypasses ABAC — the rule is the authority, basis is the rule — but keeps the
// full validation pipeline and fires the target's create-cascade in the same
// op-bracket. It is the permission-free twin of Create; the two must not diverge
// in validation.
func (e *Engine) createLocked(ctx context.Context, entity string, values map[string]any, basis *eventstore.Basis, idemKey string) (*Record, error) {
	decl, errr := e.entityOrErr(entity)
	if errr != nil {
		return nil, errr
	}
	if idemKey != "" {
		if prior, err := e.store.ByIdemKey(ctx, idemKey); err != nil {
			return nil, err
		} else if prior != nil {
			if rec, ok := e.records[prior.Subject.Entity][prior.Subject.RecordID]; ok {
				return rec, nil
			}
		}
	}
	if decl.Singleton && len(e.records[entity]) > 0 {
		return nil, &Err{Code: CodeConflict, Message: entity + " is a singleton and already exists",
			FixHint: "update the existing record instead of creating another"}
	}
	if err := e.checkWorkflowField(entity, values); err != nil {
		return nil, err
	}
	vals := make(map[string]any, len(values))
	for k, v := range values {
		vals[k] = v
	}
	e.assignSerials(decl, vals)
	if err := e.validateValues(decl, vals, false, autoActor.ID); err != nil {
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
	if err := e.sealPII(decl, vals, nil); err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(createdPayload{Values: vals})
	ev, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:          autoActor,
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
	if existing, ok := e.records[entity][ev.Subject.RecordID]; ok {
		return existing, nil
	}
	rec := &Record{ID: ev.Subject.RecordID, Entity: entity, Values: vals}
	if e.records[entity] == nil {
		e.records[entity] = map[string]*Record{}
	}
	e.records[entity][rec.ID] = rec
	e.dropIndex(entity)
	e.setStateSince(entity, rec.ID, ev.TS)
	// the created record's own cascade runs in the SAME op (LLD-01 §4b):
	e.runAutoTransitions(ctx, entity, rec.ID)
	e.runEventTriggers(ctx, "create", entity, rec.ID, nil)
	return rec, nil
}

// updateLocked applies a partial change on behalf of the system (an automation
// `update` action, LLD-01 §4b). Assumes e.mu is HELD; bypasses ABAC but keeps
// validation and the workflow-field guard. Permission-free twin of Update.
func (e *Engine) updateLocked(ctx context.Context, entity, id string, values map[string]any, basis *eventstore.Basis, idemKey string) error {
	decl, errr := e.entityOrErr(entity)
	if errr != nil {
		return errr
	}
	rec, ok := e.records[entity][id]
	if !ok {
		return &Err{Code: CodeNotFound, Message: entity + " " + id + " not found"}
	}
	if err := checkConsistent(rec); err != nil {
		return err
	}
	if err := e.checkWorkflowField(entity, values); err != nil {
		return err
	}
	if err := e.validateValues(decl, values, true, autoActor.ID); err != nil {
		return err
	}
	if err := e.checkRefsExist(decl, values); err != nil {
		return err
	}
	merged := make(map[string]any, len(rec.Values)+len(values))
	for k, v := range rec.Values {
		merged[k] = v
	}
	for k, v := range values {
		merged[k] = v
	}
	if err := e.checkUnique(decl, merged, id); err != nil {
		return err
	}
	var changes []change
	for k, v := range values {
		if rec.Values[k] != v {
			changes = append(changes, change{Field: k, Old: rec.Values[k], New: v})
		}
	}
	if len(changes) == 0 {
		return nil
	}
	payload, _ := json.Marshal(updatedPayload{Changes: changes})
	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:          autoActor,
		Kind:           eventstore.RecordUpdated,
		Subject:        eventstore.Subject{Entity: entity, RecordID: id},
		Payload:        payload,
		Basis:          basis,
		DefVersion:     e.defVersion,
		IdempotencyKey: idemKey,
	}); err != nil {
		return err
	}
	prev := maps.Clone(rec.Values)
	for _, ch := range changes {
		rec.Values[ch.Field] = ch.New
	}
	e.dropIndex(entity)
	e.runAutoTransitions(ctx, entity, id)
	e.runEventTriggers(ctx, "update", entity, id, prev)
	return nil
}

// resolveAssigns evaluates the field=expr pairs of a create/update/emit action
// against an eval context (the trigger record or event payload).
func (e *Engine) resolveAssigns(assigns []dsl.Assign, selfID string, vals map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(assigns))
	for _, a := range assigns {
		v, ok := e.evalAssignExpr(a.Expr, selfID, vals)
		if !ok {
			return nil, &Err{Code: CodeValidation,
				Message: "cannot evaluate " + a.Field + " = " + a.Expr,
				Field:   a.Field, FixHint: "the expression must resolve against the record's fields"}
		}
		out[a.Field] = v
	}
	return out, nil
}

// evalAssignExpr evaluates one create/update/emit expression. Arithmetic
// (in_stock - qty, amount * probability / 100) goes through the computed-field
// evaluator; a bare field/path/literal is resolved with type preserved (so a
// string copy stays a string, an enum value stays its bareword).
func (e *Engine) evalAssignExpr(expr, selfID string, vals map[string]any) (any, bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, false
	}
	if len(expr) >= 2 && expr[0] == '"' && expr[len(expr)-1] == '"' {
		return expr[1 : len(expr)-1], true
	}
	if f, err := strconv.ParseFloat(expr, 64); err == nil {
		return f, true // a bare numeric literal (e.g. probability = 100)
	}
	if hasArithOp(expr) {
		return e.evalComputed(expr, selfID, vals)
	}
	c := e.ctxFor(selfID, eventstore.Actor{}, vals)
	kind := "path"
	if strings.HasPrefix(expr, "$") || expr == "true" || expr == "false" || expr == "null" {
		kind = "lit"
	}
	return resolveOperand(operand{kind: kind, text: expr}, c)
}

// hasArithOp reports whether an expression combines operands with +, *, / or a
// binary minus (a leading '-' is unary; a '-' inside a word like a-b is arith).
func hasArithOp(expr string) bool {
	if strings.ContainsAny(expr, "+*/") {
		return true
	}
	for i := 1; i < len(expr); i++ {
		if expr[i] == '-' {
			return true
		}
	}
	return false
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
	return e.updateCore(ctx, actor, entity, id, values, basis, idemKey, true)
}

// updateCore is Update's body, assumed to run under e.mu. See createCore for
// what checkShadow means and why DecideMutationProposal calls this directly
// (already holding the lock) instead of the public, lock-taking Update.
func (e *Engine) updateCore(ctx context.Context, actor eventstore.Actor, entity, id string, values map[string]any, basis *eventstore.Basis, idemKey string, checkShadow bool) (*Record, error) {
	decl, errr := e.entityOrErr(entity)
	if errr != nil {
		return nil, errr
	}
	rec, ok := e.records[entity][id]
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: entity + " " + id + " not found"}
	}
	if err := checkConsistent(rec); err != nil {
		return nil, err
	}
	if err := e.checkWorkflowField(entity, values); err != nil {
		return nil, err
	}
	// Gate (1) (LLD-20): does the actor's role have update access to entity
	// at all, any field scope? Existence only — see canUpdateEntity. The
	// real per-field allow-list, including record-level `where` denies, is
	// gate (2) below (checkFieldWrites), which sees the actual record.
	if d := e.canUpdateEntity(actor.Role, entity); !d.allowed {
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

	if err := e.sealPII(decl, values, rec.Values); err != nil {
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

	if checkShadow && e.isShadow(actor.Role) {
		mp, err := e.proposeMutation(ctx, actor, MutationUpdate, entity, id, "", "", values, basis, idemKey)
		if err != nil {
			return nil, err
		}
		preview := make(map[string]any, len(rec.Values))
		for k, v := range rec.Values {
			preview[k] = v
		}
		for _, ch := range changes {
			preview[ch.Field] = ch.New
		}
		return &Record{ID: id, Entity: entity, Values: preview, ShadowProposalID: mp.ID}, nil
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
	prev := maps.Clone(rec.Values)
	for _, ch := range changes {
		rec.Values[ch.Field] = ch.New
	}
	e.dropIndex(entity)
	e.runAutoTransitions(ctx, entity, id)
	e.runEventTriggers(ctx, "update", entity, id, prev)
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
	masked := e.maskFields(actor, entity, full)
	e.openPII(decl, masked)
	return &Record{ID: rec.ID, Entity: entity, Values: masked, Inconsistent: rec.Inconsistent}, nil
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
		masked := e.maskFields(actor, entity, full)
		e.openPII(decl, masked)
		out = append(out, &Record{ID: rec.ID, Entity: entity, Values: masked, Inconsistent: rec.Inconsistent})
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
