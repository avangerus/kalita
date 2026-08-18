package api

import (
	"net/http"
	"time"

	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
)

// The dead-letter queue for webhook deliveries (D-011 hardening, English-first
// release): a read-only listing and the approver-gated manual recovery. Both
// endpoints are reserved to the definition approver role — the destination
// URLs are operational perimeter facts, and a retry re-fires a business
// effect at a third party, so it must be a deliberate human action.

// egressDlqItem is the lean per-delivery view the UI renders. No secrets live
// here: the destination URL comes from the DSL and the delivery identity is
// the per-firing uuid — credential material stays in runtime injection only.
type egressDlqItem struct {
	TaskID       string     `json:"task_id"`
	DeliveryID   string     `json:"delivery_id"`
	Entity       string     `json:"entity"`
	RecordID     string     `json:"record_id"`
	Destination  string     `json:"destination"`
	CreatedAt    *time.Time `json:"created_at,omitempty"`
	Attempts     int        `json:"attempts"`
	LastCategory string     `json:"last_category,omitempty"`
}

func (s *Server) egressDlq(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	if actor.Role != s.eng.DefinitionApprover() {
		writeJSON(w, http.StatusForbidden, &engine.Err{Code: engine.CodePermissionDenied,
			Message: "the dead-letter queue is reserved to the definition approver role",
			Rule:    "definition approver"})
		return
	}
	tasks := s.eng.FailedWebhookTasks()
	// per-task attempt counts from the folded receipts (observability only)
	attempts := map[string]int{}
	lastCategory := map[string]string{}
	for _, rec := range s.eng.WebhookReceipts() {
		attempts[rec.TaskID]++
		if rec.Outcome == "failed" {
			lastCategory[rec.TaskID] = rec.Category
		}
	}
	out := make([]egressDlqItem, 0, len(tasks))
	for _, t := range tasks {
		var created *time.Time
		if !t.CreatedAt.IsZero() {
			ts := t.CreatedAt
			created = &ts
		}
		out = append(out, egressDlqItem{
			TaskID: t.ID, DeliveryID: t.DeliveryID, Entity: t.Entity,
			RecordID: t.RecordID, Destination: t.Args, CreatedAt: created,
			Attempts: attempts[t.ID], LastCategory: lastCategory[t.ID],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "count": len(out)})
}

func (s *Server) egressRetry(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	taskID := r.PathValue("id")
	basis := &eventstore.Basis{Type: "human", ID: actor.ID}
	retried, err := s.eng.RetryWebhookTask(r.Context(), actor, taskID, basis)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id":      retried.ID,
		"delivery_id":  retried.DeliveryID,
		"destination":  retried.Args,
		"retried_from": taskID,
	})
}
