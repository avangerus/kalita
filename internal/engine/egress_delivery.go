package engine

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Durable egress delivery surface (English-first industrial release, D-011
// hardening). The business semantic stays in the DSL (automation `webhook out`
// -> TaskWebhook intent); THIS file is the delivery-runtime half the node
// runs: attempt receipts, the dead-letter listing and the operator's manual
// recovery path, and the metrics projections. No business logic lives here.

// WebhookReceipt is one folded delivery-attempt record (observability only —
// the durable truth is task.created + task.completed/failed; receipts are a
// projection that may be dropped by an older binary without consequence).
type WebhookReceipt struct {
	TaskID      string    `json:"task_id"`
	Attempt     int       `json:"attempt"`
	Outcome     string    `json:"outcome"` // delivered | retrying | failed
	Category    string    `json:"category,omitempty"`
	Destination string    `json:"destination,omitempty"`
	LatencyMS   int64     `json:"latency_ms,omitempty"`
	TS          time.Time `json:"ts"`
}

// webhookAttemptPayload is what a webhook.attempt event carries. No secrets,
// no payload bodies — destination host identity only.
type webhookAttemptPayload struct {
	TaskID      string `json:"task_id,omitempty"`
	Attempt     int    `json:"attempt"`
	Outcome     string `json:"outcome"`
	Category    string `json:"category,omitempty"`
	Destination string `json:"destination,omitempty"`
	LatencyMS   int64  `json:"latency_ms,omitempty"`
}

// RecordWebhookAttempt journals one delivery-attempt receipt (idempotent under
// idemKey). Purely additive observability: the attempt itself neither moves
// the task nor drives any business effect.
func (e *Engine) RecordWebhookAttempt(ctx context.Context, taskID string, attempt int, outcome, category, destination string, latencyMS int64, idemKey string) error {
	payload, _ := json.Marshal(webhookAttemptPayload{
		TaskID: taskID, Attempt: attempt, Outcome: outcome,
		Category: category, Destination: destination, LatencyMS: latencyMS,
	})
	ev, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:          autoActor,
		Kind:           eventstore.WebhookAttempt,
		Subject:        eventstore.Subject{TaskID: taskID},
		Payload:        payload,
		Basis:          &eventstore.Basis{Type: "rule", ID: "webhook-runner"},
		DefVersion:     e.defVersion,
		SchemaV:        eventstore.CurrentSchemaV,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.webhookReceiptIDs[ev.EventID] {
		return nil // duplicate idempotency key: already folded
	}
	e.webhookReceiptIDs[ev.EventID] = true
	e.webhookReceipts = append(e.webhookReceipts, WebhookReceipt{
		TaskID: taskID, Attempt: attempt, Outcome: outcome, Category: category,
		Destination: destination, LatencyMS: latencyMS, TS: ev.TS,
	})
	return nil
}

// WebhookReceipts returns the folded delivery-attempt receipts (observability).
func (e *Engine) WebhookReceipts() []WebhookReceipt {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]WebhookReceipt, len(e.webhookReceipts))
	copy(out, e.webhookReceipts)
	return out
}

