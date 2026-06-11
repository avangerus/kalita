package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Automation engine. Event triggers (create/update) fire inline after the
// mutation; schedule and stuck rules fire on Tick, which the node calls
// periodically. v0 honesty: the schedule TEXT (daily at 09:00) is carried but
// not parsed — every Tick evaluates every schedule rule, deduplicated per
// rule+record+day by idempotency keys. Cron parsing is a node concern (v1).

// Tick evaluates schedule and stuck rules and sweeps expired leases.
func (e *Engine) Tick(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	day := e.now().UTC().Format("2006-01-02")
	for _, rule := range e.model.Automations {
		key := fmt.Sprintf("%s:%d", rule.File, rule.Line)
		switch rule.Trigger {
		case "schedule":
			if rule.Entity == "" {
				e.fireActions(ctx, rule, "", "", fmt.Sprintf("auto|%s|%s", key, day))
				continue
			}
			decl := e.model.Entities[rule.Entity]
			for _, id := range sortedIDs(e.records[rule.Entity]) {
				rec := e.records[rule.Entity][id]
				full := e.withComputed(decl, rec.ID, rec.Values)
				if rule.When != "" && !evalWhere(rule.When, e.ctxFor(rec.ID, "", full)) {
					continue
				}
				e.fireActions(ctx, rule, rule.Entity, id, fmt.Sprintf("auto|%s|%s|%s", key, id, day))
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

	for _, t := range e.tasks {
		e.expireIfDue(ctx, t)
	}
	return nil
}

// runEventTriggers fires create/update rules for a record (called inline
// after the mutation, under the engine lock).
func (e *Engine) runEventTriggers(ctx context.Context, trigger, entity, id string) {
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
		if rule.When != "" && !evalWhere(rule.When, e.ctxFor(rec.ID, "", full)) {
			continue
		}
		e.fireActions(ctx, rule, entity, id, "")
	}
}

// fireActions executes a rule's actions. idemPrefix dedups periodic firings.
func (e *Engine) fireActions(ctx context.Context, rule *dsl.AutomationRule, entity, id, idemPrefix string) {
	ruleBasis := &eventstore.Basis{Type: "rule", ID: fmt.Sprintf("%s:%d", rule.File, rule.Line)}
	for i, a := range rule.Actions {
		idem := ""
		if idemPrefix != "" {
			idem = fmt.Sprintf("%s|%d", idemPrefix, i)
		}
		switch a.Kind {
		case "agent":
			_, _ = e.createTask(ctx, autoActor, Task{
				Kind: TaskAgent, Role: a.Role, Entity: entity, RecordID: id, Action: a.Task, Args: a.Args,
			}, ruleBasis, idem)
		case "escalate":
			_, _ = e.createTask(ctx, autoActor, Task{
				Kind: TaskEscalation, Role: a.Role, Entity: entity, RecordID: id, Action: "escalation",
			}, ruleBasis, idem)
		case "notify":
			_, _ = e.createTask(ctx, autoActor, Task{
				Kind: TaskNotification, Entity: entity, RecordID: id, Args: a.Args,
			}, ruleBasis, idem)
		case "webhook":
			// the engine records intent; the node's webhook runner delivers
			_, _ = e.createTask(ctx, autoActor, Task{
				Kind: TaskWebhook, Entity: entity, RecordID: id, Args: a.Args,
			}, ruleBasis, idem)
		}
	}
}

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
