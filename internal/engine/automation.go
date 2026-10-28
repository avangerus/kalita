package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// AutomationJournalFailures counts Append calls for automation.failed /
// automation.abandoned / the abandoned-operator task that themselves errored —
// the last-resort signal for when even "this cascade action failed" could not
// be journaled (LLD-01 §3: never swallowed, never a panic). A metrics exporter
// outside this package can read it; this package only guarantees it moves.
var AutomationJournalFailures atomic.Int64

// retryBackoff is the exponential backoff schedule for a failed automation
// action (LLD-01 §3): 5 values = 5 retries after the original attempt, so an
// action gets at most 6 tries (1 original + 5 retries) before it is abandoned.
var retryBackoff = []time.Duration{
	1 * time.Minute,
	2 * time.Minute,
	4 * time.Minute,
	8 * time.Minute,
	16 * time.Minute,
}

// actionFailState is the retry projection for one automation action (LLD-01
// §3), keyed "rule|record_id|action_index". Built by folding automation.failed
// (sets it) / automation.abandoned (clears it); a live retry that succeeds
// also clears it directly (fold alone cannot see a success — the store's
// idempotency dedup is what makes that safe across a restart: see
// retryFailedActions).
type actionFailState struct {
	RuleKey     string
	Entity      string
	RecordID    string
	ActionKind  string
	ActionIndex int
	Attempt     int
	NextRetryAt time.Time
}

type automationFailedPayload struct {
	Rule        string `json:"rule"`
	Entity      string `json:"entity"`
	RecordID    string `json:"record_id"`
	ActionKind  string `json:"action_kind"`
	ActionIndex int    `json:"action_index"`
	Error       string `json:"error"`
	Attempt     int    `json:"attempt"`
}

type automationAbandonedPayload struct {
	Rule        string `json:"rule"`
	Entity      string `json:"entity"`
	RecordID    string `json:"record_id"`
	ActionKind  string `json:"action_kind"`
	ActionIndex int    `json:"action_index"`
	Attempts    int    `json:"attempts"`
}

func failKey(ruleKey, recordID string, actionIndex int) string {
	return ruleKey + "|" + recordID + "|" + strconv.Itoa(actionIndex)
}

// applyAutomationFailed/Abandoned fold the retry projection (LLD-01 §3).
func (e *Engine) applyAutomationFailed(ev *eventstore.Event) {
	var p automationFailedPayload
	if json.Unmarshal(ev.Payload, &p) != nil {
		return
	}
	st := &actionFailState{
		RuleKey: p.Rule, Entity: p.Entity, RecordID: p.RecordID,
		ActionKind: p.ActionKind, ActionIndex: p.ActionIndex, Attempt: p.Attempt,
	}
	if p.Attempt >= 1 && p.Attempt <= len(retryBackoff) {
		st.NextRetryAt = ev.TS.Add(retryBackoff[p.Attempt-1])
	}
	e.failedActions[failKey(p.Rule, p.RecordID, p.ActionIndex)] = st
}

func (e *Engine) applyAutomationAbandoned(ev *eventstore.Event) {
	var p automationAbandonedPayload
	if json.Unmarshal(ev.Payload, &p) != nil {
		return
	}
	delete(e.failedActions, failKey(p.Rule, p.RecordID, p.ActionIndex))
}

// Automation engine. Event triggers (create/update) fire inline after the
// mutation; schedule and stuck rules fire on Tick, which the node calls every
// minute. A schedule trigger's raw text is validated at compile time against
// the closed grammar dsl.ParseSchedule accepts (E059_BAD_SCHEDULE_FORM,
// internal/dsl/schedule.go — same one-parser-two-layers discipline as
// E052's aggregates): `every <dur>` fires once per interval window; `daily
// [at HH:MM]` fires once per day, on the first Tick at or after that time of
// day (defaulting to midnight) — not, as before, on the first Tick of the day
// regardless of the wall clock. Schedule dedup is per rule (and record): the
// idempotency bucket above prevents re-firing within the same window/day,
// and hasOpenRuleTask (TECHDEBT "Ядро: automation / schedule" P1) additionally
// skips firing at all while the previous firing's task is still open/taken —
// the fix for `on schedule every 2m` piling up a fresh task every bucket
// forever even when nothing has picked up the last one (KnowVault resync).

