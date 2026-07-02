package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Domain events (LLD-01 §4). A pack declares an `event`, transitions/automations
// `emit` it, and other rules subscribe with `on event`. Emitting happens AFTER a
// successful transition, INSIDE the op-bracket of the cascade (so a crash mid
// dispatch is recoverable), and each subscriber is an independent action with
// the failed/retry mechanics of §3. event.emitted itself never mutates a record
// — every change is made by a subscriber under its own action.

type eventEmittedPayload struct {
	Name    string         `json:"name"`
	Payload map[string]any `json:"payload"`
}

// EmittedFact is the folded projection of one event.emitted entry. It carries
// no state a subscriber didn't already journal separately — it exists for
// audit/observability and to make dispatch NON-idempotent-on-replay explicit:
// on replay we fold the fact but do NOT re-dispatch (subscriber effects are
// already in the journal as their own events).
type EmittedFact struct {
	Name    string
	Payload map[string]any
	Seq     uint64
}

// applyEventEmitted folds event.emitted (LLD-01 §4). Records the fact; does not
// touch record projections and does not re-dispatch subscribers.
func (e *Engine) applyEventEmitted(ev *eventstore.Event) {
	var p eventEmittedPayload
	if json.Unmarshal(ev.Payload, &p) != nil {
		return
	}
	e.emittedEvents = append(e.emittedEvents, EmittedFact{Name: p.Name, Payload: p.Payload, Seq: ev.Seq})
}

// EmittedEvents returns the domain events journaled so far (audit/test view).
func (e *Engine) EmittedEvents() []EmittedFact {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]EmittedFact, len(e.emittedEvents))
	copy(out, e.emittedEvents)
	return out
}

// emitPlan is a resolved, validated emit ready to be journaled and dispatched.
type emitPlan struct {
	canonical string
	payload   map[string]any
}

// buildEmitPlans evaluates and validates every emit clause against the record's
// values (the post-transition state, for a transition emit). Any evaluation or
// schema failure returns an error so the WHOLE transition aborts atomically
// (LLD-01 §4.2) — the caller must call this BEFORE journaling the transition.
func (e *Engine) buildEmitPlans(selfID string, vals map[string]any, emits []dsl.EmitClause) ([]emitPlan, *Err) {
	var plans []emitPlan
	for _, ec := range emits {
		decl, canonical, own := e.model.EventByRef(ec.Event)
		if !own || decl == nil {
			return nil, &Err{Code: CodeValidation,
				Message: "emit references unknown event " + ec.Event,
				FixHint: "declare the event or fix the name"}
		}
		payload := make(map[string]any, len(ec.Args))
		for _, as := range ec.Args {
			v, ok := e.evalAssignExpr(as.Expr, selfID, vals)
			if !ok {
				return nil, invalid(as.Field,
					"cannot evaluate emit payload "+as.Field+" = "+as.Expr,
					"the expression must resolve against the record's fields")
			}
			payload[as.Field] = v
		}
		if err := e.validateEventPayload(decl, payload); err != nil {
			return nil, err
		}
		plans = append(plans, emitPlan{canonical: canonical, payload: payload})
	}
	return plans, nil
}

// validateEventPayload type-checks a resolved payload against the event schema
// (LLD-01 §4.2): types, required fields, ref existence.
func (e *Engine) validateEventPayload(decl *dsl.EventDecl, payload map[string]any) *Err {
	byName := map[string]*dsl.FieldDecl{}
	for _, f := range decl.Fields {
		byName[f.Name] = f
	}
	for name, v := range payload {
		f, ok := byName[name]
		if !ok {
			return invalid(name, "event "+decl.Name+" has no payload field "+name,
				"emit only declared payload fields")
		}
		if v != nil {
			if err := checkType(f, v); err != nil {
				return err
			}
		}
	}
	for _, f := range decl.Fields {
		v, present := payload[f.Name]
		if f.Required && (!present || v == nil || v == "") {
			return invalid(f.Name, "event "+decl.Name+" payload is missing required field "+f.Name,
				"add "+f.Name+" to the emit")
		}
		if f.Type.Kind == dsl.TyRef {
			if id, _ := v.(string); id != "" && !strings.HasPrefix(f.Type.RefTarget, "core.") {
				if _, exists := e.records[f.Type.RefTarget][id]; !exists {
					return invalid(f.Name, f.Name+" references missing "+f.Type.RefTarget+" "+id,
						"pass the id of an existing "+f.Type.RefTarget)
				}
			}
		}
	}
	return nil
}

