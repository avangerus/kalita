package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Task subsystem: the work queue agents (and humans) live on. Tasks come from
// workflow assignees, automation rules and escalations. Lease semantics: take
// grants an exclusive TTL lease; an expired lease returns the task to the pool
// (a silently hung agent loses the task, it does not block the line).

type TaskKind string

const (
	TaskWorkflow     TaskKind = "workflow"     // perform a transition action
	TaskAgent        TaskKind = "agent"        // automation `agent Role: task(args)`
	TaskEscalation   TaskKind = "escalation"   // escalate_to Role
	TaskNotification TaskKind = "notification" // notify ...
	TaskWebhook      TaskKind = "webhook"      // declared outgoing webhook (node executes)
)

type TaskStatus string

const (
	TaskOpen      TaskStatus = "open"
	TaskTaken     TaskStatus = "taken"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskExpired   TaskStatus = "expired"
)

type Task struct {
	ID         string     `json:"id"`
	Kind       TaskKind   `json:"kind"`
	Role       string     `json:"role"`
	Entity     string     `json:"entity,omitempty"`
	RecordID   string     `json:"record_id,omitempty"`
	Action     string     `json:"action,omitempty"` // transition action or agent task name
	Args       string     `json:"args,omitempty"`
	Status     TaskStatus `json:"status"`
	TakenBy    string     `json:"taken_by,omitempty"`
	LeaseUntil time.Time  `json:"lease_until,omitempty"`
	Note       string     `json:"note,omitempty"`    // last progress note from the agent
	NoteAt     *time.Time `json:"note_at,omitempty"` // timestamp of that note
	// ruleKey is the "file:line" identity of the automation rule that created
	// this task (from the event's Basis, Type=="rule" — see fireActions'
	// ruleBasis), empty for tasks with no rule provenance (e.g. a workflow
	// assignee task, whose Basis.ID is "workflow-assignee" instead — also
	// carried here, just never matched by an automation rule lookup). Folded
	// straight from ev.Basis on both the live path (createTask) and replay
	// (applyEvent's TaskCreated), so it is a pure, deterministic function of
	// the journal (TECHDEBT "Ядро: automation / schedule" P1 — schedule task
	// dedup/retention). Deliberately unexported: it is an engine-internal
	// bookkeeping detail, not part of the Task API — exporting it would leak
	// into every JSON dump of a task (API responses, the golden fixtures)
	// for a mechanism nothing outside this package needs to see.
	ruleKey string
}

type taskPayload struct {
	Kind     TaskKind `json:"kind,omitempty"`
	Role     string   `json:"role,omitempty"`
	Entity   string   `json:"entity,omitempty"`
	RecordID string   `json:"record_id,omitempty"`
	Action   string   `json:"action,omitempty"`
	Args     string   `json:"args,omitempty"`
	Note     string   `json:"note,omitempty"`
	Facts    int      `json:"facts,omitempty"` // events by this actor on the record since take
	Reason   string   `json:"reason,omitempty"`
	Result   string   `json:"result,omitempty"`
	Lease    string   `json:"lease_until,omitempty"`
}

// createTask appends task.created (idempotent under idemKey) and projects it.
func (e *Engine) createTask(ctx context.Context, actor eventstore.Actor, t Task, basis *eventstore.Basis, idemKey string) (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	t.ID = id.String()
	payload, _ := json.Marshal(taskPayload{Kind: t.Kind, Role: t.Role, Entity: t.Entity,
		RecordID: t.RecordID, Action: t.Action, Args: t.Args})
	ev, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:          actor,
		Kind:           eventstore.TaskCreated,
		Subject:        eventstore.Subject{TaskID: t.ID, Entity: t.Entity, RecordID: t.RecordID},
		Payload:        payload,
		Basis:          basis,
		DefVersion:     e.defVersion,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		return "", err
	}
	// idempotent replay returns the original event with the original task id
	t.ID = ev.Subject.TaskID
	if _, exists := e.tasks[t.ID]; !exists {
		t.Status = TaskOpen
		if basis != nil && basis.Type == "rule" {
			t.ruleKey = basis.ID
		}
		cp := t
		e.tasks[t.ID] = &cp
		e.wakeTaskWaiters()
	}
	return t.ID, nil
}

