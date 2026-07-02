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
func (e *Engine) fireEmits(ctx context.Context, actor eventstore.Actor, entity, id string, plans []emitPlan, basis *eventstore.Basis) {
	for _, pl := range plans {
		payload, _ := json.Marshal(eventEmittedPayload{Name: pl.canonical, Payload: pl.payload})
		ev, err := e.store.Append(ctx, eventstore.AppendInput{
			Actor:      actor,
			Kind:       eventstore.EventEmitted,
			Subject:    eventstore.Subject{Entity: entity, RecordID: id},
			Payload:    payload,
			Basis:      basis,
			DefVersion: e.defVersion,
			SchemaV:    1,
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
