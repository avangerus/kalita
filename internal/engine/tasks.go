package engine

import (
	"context"
	"encoding/json"
	"fmt"
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
		cp := t
		e.tasks[t.ID] = &cp
	}
	return t.ID, nil
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

// TakeTask grants an exclusive lease to the actor.
func (e *Engine) TakeTask(ctx context.Context, actor eventstore.Actor, taskID string) (*Task, error) {
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
		Basis: &eventstore.Basis{Type: "task", ID: taskID},
	}); err != nil {
		return nil, err
	}
	t.Status, t.TakenBy, t.LeaseUntil = TaskTaken, actor.ID, lease
	return t, nil
}

// ReportProgress attaches a note, cross-checked against the journal: the
// payload records how many events this actor actually produced on the task's
// record since taking it. A glowing report with zero facts is visible as such.
func (e *Engine) ReportProgress(ctx context.Context, actor eventstore.Actor, taskID, note string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, ok := e.tasks[taskID]
	if !ok || t.TakenBy != actor.ID || t.Status != TaskTaken {
		return &Err{Code: CodeConflict, Message: "task is not taken by this actor"}
	}
	facts := e.countActorEvents(ctx, actor.ID, t.Entity, t.RecordID)
	payload, _ := json.Marshal(taskPayload{Note: note, Facts: facts})
	_, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor: actor, Kind: eventstore.TaskProgress,
		Subject: eventstore.Subject{TaskID: taskID},
		Payload: payload, DefVersion: e.defVersion,
		Basis: &eventstore.Basis{Type: "task", ID: taskID},
	})
	return err
}

// CompleteTask finishes a task with a result.
func (e *Engine) CompleteTask(ctx context.Context, actor eventstore.Actor, taskID, result string) error {
	return e.finishTask(ctx, actor, taskID, eventstore.TaskCompleted, TaskCompleted, result)
}

// FailTask is the honest way out: cheaper for the record than a silent hang.
func (e *Engine) FailTask(ctx context.Context, actor eventstore.Actor, taskID, reason string) error {
	return e.finishTask(ctx, actor, taskID, eventstore.TaskFailed, TaskFailed, reason)
}

func (e *Engine) finishTask(ctx context.Context, actor eventstore.Actor, taskID string, kind eventstore.Kind, status TaskStatus, text string) error {
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
		Basis: &eventstore.Basis{Type: "task", ID: taskID},
	}); err != nil {
		return err
	}
	t.Status = status
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
		_, _ = e.createTask(ctx, autoActor, Task{
			Kind: TaskWorkflow, Role: tr.AssigneeRole, Entity: entity, RecordID: id, Action: tr.Action,
		}, &eventstore.Basis{Type: "rule", ID: "workflow-assignee"},
			fmt.Sprintf("wftask|%s|%s|%s|%s", entity, id, tr.Action, since))
	}
}