// WaitForTask blocks until an open task exists for the role or the timeout
// passes — the long-polling primitive: a thousand idle workers cost zero
// instead of storming the node with list calls.
func (e *Engine) WaitForTask(ctx context.Context, role string, timeout time.Duration) []*Task {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		if tasks := e.Tasks(role, TaskOpen); len(tasks) > 0 {
			return tasks
		}
		select {
		case <-ctx.Done():
			return nil
		case <-deadline.C:
			return nil
		case <-e.taskSignal():
		}
	}
}

// taskSignal returns a channel closed on the next task-affecting change.
func (e *Engine) taskSignal() <-chan struct{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.taskWake == nil {
		e.taskWake = make(chan struct{})
	}
	return e.taskWake
}

// wakeTaskWaiters releases all current waiters (called after task creation).
func (e *Engine) wakeTaskWaiters() {
	if e.taskWake != nil {
		close(e.taskWake)
		e.taskWake = nil
	}
}

// Tasks lists tasks for a role (open and taken-by-actor first; deterministic).
func (e *Engine) Tasks(role string, status TaskStatus) []*Task {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var out []*Task
	for _, t := range e.tasks {
		if t.Role == role && (status == "" || t.Status == status) {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// AgentTasks returns every task on an agent role, any status — the data behind
// the operator's agent board: the workforce pipeline (waiting → working → done),
// not scoped to one role the way the per-role pool is.
func (e *Engine) AgentTasks() []*Task {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var out []*Task
	for _, t := range e.tasks {
		if r := e.model.Roles[t.Role]; r != nil && r.IsAgent {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// TakeTask grants an exclusive lease to the actor.
func (e *Engine) TakeTask(ctx context.Context, actor eventstore.Actor, taskID, idemKey string) (*Task, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, ok := e.tasks[taskID]
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: "task " + taskID + " not found"}
	}
	e.expireIfDue(ctx, t)
	if t.Status != TaskOpen {
		return nil, &Err{Code: CodeConflict, Message: "task is " + string(t.Status),
			FixHint: "only open tasks can be taken"}
	}
	if t.Role != actor.Role {
		return nil, &Err{Code: CodePermissionDenied,
			Message: fmt.Sprintf("task belongs to role %s, actor is %s", t.Role, actor.Role)}
	}
	lease := e.now().Add(e.taskTTL)
	payload, _ := json.Marshal(taskPayload{Lease: lease.UTC().Format(time.RFC3339)})
	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor: actor, Kind: eventstore.TaskTaken,
		Subject: eventstore.Subject{TaskID: taskID},
		Payload: payload, DefVersion: e.defVersion,
		Basis:          &eventstore.Basis{Type: "task", ID: taskID},
		IdempotencyKey: idemKey,
	}); err != nil {
		return nil, err
	}
	t.Status, t.TakenBy, t.LeaseUntil = TaskTaken, actor.ID, lease
	return t, nil
}

// ReportProgress attaches a note, cross-checked against the journal: the
// payload records how many events this actor actually produced on the task's
// record since taking it. A glowing report with zero facts is visible as such.
func (e *Engine) ReportProgress(ctx context.Context, actor eventstore.Actor, taskID, note, idemKey string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, ok := e.tasks[taskID]
	if !ok || t.TakenBy != actor.ID || t.Status != TaskTaken {
		return &Err{Code: CodeConflict, Message: "task is not taken by this actor"}
	}
	facts := e.countActorEvents(ctx, actor.ID, t.Entity, t.RecordID)
	payload, _ := json.Marshal(taskPayload{Note: note, Facts: facts})
	ev, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor: actor, Kind: eventstore.TaskProgress,
		Subject: eventstore.Subject{TaskID: taskID},
		Payload: payload, DefVersion: e.defVersion,
		Basis:          &eventstore.Basis{Type: "task", ID: taskID},
		IdempotencyKey: idemKey,
	})
	if err != nil {
		return err
	}
	// live projection update (like finishTask) — applyEvent only covers replay.
	// NoteAt comes from the journaled event's TS, exactly as replay derives it:
	// wall time here would make live and replayed projections disagree (K-BUG-10).
	t.Note, t.NoteAt = note, &ev.TS
	return nil
}