// Tick evaluates schedule and stuck rules and sweeps expired leases.
func (e *Engine) Tick(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := e.now().UTC()
	day := now.Format("2006-01-02")
	for _, rule := range e.model.Automations {
		key := fmt.Sprintf("%s:%d", rule.File, rule.Line)
		switch rule.Trigger {
		case "schedule":
			bucket, fire, dedupOpen := scheduleBucket(rule.Schedule, now, day)
			if !fire {
				continue
			}
			if rule.Entity == "" {
				if dedupOpen && e.hasOpenRuleTask(key, "", "") {
					continue
				}
				e.fireActions(ctx, rule, "", "", fmt.Sprintf("auto|%s|%s", key, bucket))
				continue
			}
			decl := e.model.Entities[rule.Entity]
			for _, id := range sortedIDs(e.records[rule.Entity]) {
				rec := e.records[rule.Entity][id]
				full := e.withComputed(decl, rec.ID, rec.Values)
				if rule.When != "" && !evalWhere(rule.When, e.ctxFor(rec.ID, eventstore.Actor{}, full)) {
					continue
				}
				if dedupOpen && e.hasOpenRuleTask(key, rule.Entity, id) {
					continue
				}
				e.fireActions(ctx, rule, rule.Entity, id, fmt.Sprintf("auto|%s|%s|%s", key, id, bucket))
			}
		case "stuck":
			wf := e.model.Workflows[rule.Entity]
			dur, err := parseDuration(rule.StuckFor)
			if err != nil {
				continue
			}
			for _, id := range sortedIDs(e.records[rule.Entity]) {
				rec := e.records[rule.Entity][id]
				state, _ := rec.Values[wf.Field].(string)
				since, ok := e.stateSince[rule.Entity][id]
				if state != rule.StuckState || !ok || e.now().Sub(since) < dur {
					continue
				}
				e.fireActions(ctx, rule, rule.Entity, id,
					fmt.Sprintf("stuck|%s|%s|%s", key, id, since.UTC().Format(time.RFC3339Nano)))
			}
		}
	}

	e.retryFailedActions(ctx)

	for _, t := range e.tasks {
		if err := e.expireIfDue(ctx, t); err != nil {
			log.Printf("engine: lease expiry of task %s could not be journaled, lease kept: %v", t.ID, err)
			AutomationJournalFailures.Add(1)
		}
	}
	return nil
}

// runEventTriggers fires create/update rules for a record (called inline
// after the mutation, under the engine lock).
func (e *Engine) runEventTriggers(ctx context.Context, trigger, entity, id string, prev map[string]any) {
	decl := e.model.Entities[entity]
	for _, rule := range e.model.Automations {
		if rule.Trigger != trigger || rule.Entity != entity {
			continue
		}
		rec, ok := e.records[entity][id]
		if !ok {
			return
		}
		full := e.withComputed(decl, rec.ID, rec.Values)
		ec := e.ctxFor(rec.ID, eventstore.Actor{}, full)
		ec.prev = prev
		if rule.When != "" && !evalWhere(rule.When, ec) {
			continue
		}
		e.fireActions(ctx, rule, entity, id, "")
	}
}

