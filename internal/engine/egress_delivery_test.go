package engine

import (
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/eventstore"
)

// D-011 (English-first release) — the durable egress delivery surface at the
// engine layer: the per-firing DeliveryID, webhook.attempt receipts, the DLQ
// listing + manual recovery, and the metrics projection. The runner (cmd/
// kalita) drives these; the engine only folds journal truth.

// TestWebhookDeliveryIdentityPersistsAcrossReplay: a webhook task is born with
// a per-firing DeliveryID (uuid v7) and a CreatedAt, and BOTH survive a
// journal replay — the idempotency identity is durable, not in-memory.
func TestWebhookDeliveryIdentityPersistsAcrossReplay(t *testing.T) {
	e, store := webhookEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(ctx, owner, "Doc", map[string]any{"title": "t"}, basis, ""); err != nil {
		t.Fatal(err)
	}
	tasks := e.OpenWebhookTasks()
	if len(tasks) != 1 {
		t.Fatalf("want one open webhook task, got %d", len(tasks))
	}
	first := tasks[0]
	if first.DeliveryID == "" {
		t.Fatal("a hardened webhook task must carry a DeliveryID")
	}
	if first.CreatedAt.IsZero() {
		t.Fatal("the task must carry its journaled CreatedAt")
	}

	// receipts recorded before the restart must also fold back from the journal
	if err := e.RecordWebhookAttempt(ctx, first.ID, 1, "retrying", "network", "example.com", 12, "att-1"); err != nil {
		t.Fatal(err)
	}

	e2, err := New(ctx, e.Model(), store)
	if err != nil {
		t.Fatal(err)
	}
	again := e2.OpenWebhookTasks()
	if len(again) != 1 || again[0].DeliveryID != first.DeliveryID {
		t.Fatalf("DeliveryID must survive replay: got %+v want %q", again, first.DeliveryID)
	}
	if again[0].CreatedAt.Equal(first.CreatedAt) == false {
		t.Fatalf("CreatedAt must survive replay: %v vs %v", again[0].CreatedAt, first.CreatedAt)
	}
	receipts := e2.WebhookReceipts()
	if len(receipts) != 1 || receipts[0].TaskID != first.ID || receipts[0].Outcome != "retrying" {
		t.Fatalf("receipts must survive replay: %+v", receipts)
	}
}

// TestRecordWebhookAttemptIdempotent: the same attempt key journals exactly
// ONE receipt (a crash between send and settle re-runs the runner; the receipt
// must not double-count).
func TestRecordWebhookAttemptIdempotent(t *testing.T) {
	e, _ := webhookEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(ctx, owner, "Doc", map[string]any{"title": "t"}, basis, ""); err != nil {
		t.Fatal(err)
	}
	task := e.OpenWebhookTasks()[0]
	if err := e.RecordWebhookAttempt(ctx, task.ID, 1, "delivered", "", "example.com", 7, "same-key"); err != nil {
		t.Fatal(err)
	}
	if err := e.RecordWebhookAttempt(ctx, task.ID, 1, "delivered", "", "example.com", 7, "same-key"); err != nil {
		t.Fatal(err)
	}
	if got := e.WebhookReceipts(); len(got) != 1 {
		t.Fatalf("one idempotency key must fold exactly one receipt, got %d", len(got))
	}
}