// CompleteTask finishes a task with a result.
func (e *Engine) CompleteTask(ctx context.Context, actor eventstore.Actor, taskID, result, idemKey string) error {
	return e.finishTask(ctx, actor, taskID, eventstore.TaskCompleted, TaskCompleted, result, idemKey)
}

// FailTask is the honest way out: cheaper for the record than a silent hang.
func (e *Engine) FailTask(ctx context.Context, actor eventstore.Actor, taskID, reason, idemKey string) error {
	return e.finishTask(ctx, actor, taskID, eventstore.TaskFailed, TaskFailed, reason, idemKey)
}

// OpenWebhookTasks lists open webhook delivery tasks (Cycle-B1 / D-011).
// Webhook tasks carry no role, so the role-scoped Tasks() does not see them;
// the node's webhook runner drives them directly.
func (e *Engine) OpenWebhookTasks() []*Task {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var out []*Task
	for _, t := range e.tasks {
		if t.Kind == TaskWebhook && t.Status == TaskOpen {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// WebhookRecordValues returns the record's current values for a webhook
// delivery (Cycle-B1 / D-011). It is an INTERNAL read for the node's own
// trusted runner, not an ABAC subject — the webhook task was already created
// by automation under the engine's authority. Sealed PII fields are included
// as their ciphertext (the mock reference endpoint does not unseal them).
func (e *Engine) WebhookRecordValues(entity, recordID string) (map[string]any, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	rec, ok := e.records[entity][recordID]
	if !ok {
		return nil, false
	}
	return rec.Values, true
}

// SettleWebhookTask finishes a webhook delivery attempt (Cycle-B1 / D-011).
// Webhook tasks are never "taken" through the role-scoped pool (they have no
// role), so the ordinary finishTask's taken-by check does not apply: the node
// runner settles an OPEN task directly — completed on success, failed on a
// permanent delivery error. Idempotent under idemKey; settling an already
// settled task is a conflict.
func (e *Engine) SettleWebhookTask(ctx context.Context, taskID string, ok bool, text, idemKey string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, found := e.tasks[taskID]
	if !found {
		return &Err{Code: CodeNotFound, Message: "task " + taskID + " not found"}
	}
	if t.Kind != TaskWebhook {
		return &Err{Code: CodeConflict, Message: "task " + taskID + " is not a webhook task"}
	}
	if t.Status == TaskCompleted || t.Status == TaskFailed {
		return &Err{Code: CodeConflict, Message: "webhook task already " + string(t.Status)}
	}
	kind, status := eventstore.TaskCompleted, TaskCompleted
	if !ok {
		kind, status = eventstore.TaskFailed, TaskFailed
	}
	payload, _ := json.Marshal(taskPayload{Result: text, Reason: text})
	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor: autoActor, Kind: kind,
		Subject: eventstore.Subject{TaskID: taskID},
		Payload: payload, DefVersion: e.defVersion,
		Basis:          &eventstore.Basis{Type: "rule", ID: "webhook-runner"},
		IdempotencyKey: idemKey,
	}); err != nil {
		return err
	}
	t.Status = status
	e.noteScheduleTaskFinished(t)
	return nil
}

func (e *Engine) finishTask(ctx context.Context, actor eventstore.Actor, taskID string, kind eventstore.Kind, status TaskStatus, text, idemKey string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, ok := e.tasks[taskID]
	if !ok {
		return &Err{Code: CodeNotFound, Message: "task " + taskID + " not found"}
	}
	e.expireIfDue(ctx, t)
	if t.Status != TaskTaken || t.TakenBy != actor.ID {
		return &Err{Code: CodeConflict, Message: "task is not taken by this actor",
			FixHint: "take the task first; expired leases return tasks to the pool"}
	}
	payload, _ := json.Marshal(taskPayload{Result: text, Reason: text})
	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor: actor, Kind: kind,
		Subject: eventstore.Subject{TaskID: taskID},
		Payload: payload, DefVersion: e.defVersion,
		Basis:          &eventstore.Basis{Type: "task", ID: taskID},
		IdempotencyKey: idemKey,
	}); err != nil {
		return err
	}
	t.Status = status
	e.noteScheduleTaskFinished(t)
	return nil
}