// fireActions executes a rule's actions. idemPrefix dedups periodic firings.
// A cascade producing >=2 events (a rule with >=2 actions) is one op-bracket
// (LLD-01 §2) — a crash partway through leaves the bracket open for startup
// recovery to catch. A single action is one event, atomic by itself:
// bracketing it costs +2 journal events per automation firing for nothing
// (caught by the passport bench: tick x2). PR-4b widens this — a lone
// create/update action CAN cascade further and must be bracketed even alone.
// Each action's error is independently journaled (automation.failed) rather
// than aborting its siblings or vanishing (§3, I-3): a rule with a broken
// action still runs its other actions.
func (e *Engine) fireActions(ctx context.Context, rule *dsl.AutomationRule, entity, id, idemPrefix string) {
	ruleKey := fmt.Sprintf("%s:%d", rule.File, rule.Line)
	ruleBasis := &eventstore.Basis{Type: "rule", ID: ruleKey}

	// the eval context for create/update/emit expressions: the trigger record's
	// computed values (LLD-01 §4b). Empty for global schedule rules.
	var vals map[string]any
	if entity != "" && id != "" {
		if decl := e.model.Entities[entity]; decl != nil {
			if rec, ok := e.records[entity][id]; ok {
				vals = e.withComputed(decl, id, rec.Values)
			}
		}
	}

	opened := false
	// A cascade of >=2 events (>=2 actions), OR a single create/update/emit
	// action that can cascade further (LLD-01 §2 as widened by §4b), is wrapped
	// in an op-bracket. A lone notify/agent/escalate/webhook stays unbracketed
	// (one atomic event — bracketing it is pure overhead, caught by the passport
	// bench: tick x2).
	if len(rule.Actions) >= 2 || ruleHasCascadingAction(rule) {
		var err error
		opened, err = e.beginOp(ctx, "cascade", entity, id)
		if err != nil {
			log.Printf("engine: automation cascade rule=%s entity=%s record=%s could not open op-bracket, action(s) NOT executed: %v", ruleKey, entity, id, err)
			AutomationJournalFailures.Add(1)
			return
		}
	}
	for i, a := range rule.Actions {
		idem := ""
		if idemPrefix != "" {
			idem = fmt.Sprintf("%s|%d", idemPrefix, i)
		}
		if err := e.execAction(ctx, a, entity, id, ruleBasis, idem, vals); err != nil {
			e.recordActionFailure(ctx, ruleKey, a.Kind, i, entity, id, 1, err)
		}
	}
	if opened {
		if err := e.endOp(ctx); err != nil {
			log.Printf("engine: automation cascade rule=%s entity=%s record=%s could not close op-bracket: %v", ruleKey, entity, id, err)
			AutomationJournalFailures.Add(1)
		}
	}
}

// ruleHasCascadingAction reports whether a rule has a lone action that can
// itself cascade (create/update/emit) and therefore must be op-bracketed even
// when it is the only action (LLD-01 §2/§4b).
func ruleHasCascadingAction(rule *dsl.AutomationRule) bool {
	for _, a := range rule.Actions {
		switch a.Kind {
		case "create", "update", "emit":
			return true
		}
	}
	return false
}

// execAction runs one automation action and reports whether it succeeded.
// Shared by fireActions (attempt 1) and retryFailedActions (attempt 2+) so
// both paths execute the exact same logic. A role target that does not exist
// in the model is a failure here (referential integrity, not a new permission
// grant/deny) rather than a task silently created for no one to ever pick up.
func (e *Engine) execAction(ctx context.Context, a dsl.AutomationAction, entity, id string, basis *eventstore.Basis, idem string, vals map[string]any) error {
	if a.Role != "" {
		if _, ok := e.model.Roles[a.Role]; !ok {
			return fmt.Errorf("role %q does not exist", a.Role)
		}
	}
	var err error
	switch a.Kind {
	case "agent":
		_, err = e.createTask(ctx, autoActor, Task{
			Kind: TaskAgent, Role: a.Role, Entity: entity, RecordID: id, Action: a.Task, Args: a.Args,
		}, basis, idem)
	case "escalate":
		_, err = e.createTask(ctx, autoActor, Task{
			Kind: TaskEscalation, Role: a.Role, Entity: entity, RecordID: id, Action: "escalation",
		}, basis, idem)
	case "notify":
		_, err = e.createTask(ctx, autoActor, Task{
			Kind: TaskNotification, Entity: entity, RecordID: id, Args: a.Args,
		}, basis, idem)
	case "webhook":
		// the engine records intent; the node's webhook runner delivers.
		// DeliveryID is the per-firing idempotency identity (D-011 hardening):
		// a fresh uuid per FIRING (record-update-then-refire is a NEW business
		// effect), persisted and carried forward across DLQ retries.
		deliveryID, _ := uuid.NewV7()
		_, err = e.createTask(ctx, autoActor, Task{
			Kind: TaskWebhook, Entity: entity, RecordID: id, Args: a.Args,
			DeliveryID: deliveryID.String(),
		}, basis, idem)
	case "create":
		err = e.execCreate(ctx, a, id, basis, idem, vals)
	case "update":
		err = e.execUpdate(ctx, a, entity, id, basis, idem, vals)
	case "emit":
		if errr := e.execEmit(ctx, a, entity, id, basis, vals); errr != nil {
			err = errr
		}
	default:
		err = fmt.Errorf("unknown action kind %q", a.Kind)
	}
	return err
}

