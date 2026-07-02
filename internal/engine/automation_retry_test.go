package engine

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// lastEventOfKind returns the most recent journal event of the given kind for
// the given record, or nil. The fixture's own schedule/stuck rules also run
// on every Tick in these tests, so assertions must not assume the failure
// path produced literally the last event in the whole journal.
func lastEventOfKind(t *testing.T, e *Engine, kind eventstore.Kind, entity, recordID string) *eventstore.Event {
	t.Helper()
	events, err := e.store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var last *eventstore.Event
	for _, ev := range events {
		if ev.Kind == kind && ev.Subject.Entity == entity && ev.Subject.RecordID == recordID {
			last = ev
		}
	}
	return last
}

// TestCascadeFailureIsJournaled is acceptance test 1 (LLD-01 §7): a fireActions
// action whose target does not resolve — LLD-01 §7's "notify на несуществующую
// роль" — is journaled as automation.failed rather than a task nobody could
// ever pick up; after the retry cap it is journaled as automation.abandoned
// and escalated to the node's approver role. Zero silent loss.
//
// The current DSL grammar validates escalate_to/agent role references at
// compile time (checkRole in internal/dsl/semantic_blocks.go) and notify
// carries no role reference at all yet (parser_blocks.go: notify's Args is a
// free-form expression, no a.Role). So a *compiling* pack cannot reach this as
// a runtime failure for escalate/agent, and today's notify grammar has no role
// to be missing in the first place. This test constructs the AutomationRule
// directly (white-box, same package) with a notify action carrying a Role that
// is not declared in the model — exactly LLD-01 §7 test 1's scenario — and
// drives it through the real fireActions/execAction/Tick machinery. See
// agent/kalita/QUESTIONS-LLD.md for the full reasoning.
func TestCascadeFailureIsJournaled(t *testing.T) {
	e := automationEngine(t)
	contract, err := e.Create(ctx, anna, "Contract",
		map[string]any{"company": "Ghost Corp", "due_date": "2026-06-01"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}

	// Two actions: the broken one (index 0) plus a healthy notify — a rule
	// with >=2 actions is a multi-event cascade, so it is op-bracketed
	// (LLD-01 §2); a single-action rule deliberately is not.
	rule := &dsl.AutomationRule{File: "test", Line: 1, Actions: []dsl.AutomationAction{
		{Kind: "notify", Role: "Ghost", Args: `email(manager)`},
		{Kind: "notify", Args: `email(ok)`},
	}}
	ruleKey := "test:1"
	// retryFailedActions re-resolves the rule from the live model on every
	// tick (LLD-01 §3's retry follows the CURRENT ruleset, not a snapshot) —
	// register it there, exactly as dsl.Compile would for a real pack.
	e.model.Automations = append(e.model.Automations, rule)

	// attempt 1: fired directly, as runEventTriggers/Tick would.
	e.fireActions(ctx, rule, "Contract", contract.ID, "")

	failed := lastEventOfKind(t, e.Engine, eventstore.AutomationFailed, "Contract", contract.ID)
	if failed == nil {
		t.Fatal("automation.failed must be journaled on a cascade failure")
	}
	var fp automationFailedPayload
	if err := json.Unmarshal(failed.Payload, &fp); err != nil {
		t.Fatal(err)
	}
	if fp.Rule != ruleKey || fp.ActionKind != "notify" || fp.ActionIndex != 0 || fp.Attempt != 1 {
		t.Fatalf("unexpected automation.failed payload: %+v", fp)
	}
	if fp.Error == "" {
		t.Fatal("automation.failed must carry the error text")
	}

	key := failKey(ruleKey, contract.ID, 0)
	if _, ok := e.failedActions[key]; !ok {
		t.Fatal("retry projection must hold the failed action after attempt 1")
	}

	// op-brackets: a two-action cascade is bracketed (LLD-01 §2).
	begins := countEventsOfKind(t, e.Engine, eventstore.OpBegin)
	ends := countEventsOfKind(t, e.Engine, eventstore.OpEnd)
	if begins == 0 || begins != ends {
		t.Fatalf("fireActions cascade must open and close its op-bracket: begins=%d ends=%d", begins, ends)
	}

	// drive attempts 2..6 through Tick's backoff-driven retry (LLD-01 §3:
	// 1m,2m,4m,8m,16m; cap 5 retries -> abandoned on the 6th total attempt).
	backoffs := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute}
	for i, d := range backoffs {
		attempt := i + 2
		*e.clock = e.clock.Add(d)
		if err := e.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		if attempt < 6 {
			f := lastEventOfKind(t, e.Engine, eventstore.AutomationFailed, "Contract", contract.ID)
			var p automationFailedPayload
			_ = json.Unmarshal(f.Payload, &p)
			if p.Attempt != attempt {
				t.Fatalf("after retry %d want automation.failed attempt=%d, got %+v", i+1, attempt, p)
			}
			if _, ok := e.failedActions[key]; !ok {
				t.Fatalf("retry projection must still hold the key before the cap (attempt %d)", attempt)
			}
		}
	}

	// the 6th attempt (5th retry) exhausts the cap: abandoned + operator task.
	abandoned := lastEventOfKind(t, e.Engine, eventstore.AutomationAbandoned, "Contract", contract.ID)
	if abandoned == nil {
		t.Fatal("automation.abandoned must be journaled once retries are exhausted")
	}
	var ap automationAbandonedPayload
	if err := json.Unmarshal(abandoned.Payload, &ap); err != nil {
		t.Fatal(err)
	}
	if ap.Attempts != 6 {
		t.Fatalf("want abandoned at attempts=6, got %d", ap.Attempts)
	}
	if _, ok := e.failedActions[key]; ok {
		t.Fatal("retry projection must be cleared once abandoned")
	}

	ops := e.Tasks(e.defApprover, TaskOpen)
	var escalated *Task
	for _, tk := range ops {
		if tk.Kind == TaskEscalation && tk.RecordID == contract.ID && tk.Action == "automation_abandoned" {
			escalated = tk
		}
	}
	if escalated == nil {
		t.Fatalf("abandoned automation must escalate to the approver role %s, got tasks: %+v", e.defApprover, ops)
	}

	// no further retries after abandonment: one more tick must not re-fire it.
	priorFailedCount := countEventsOfKind(t, e.Engine, eventstore.AutomationFailed)
	*e.clock = e.clock.Add(time.Hour)
	if err := e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got := countEventsOfKind(t, e.Engine, eventstore.AutomationFailed); got != priorFailedCount {
		t.Fatalf("abandoned action must not retry again: automation.failed count %d -> %d", priorFailedCount, got)
	}
}

func countEventsOfKind(t *testing.T, e *Engine, kind eventstore.Kind) int {
	t.Helper()
	events, err := e.store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ev := range events {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}