// TestRetryWebhookTask: DLQ recovery is approver-gated, copies the SAME
// DeliveryID forward, refuses double-open deliveries and pre-hardening tasks.
func TestRetryWebhookTask(t *testing.T) {
	e, _ := webhookEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(ctx, owner, "Doc", map[string]any{"title": "t"}, basis, ""); err != nil {
		t.Fatal(err)
	}
	failed := e.OpenWebhookTasks()[0]
	if err := e.SettleWebhookTask(ctx, failed.ID, false, "delivery failed", "settle-1"); err != nil {
		t.Fatal(err)
	}

	// a non-approver human is refused
	viewer := eventstore.Actor{Type: eventstore.ActorHuman, ID: "v", Role: "Viewer"}
	if _, err := e.RetryWebhookTask(ctx, viewer, failed.ID, basis); err == nil {
		t.Fatal("a non-approver must not retry deliveries")
	}
	// a non-human actor is refused even with the role name
	bot := eventstore.Actor{Type: eventstore.ActorAgent, ID: "bot", Role: "Owner"}
	if _, err := e.RetryWebhookTask(ctx, bot, failed.ID, basis); err == nil {
		t.Fatal("an agent must not retry deliveries")
	}

	// the approver's retry creates a NEW open task with the SAME identity
	retried, err := e.RetryWebhookTask(ctx, owner, failed.ID, basis)
	if err != nil {
		t.Fatal(err)
	}
	if retried.ID == failed.ID {
		t.Fatal("the retry must be a new task, not the settled one")
	}
	if retried.DeliveryID != failed.DeliveryID {
		t.Fatalf("retry must carry the same DeliveryID: %q vs %q", retried.DeliveryID, failed.DeliveryID)
	}
	if retried.Status != TaskOpen || retried.Args != failed.Args || retried.RecordID != failed.RecordID {
		t.Fatalf("retry task must mirror the dead-lettered intent: %+v", retried)
	}
	if got := e.OpenWebhookTasks(); len(got) != 1 || got[0].ID != retried.ID {
		t.Fatalf("the retry must be the one open webhook task, got %+v", got)
	}

	// while it is open, another retry of the same identity is refused
	if _, err := e.RetryWebhookTask(ctx, owner, failed.ID, basis); err == nil {
		t.Fatal("a second retry while one is open must conflict")
	}

	// a COMPLETED webhook task is not retryable either
	done := retried
	if err := e.SettleWebhookTask(ctx, done.ID, true, "delivered", "settle-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RetryWebhookTask(ctx, owner, done.ID, basis); err == nil {
		t.Fatal("retrying a completed delivery must conflict")
	}

	// a pre-hardening task without a DeliveryID is refused with guidance
	e.mu.Lock()
	done.DeliveryID = ""
	e.tasks[done.ID] = done
	e.mu.Unlock()
	if _, err := e.RetryWebhookTask(ctx, owner, done.ID, basis); err == nil {
		t.Fatal("a delivery without a DeliveryID must not be blindly retried")
	}
}

// TestEgressMetrics: the observability projection counts queued/delivered/
// dead-lettered deliveries, attempts, retries, latency and the oldest pending.
func TestEgressMetrics(t *testing.T) {
	e, _ := webhookEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}

	// three deliveries: one delivered, one dead-lettered, one still queued
	mk := func() *Task {
		t.Helper()
		if _, err := e.Create(ctx, owner, "Doc", map[string]any{"title": "t"}, basis, ""); err != nil {
			t.Fatal(err)
		}
		open := e.OpenWebhookTasks()
		if len(open) != 1 {
			t.Fatalf("want one open webhook task, got %d", len(open))
		}
		return open[0]
	}

	delivered := mk()
	_ = e.RecordWebhookAttempt(ctx, delivered.ID, 1, "delivered", "", "example.com", 50, "r1")
	if err := e.SettleWebhookTask(ctx, delivered.ID, true, "delivered", "s1"); err != nil {
		t.Fatal(err)
	}

	dead := mk()
	_ = e.RecordWebhookAttempt(ctx, dead.ID, 1, "retrying", "network", "example.com", 10, "r2a")
	_ = e.RecordWebhookAttempt(ctx, dead.ID, 2, "failed", "retry_exhausted", "example.com", 40, "r2b")
	if err := e.SettleWebhookTask(ctx, dead.ID, false, "delivery failed", "s2"); err != nil {
		t.Fatal(err)
	}

	queued := mk()

	m := e.EgressMetrics(time.Now())
	if m.Queued != 1 || m.Delivered != 1 || m.DeadLettered != 1 {
		t.Fatalf("queued/delivered/dead_lettered = %d/%d/%d, want 1/1/1", m.Queued, m.Delivered, m.DeadLettered)
	}
	if m.Attempts != 3 || m.Retries != 1 {
		t.Fatalf("attempts/retries = %d/%d, want 3/1", m.Attempts, m.Retries)
	}
	// latency folds over DELIVERED receipts only (the one success here, 50ms)
	if m.LatencySumMS != 50 || m.LatencyCount != 1 {
		t.Fatalf("latency sum/count = %d/%d, want 50/1", m.LatencySumMS, m.LatencyCount)
	}
	if m.OldestPendingSec < 0 {
		t.Fatalf("oldest pending must not be negative, got %f", m.OldestPendingSec)
	}
	// the queued task is the pending one — its age must be near zero (created
	// moments ago), proving the oldest-pending clock tracks real queue age
	if m.OldestPendingSec > 60 {
		t.Fatalf("oldest pending looks wrong: %fs for a just-created task", m.OldestPendingSec)
	}
	_ = queued
}