// execCreate runs a `create Entity(field=expr,...)` action (LLD-01 §4b): it
// resolves the field expressions against the trigger context, then creates the
// record with full validation. The target's own `on create` automation runs in
// the same cascade (createLocked calls runEventTriggers under the open op).
func (e *Engine) execCreate(ctx context.Context, a dsl.AutomationAction, selfID string, basis *eventstore.Basis, idem string, vals map[string]any) error {
	values, err := e.resolveAssigns(a.Assigns, selfID, vals)
	if err != nil {
		return err
	}
	_, err = e.createLocked(ctx, a.Entity, values, basis, idem)
	return err
}

// execUpdate runs an `update [ref] set field=expr,...` action (LLD-01 §4b). Without
// a ref path it updates the trigger record; with one it follows a single ref
// field of the trigger record. Expressions see the target record's fields
// (e.g. in_stock) merged with the trigger record's fields (e.g. qty).
func (e *Engine) execUpdate(ctx context.Context, a dsl.AutomationAction, entity, id string, basis *eventstore.Basis, idem string, vals map[string]any) error {
	targetEntity, targetID := entity, id
	evalVals := vals
	selfID := id
	if a.RefPath != "" {
		te := e.model.Entities[entity]
		rf := findFieldDecl(te, a.RefPath)
		if rf == nil {
			return fmt.Errorf("update: %s is not a field of %s", a.RefPath, entity)
		}
		refID, _ := vals[a.RefPath].(string)
		if refID == "" {
			return fmt.Errorf("update: ref field %s is empty on %s %s", a.RefPath, entity, id)
		}
		targetEntity, targetID, selfID = rf.Type.RefTarget, refID, refID
		tgtDecl := e.model.Entities[targetEntity]
		rec, ok := e.records[targetEntity][targetID]
		if !ok {
			return fmt.Errorf("update: %s %s not found", targetEntity, targetID)
		}
		merged := e.withComputed(tgtDecl, targetID, rec.Values)
		for k, v := range vals {
			if _, present := merged[k]; !present {
				merged[k] = v
			}
		}
		evalVals = merged
	}
	values, err := e.resolveAssigns(a.Assigns, selfID, evalVals)
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return nil
	}
	return e.updateLocked(ctx, targetEntity, targetID, values, basis, idem)
}

// execEmit runs an `emit Event(...)` automation action (LLD-01 §4).
func (e *Engine) execEmit(ctx context.Context, a dsl.AutomationAction, entity, id string, basis *eventstore.Basis, vals map[string]any) *Err {
	plans, errr := e.buildEmitPlans(id, vals, []dsl.EmitClause{{Event: a.Event, Args: a.Assigns}})
	if errr != nil {
		return errr
	}
	e.fireEmits(ctx, autoActor, entity, id, plans, basis, "")
	return nil
}