// expireIfDue lazily expires an overdue lease (also swept in Tick).
func (e *Engine) expireIfDue(ctx context.Context, t *Task) {
	if t.Status != TaskTaken || e.now().Before(t.LeaseUntil) {
		return
	}
	_, _ = e.store.Append(ctx, eventstore.AppendInput{
		Actor: autoActor, Kind: eventstore.TaskExpired,
		Subject:    eventstore.Subject{TaskID: t.ID},
		DefVersion: e.defVersion,
		Basis:      &eventstore.Basis{Type: "rule", ID: "lease-ttl"},
	})
	t.Status, t.TakenBy = TaskOpen, "" // expired lease returns the task to the pool
}

// countActorEvents counts journal events by the actor on a record.
func (e *Engine) countActorEvents(ctx context.Context, actorID, entity, recordID string) int {
	events, err := e.store.All(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, ev := range events {
		if ev.Actor.ID == actorID && ev.Subject.Entity == entity && ev.Subject.RecordID == recordID &&
			(ev.Kind == eventstore.RecordCreated || ev.Kind == eventstore.RecordUpdated || ev.Kind == eventstore.RecordAction) {
			n++
		}
	}
	return n
}

// ensureWorkflowTasks creates tasks for transitions available from the
// record's current state that have an assignee. Idempotent per state entry.
func (e *Engine) ensureWorkflowTasks(ctx context.Context, entity, id string) {
	wf, ok := e.model.Workflows[entity]
	if !ok {
		return
	}
	rec, ok := e.records[entity][id]
	if !ok {
		return
	}
	current, _ := rec.Values[wf.Field].(string)
	since := e.stateSince[entity][id].UTC().Format(time.RFC3339Nano)
	for _, tr := range wf.Transitions {
		if tr.AssigneeRole == "" || tr.Auto || (tr.From != current && tr.From != "any") {
			continue
		}
		if _, err := e.createTask(ctx, autoActor, Task{
			Kind: TaskWorkflow, Role: tr.AssigneeRole, Entity: entity, RecordID: id, Action: tr.Action,
		}, &eventstore.Basis{Type: "rule", ID: "workflow-assignee"},
			fmt.Sprintf("wftask|%s|%s|%s|%s", entity, id, tr.Action, since)); err != nil {
			// A real failure here (idempotent replay never reaches this branch —
			// createTask returns the original id with a nil error for a dup, see
			// the comment above) means the assignee silently never got a task
			// and the workflow would hang unnoticed. Surface it the same way
			// automation.go's cascade/abandon path does: a CRITICAL log line plus
			// the shared journal-failure counter, so an operator or metrics
			// exporter sees it instead of a workflow quietly stalling.
			log.Printf("engine: CRITICAL could not create workflow-assignee task entity=%s record=%s action=%s assignee=%s: %v",
				entity, id, tr.Action, tr.AssigneeRole, err)
			AutomationJournalFailures.Add(1)
		}
	}
}