// FailedWebhookTasks lists unresolved dead-lettered webhook deliveries — the
// operator's DLQ view. A failed task whose delivery identity was retried (a
// newer task with the same DeliveryID exists, whatever its outcome) is
// superseded and no longer listed: the DLQ shows what still needs a decision.
// Newest-first by task id (uuid v7 = time order).
func (e *Engine) FailedWebhookTasks() []*Task {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var out []*Task
	for _, t := range e.tasks {
		if t.Kind == TaskWebhook && t.Status == TaskFailed && !e.webhookSupersededLocked(t) {
			cp := *t
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// webhookSupersededLocked reports whether a newer webhook task carries the
// same delivery identity (a retry of t). Callers hold e.mu.
func (e *Engine) webhookSupersededLocked(t *Task) bool {
	if t.DeliveryID == "" {
		return false
	}
	for _, o := range e.tasks {
		if o.Kind == TaskWebhook && o.DeliveryID == t.DeliveryID && o.ID > t.ID {
			return true
		}
	}
	return false
}

// RetryWebhookTask is the operator's manual recovery: it creates a NEW open
// delivery task for a dead-lettered webhook, carrying the SAME per-firing
// DeliveryID forward — so redelivery carries the same idempotency key and a
// correct receiver collapses it to one business effect. Only a human with the
// definition approver role may retry (privileged operational action, the API
// gates this too). Refuses when the target is not a failed webhook task or a
// delivery with the same identity is already open.
func (e *Engine) RetryWebhookTask(ctx context.Context, actor eventstore.Actor, taskID string, basis *eventstore.Basis) (*Task, error) {
	if actor.Type != eventstore.ActorHuman || actor.Role != e.defApprover {
		return nil, &Err{Code: CodePermissionDenied,
			Message: "webhook retries are reserved to the definition approver role",
			Rule:    "definition approver"}
	}
	e.mu.Lock()
	t, ok := e.tasks[taskID]
	if !ok || t.Kind != TaskWebhook {
		e.mu.Unlock()
		return nil, &Err{Code: CodeNotFound, Message: "webhook task " + taskID + " not found"}
	}
	if t.Status != TaskFailed {
		e.mu.Unlock()
		return nil, &Err{Code: CodeConflict, Message: "only a failed (dead-lettered) webhook delivery can be retried"}
	}
	if t.DeliveryID == "" {
		e.mu.Unlock()
		return nil, &Err{Code: CodeValidation,
			Message: "this webhook task has no delivery identity (pre-hardening journal)",
			FixHint: "re-trigger the business effect instead of retrying the delivery",
		}
	}
	if e.webhookSupersededLocked(t) {
		e.mu.Unlock()
		return nil, &Err{Code: CodeConflict,
			Message: "this delivery was already retried",
			FixHint: "retry the newest dead-lettered task of this delivery identity (see the DLQ listing)"}
	}
	for _, other := range e.tasks {
		if other.Kind == TaskWebhook && other.Status == TaskOpen && other.DeliveryID == t.DeliveryID {
			e.mu.Unlock()
			return nil, &Err{Code: CodeConflict,
				Message: "a delivery with the same identity is already open",
				FixHint: "wait for the open delivery to settle"}
		}
	}
	entity, recordID, args, deliveryID := t.Entity, t.RecordID, t.Args, t.DeliveryID
	e.mu.Unlock()

	retryID := uuid.New().String()
	id, err := e.createTask(ctx, actor, Task{
		Kind: TaskWebhook, Entity: entity, RecordID: recordID, Args: args,
		DeliveryID: deliveryID,
	}, basis, "wh-retry:"+taskID+":"+retryID)
	if err != nil {
		return nil, err
	}
	e.mu.RLock()
	cp := *e.tasks[id]
	e.mu.RUnlock()
	return &cp, nil
}

// EgressMetrics is the projection the /metrics endpoint renders (D-011
// observability). All values derive from folded journal state — replay-
// deterministic, no runner-side mutable counters required.
type EgressMetrics struct {
	Queued           int     `json:"queued"`             // open webhook deliveries
	DeadLettered     int     `json:"dead_lettered"`      // failed deliveries not yet retried
	Delivered        int     `json:"delivered"`          // completed deliveries
	Attempts         int     `json:"attempts"`           // folded webhook.attempt receipts
	Retries          int     `json:"retries"`            // receipts with attempt > 1
	OldestPendingSec float64 `json:"oldest_pending_sec"` // seconds since the oldest open delivery's creation
	LatencySumMS     int64   `json:"latency_sum_ms"`     // sum over delivered receipts
	LatencyCount     int64   `json:"latency_count"`      // count of delivered receipts
}

// EgressMetrics computes the delivery metrics from projections.
func (e *Engine) EgressMetrics(now time.Time) EgressMetrics {
	e.mu.RLock()
	defer e.mu.RUnlock()
	m := EgressMetrics{}
	var oldest time.Time
	for _, t := range e.tasks {
		if t.Kind != TaskWebhook {
			continue
		}
		switch t.Status {
		case TaskOpen:
			m.Queued++
			if oldest.IsZero() || t.CreatedAt.Before(oldest) {
				oldest = t.CreatedAt
			}
		case TaskCompleted:
			m.Delivered++
		case TaskFailed:
			if !e.webhookSupersededLocked(t) {
				m.DeadLettered++
			}
		}
	}
	for _, r := range e.webhookReceipts {
		m.Attempts++
		if r.Attempt > 1 {
			m.Retries++
		}
		if r.Outcome == "delivered" {
			m.LatencySumMS += r.LatencyMS
			m.LatencyCount++
		}
	}
	if !oldest.IsZero() {
		m.OldestPendingSec = now.Sub(oldest).Seconds()
	}
	return m
}