// recordActionFailure journals automation.failed for one action's error and
// either schedules the next retry or — once the backoff schedule is exhausted
// — abandons it. The automation.failed Append itself failing is never
// swallowed and never panics (LLD-01 §3): it is logged and counted, and in
// that case the retry projection is deliberately left untouched (we could not
// even confirm the failure was recorded, so we do not pretend to schedule
// a retry for it either).
func (e *Engine) recordActionFailure(ctx context.Context, ruleKey, actionKind string, actionIndex int, entity, id string, attempt int, cause error) {
	payload, _ := json.Marshal(automationFailedPayload{
		Rule: ruleKey, Entity: entity, RecordID: id, ActionKind: actionKind, ActionIndex: actionIndex,
		Error: cause.Error(), Attempt: attempt,
	})
	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:      autoActor,
		Kind:       eventstore.AutomationFailed,
		Subject:    eventstore.Subject{Entity: entity, RecordID: id},
		Payload:    payload,
		Basis:      &eventstore.Basis{Type: "rule", ID: ruleKey},
		DefVersion: e.defVersion,
		SchemaV:    1,
	}); err != nil {
		log.Printf("engine: CRITICAL could not journal automation.failed rule=%s entity=%s record=%s action=%d attempt=%d cause=%v: %v",
			ruleKey, entity, id, actionIndex, attempt, cause, err)
		AutomationJournalFailures.Add(1)
		return
	}
	if attempt > len(retryBackoff) {
		e.abandonAction(ctx, ruleKey, actionKind, actionIndex, entity, id, attempt)
		return
	}
	e.failedActions[failKey(ruleKey, id, actionIndex)] = &actionFailState{
		RuleKey: ruleKey, Entity: entity, RecordID: id, ActionKind: actionKind, ActionIndex: actionIndex,
		Attempt: attempt, NextRetryAt: e.now().Add(retryBackoff[attempt-1]),
	}
}

// abandonAction journals automation.abandoned and escalates to the node's
// approver role once the retry cap (5 retries, LLD-01 §3) is spent.
func (e *Engine) abandonAction(ctx context.Context, ruleKey, actionKind string, actionIndex int, entity, id string, attempts int) {
	key := failKey(ruleKey, id, actionIndex)
	payload, _ := json.Marshal(automationAbandonedPayload{
		Rule: ruleKey, Entity: entity, RecordID: id, ActionKind: actionKind, ActionIndex: actionIndex, Attempts: attempts,
	})
	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:      autoActor,
		Kind:       eventstore.AutomationAbandoned,
		Subject:    eventstore.Subject{Entity: entity, RecordID: id},
		Payload:    payload,
		Basis:      &eventstore.Basis{Type: "rule", ID: ruleKey},
		DefVersion: e.defVersion,
		SchemaV:    1,
	}); err != nil {
		log.Printf("engine: CRITICAL could not journal automation.abandoned rule=%s entity=%s record=%s action=%d: %v",
			ruleKey, entity, id, actionIndex, err)
		AutomationJournalFailures.Add(1)
	}
	delete(e.failedActions, key)
	if _, err := e.createTask(ctx, autoActor, Task{
		Kind: TaskEscalation, Role: e.defApprover, Entity: entity, RecordID: id, Action: "automation_abandoned",
	}, &eventstore.Basis{Type: "rule", ID: ruleKey}, "abandoned|"+key); err != nil {
		log.Printf("engine: CRITICAL could not create operator task for abandoned automation rule=%s entity=%s record=%s action=%d: %v",
			ruleKey, entity, id, actionIndex, err)
		AutomationJournalFailures.Add(1)
	}
}