// fireEmits journals event.emitted for each plan (schema_v 1) and dispatches its
// subscribers synchronously. Called inside an already-open op-bracket. A failed
// event.emitted Append is never swallowed (logged + counted, LLD-01 §3).
//
// idemKey threads LLD-01 §5 idempotency through to the Store: a transition's
// `emits` clauses have no caller-supplied key (idemKey == "" — the transition's
// own Append already dedups the whole cascade on retry), while the MCP
// emit_event tool (a free-standing emit with no enclosing transition) passes
// its caller's key. When more than one plan shares a non-empty key (only
// possible for multiple `emits` clauses on one transition — emit_event always
// builds exactly one plan) each plan gets its own derived key so they cannot
// collide with one another under ByIdemKey.
func (e *Engine) fireEmits(ctx context.Context, actor eventstore.Actor, entity, id string, plans []emitPlan, basis *eventstore.Basis, idemKey string) {
	for i, pl := range plans {
		key := idemKey
		if key != "" && len(plans) > 1 {
			key = fmt.Sprintf("%s|%d", idemKey, i)
		}
		payload, _ := json.Marshal(eventEmittedPayload{Name: pl.canonical, Payload: pl.payload})
		ev, err := e.store.Append(ctx, eventstore.AppendInput{
			Actor:          actor,
			Kind:           eventstore.EventEmitted,
			Subject:        eventstore.Subject{Entity: entity, RecordID: id},
			Payload:        payload,
			Basis:          basis,
			DefVersion:     e.defVersion,
			SchemaV:        1,
			IdempotencyKey: key,
		})
		if err != nil {
			log.Printf("engine: CRITICAL could not journal event.emitted %s entity=%s record=%s: %v", pl.canonical, entity, id, err)
			AutomationJournalFailures.Add(1)
			continue
		}
		e.applyEventEmitted(ev)
		e.dispatchEvent(ctx, pl.canonical, pl.payload, entity, id)
	}
}

// mcpEmitBasis is the provenance recorded for a free-standing emit_event MCP
// call (LLD-01 §4.2). The tool's schema is deliberately `{name, payload,
// idempotency_key}` — no basis parameter (unlike create/update/act/comment) —
// because the call itself, under the actor's own identity, IS the provenance;
// the engine synthesizes a kernel basis the same way op-brackets do (op.go's
// opBasis) rather than accepting an unverifiable free-text one from the caller.
var mcpEmitBasis = &eventstore.Basis{Type: "rule", ID: "mcp:emit_event"}

// shortEventName strips a possible "<pack>." qualifier, e.g. for matching an
// `emit [Name]` grant, which is always written unqualified (own-pack only —
// the same ownership invariant `emits`/`emit` enforce at compile time).
func shortEventName(ref string) string {
	if dot := strings.LastIndexByte(ref, '.'); dot >= 0 {
		return ref[dot+1:]
	}
	return ref
}