// retryFailedActions re-attempts every action whose backoff has elapsed
// (called from Tick, LLD-01 §3). The idempotency key embeds the attempt
// number (<failKey>|a<attempt>) so a retry that lands after a prior attempt
// actually succeeded (the process died before updating this projection, or a
// concurrent path already retried it) dedups via the store's ByIdemKey instead
// of re-executing — createTask returns the original event with a nil error,
// which this treats as success and clears the projection either way.
func (e *Engine) retryFailedActions(ctx context.Context) {
	if len(e.failedActions) == 0 {
		return
	}
	now := e.now()
	keys := make([]string, 0, len(e.failedActions))
	for k := range e.failedActions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		st, ok := e.failedActions[key]
		if !ok || now.Before(st.NextRetryAt) {
			continue
		}
		rule := e.findAutomationRule(st.RuleKey)
		if rule == nil || st.ActionIndex < 0 || st.ActionIndex >= len(rule.Actions) {
			// the model changed under the rule (or it no longer exists): there
			// is nothing left to retry — abandon defensively rather than retry
			// forever against a rule that vanished.
			e.abandonAction(ctx, st.RuleKey, st.ActionKind, st.ActionIndex, st.Entity, st.RecordID, st.Attempt)
			continue
		}
		a := rule.Actions[st.ActionIndex]
		nextAttempt := st.Attempt + 1
		idem := fmt.Sprintf("%s|a%d", key, nextAttempt)
		basis := &eventstore.Basis{Type: "rule", ID: st.RuleKey}
		// recompute the eval context for create/update/emit retries from the
		// record's current values (the original trigger/payload context is not
		// persisted in the retry projection — see agent/kalita/QUESTIONS-LLD.md).
		var vals map[string]any
		if st.Entity != "" && st.RecordID != "" {
			if decl := e.model.Entities[st.Entity]; decl != nil {
				if rec, ok := e.records[st.Entity][st.RecordID]; ok {
					vals = e.withComputed(decl, st.RecordID, rec.Values)
				}
			}
		}
		if err := e.execAction(ctx, a, st.Entity, st.RecordID, basis, idem, vals); err != nil {
			e.recordActionFailure(ctx, st.RuleKey, st.ActionKind, st.ActionIndex, st.Entity, st.RecordID, nextAttempt, err)
			continue
		}
		delete(e.failedActions, key) // success clears the retry state
	}
}

// findAutomationRule resolves a "file:line" rule key back to the live
// dsl.AutomationRule — used to re-fire a specific action on retry.
func (e *Engine) findAutomationRule(ruleKey string) *dsl.AutomationRule {
	for _, rule := range e.model.Automations {
		if fmt.Sprintf("%s:%d", rule.File, rule.Line) == ruleKey {
			return rule
		}
	}
	return nil
}

// scheduleInterval parses an interval schedule ("every 5m", "every 2h") into a
// duration. Wall-clock forms ("daily at 09:00") don't match. Delegates the
// form check to dsl.ParseSchedule — the one parser shared with the compiler
// (internal/dsl/schedule.go) — and only turns the accepted text into an
// actual time.Duration here.
func scheduleInterval(s string) (time.Duration, bool) {
	form, ok := dsl.ParseSchedule(s)
	if !ok || form.Kind != dsl.ScheduleEvery {
		return 0, false
	}
	d, err := parseDuration(form.Interval)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}

// scheduleBucket decides, for one Tick, whether a schedule rule fires and
// under what idempotency bucket — honoring the actual wall-clock time of day
// for "daily at HH:MM" (TECHDEBT "Ядро: automation / schedule" P1) instead of
// firing on the first Tick of the day regardless of the clock. now is the
// engine clock (already e.now().UTC()); day is now formatted "2006-01-02".
//
//   - "every <dur>": always fires; bucket changes once per interval window.
//   - "daily" (no time) or an unparseable schedule (a journal/model compiled
//     by a binary older than E059_BAD_SCHEDULE_FORM validation): fires on the
//     first Tick of the day — the exact v0 behavior, preserved as the
//     defensive fallback.
//   - "daily at HH:MM": fires only once the clock's time-of-day is at or
//     after HH:MM; before that, the whole rule is skipped for this Tick (the
//     next Tick within the same day re-checks, so it still fires the same
//     day once the threshold passes — it does not wait for tomorrow).
//
// Either way the bucket is "day" for wall-clock forms, so once fired, the
// per-bucket idempotency key (see fireActions' idemPrefix) keeps every later
// Tick that same day from re-firing.
//
// dedupOpen tells the caller whether it should ALSO skip firing while the
// previous firing's task is still open (hasOpenRuleTask) — true only for
// "every <dur>", the form TECHDEBT's P1 #2 names as the actual growth
// problem (a 2m/5m interval piles up fast if nothing drains it). A "daily"
// rule is already bounded to at most one firing per day by its bucket, so it
// deliberately does NOT get this extra gate: a daily rule must still fire
// tomorrow even if yesterday's task is still sitting open — gating it too
// would mean one slow/forgotten daily task silently cancels every future day
// forever, trading a slow-growth problem for a worse silent-stop one.
func scheduleBucket(schedule string, now time.Time, day string) (bucket string, fire, dedupOpen bool) {
	form, ok := dsl.ParseSchedule(schedule)
	if !ok {
		return day, true, false
	}
	switch form.Kind {
	case dsl.ScheduleEvery:
		d, err := parseDuration(form.Interval)
		if err != nil || d <= 0 {
			return day, true, false // matches the pre-fix fallback for an internally inconsistent form
		}
		return "iv" + strconv.FormatInt(now.Unix()/int64(d/time.Second), 10), true, true
	case dsl.ScheduleDaily:
		nowMinutes := now.Hour()*60 + now.Minute()
		wantMinutes := form.Hour*60 + form.Min
		return day, nowMinutes >= wantMinutes, false
	default:
		return day, true, false
	}
}

// hasOpenRuleTask reports whether an automation rule already has a task in
// flight (open or taken) for this ruleKey (+ entity/record when the rule is
// record-bound) — the dedup that keeps `on schedule every Nm` from piling up
// a fresh task every bucket forever while the previous firing's task is
// still being worked (TECHDEBT "Ядро: automation / schedule" P1). ruleKey is
// the "file:line" identity fireActions stamps on every task it creates via
// ruleBasis (Basis.Type=="rule"); entity/id=="" matches a rule-scoped
// (non-record) firing. A rule whose actions never create a Task (only
// create/update/emit) never has anything to find here, so this is a no-op
// for it — unaffected by this dedup, exactly as before.
func (e *Engine) hasOpenRuleTask(ruleKey, entity, id string) bool {
	for _, t := range e.tasks {
		if t.ruleKey == ruleKey && t.Entity == entity && t.RecordID == id &&
			(t.Status == TaskOpen || t.Status == TaskTaken) {
			return true
		}
	}
	return false
}

// noteScheduleTaskFinished records a just-finished (completed/failed) task
// against its schedule-rule lineage and prunes the oldest entries past
// e.scheduleTaskRetention from the in-memory projection (TECHDEBT "Ядро:
// automation / schedule" P1) — the journal is untouched (append-only); this
// only bounds e.tasks. Called from both the live path (finishTask) and
// replay (applyEvent's TaskCompleted/TaskFailed), so the eviction is a pure
// function of fold ORDER — the sequence of finish-events as the journal
// presents them — never of wall-clock time: replaying the same journal
// prunes the exact same task IDs in the exact same order (I-5 determinism: offline replay).
// A no-op for any task whose ruleKey doesn't resolve to a live `schedule`
// rule (workflow-assignee tasks, escalations, or a rule the model no longer
// has) — retention is scoped to the schedule-growth problem, not a general
// task GC.
func (e *Engine) noteScheduleTaskFinished(t *Task) {
	if t.ruleKey == "" || e.scheduleTaskRetention <= 0 {
		return
	}
	rule := e.findAutomationRule(t.ruleKey)
	if rule == nil || rule.Trigger != "schedule" {
		return
	}
	lineage := t.ruleKey + "|" + t.Entity + "|" + t.RecordID
	hist := append(e.scheduleTaskHistory[lineage], t.ID)
	if over := len(hist) - e.scheduleTaskRetention; over > 0 {
		for _, id := range hist[:over] {
			delete(e.tasks, id)
		}
		hist = hist[over:]
	}
	e.scheduleTaskHistory[lineage] = hist
}

// ParseDuration is the exported form of parseDuration (10d / 48h / 30m) for
// callers outside the package — `kalita report --since`/`egress-report
// --since` (LLD-09 §2/§3) reuse the exact same closed unit set the DSL's
// `for <dur>` stuck-clause already parses, instead of inventing a second
// duration grammar.
func ParseDuration(s string) (time.Duration, error) { return parseDuration(s) }

// parseDuration: 10d / 48h / 30m (closed list, days are the unit that matters).
func parseDuration(s string) (time.Duration, error) {
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	unit := s[len(s)-1]
	n, err := strconv.Atoi(strings.TrimSuffix(s, string(unit)))
	if err != nil {
		return 0, err
	}
	switch unit {
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'm':
		return time.Duration(n) * time.Minute, nil
	}
	return 0, fmt.Errorf("unknown duration unit %c", unit)
}