// EmitEvent is the MCP tool `emit_event` (LLD-01 §4.2): an inbound connector
// or cross-node bridge (H8) emits a domain event directly, outside any DSL
// transition. Same choke point as every other mutation (HLD I-2): verb
// `emit [Name]` via the Permission Engine — checked BEFORE the event is even
// resolved, so an unauthorized caller learns nothing about which events
// exist — then the payload is validated against the event's own schema
// (validateEventPayload, the exact function transitions use), journaled as
// event.emitted under the calling actor, and dispatched via dispatchEvent —
// the identical fold a transition's `emits` clause drives; no parallel path.
//
// Idempotent (LLD-01 §5): a repeat call with the same idempotency_key
// short-circuits to the already-journaled fact without re-checking
// permissions, re-validating the payload or re-dispatching subscribers —
// dispatch has side effects (subscriber actions with their own journal
// writes), so replaying it on every retry would not be safe.
func (e *Engine) EmitEvent(ctx context.Context, actor eventstore.Actor, name string, payload map[string]any, idemKey string) (*EmittedFact, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if idemKey != "" {
		if prior, _ := e.store.ByIdemKey(ctx, idemKey); prior != nil {
			if prior.Kind != eventstore.EventEmitted {
				return nil, &Err{Code: CodeConflict,
					Message: "idempotency_key was already used for a different mutation",
					FixHint: "generate a fresh idempotency_key per task attempt"}
			}
			var p eventEmittedPayload
			if json.Unmarshal(prior.Payload, &p) != nil {
				return nil, &Err{Code: CodeConflict, Message: "cannot read the prior emit for this idempotency_key"}
			}
			return &EmittedFact{Name: p.Name, Payload: p.Payload, Seq: prior.Seq}, nil
		}
	}

	if !e.canEmit(actor.Role, shortEventName(name)) {
		return nil, &Err{Code: CodePermissionDenied,
			Message: fmt.Sprintf("role %s may not emit %s", actor.Role, shortEventName(name)),
			Rule:    "emit permissions list the events a role may emit"}
	}
	decl, canonical, own := e.model.EventByRef(name)
	if !own || decl == nil {
		return nil, &Err{Code: CodeValidation,
			Message: "emit_event references unknown event " + name,
			FixHint: "call describe_system for the declared events, or fix the name; only events declared in the loaded pack(s) can be emitted"}
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if err := e.validateEventPayload(decl, payload); err != nil {
		return nil, err
	}

	// a free-standing emit plus its subscriber dispatch is a multi-event
	// operation (LLD-01 §2: "цепочка emits→подписчики") — bracket it exactly
	// like a transition's emits.
	opened, err := e.beginOp(ctx, "cascade", "", "")
	if err != nil {
		return nil, err
	}
	plan := emitPlan{canonical: canonical, payload: payload}
	before := len(e.emittedEvents)
	e.fireEmits(ctx, actor, "", "", []emitPlan{plan}, mcpEmitBasis, idemKey)
	if opened {
		if cerr := e.endOp(ctx); cerr != nil {
			log.Printf("engine: could not close emit_event op-bracket for %s: %v", canonical, cerr)
			AutomationJournalFailures.Add(1)
		}
	}
	if len(e.emittedEvents) == before {
		return nil, &Err{Code: CodeValidation,
			Message: "emit_event: event.emitted could not be journaled",
			FixHint: "retry; if this persists it is a node-level fault (see server logs)"}
	}
	fact := e.emittedEvents[len(e.emittedEvents)-1]
	return &fact, nil
}

// dispatchEvent runs every `on event` subscriber for a canonical event name, in
// deterministic (file, line) order (LLD-01 §4.2). Each subscriber's actions are
// individual: a failure is journaled (automation.failed) and retried on its own
// key without aborting the emit or the other subscribers. Subscriber
// expressions evaluate against the event payload.
func (e *Engine) dispatchEvent(ctx context.Context, canonical string, payload map[string]any, emitEntity, emitID string) {
	var subs []*dsl.AutomationRule
	for _, r := range e.model.Automations {
		if r.Trigger != "event" {
			continue
		}
		if _, c, _ := e.model.EventByRef(r.Event); c == canonical {
			subs = append(subs, r)
		}
	}
	sort.SliceStable(subs, func(i, j int) bool {
		if subs[i].File != subs[j].File {
			return subs[i].File < subs[j].File
		}
		return subs[i].Line < subs[j].Line
	})
	for _, sub := range subs {
		if sub.When != "" && !evalWhere(sub.When, e.ctxFor(emitID, eventstore.Actor{}, payload)) {
			continue
		}
		ruleKey := ruleKeyOf(sub)
		ruleBasis := &eventstore.Basis{Type: "rule", ID: ruleKey}
		for i, a := range sub.Actions {
			if err := e.execAction(ctx, a, emitEntity, emitID, ruleBasis, "", payload); err != nil {
				e.recordActionFailure(ctx, ruleKey, a.Kind, i, emitEntity, emitID, 1, err)
			}
		}
	}
}

func ruleKeyOf(r *dsl.AutomationRule) string {
	return fmt.Sprintf("%s:%d", r.File, r.Line)
}
