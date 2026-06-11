package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Handlers backing the generated UI (week 7): per-actor meta, workflow
// actions, the approval inbox and human task queues.

func (s *Server) meta(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	writeJSON(w, http.StatusOK, s.eng.MetaFor(actor.ID, actor.Role))
}

type actRequest struct {
	Action         string            `json:"action"`
	Basis          *eventstore.Basis `json:"basis"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
}

func (s *Server) act(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req actRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR", "message": "bad json"})
		return
	}
	res, err := s.eng.Act(r.Context(), actor, r.PathValue("entity"), r.PathValue("id"), req.Action, req.Basis, req.IdempotencyKey)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) journal(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	events, err := s.eng.Journal(r.Context(), actor, r.PathValue("entity"), r.PathValue("id"), 200)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]map[string]any, 0, len(events))
	for _, ev := range events {
		out = append(out, map[string]any{
			"seq": ev.Seq, "ts": ev.TS.Format(time.RFC3339), "kind": ev.Kind,
			"actor": ev.Actor, "payload": json.RawMessage(ev.Payload), "basis": ev.Basis,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

func (s *Server) approvals(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": s.eng.PendingApprovals(actor.Role)})
}

type decideRequest struct {
	Grant     bool              `json:"grant"`
	Signature []byte            `json:"signature,omitempty"`
	Basis     *eventstore.Basis `json:"basis"`
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req decideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR", "message": "bad json"})
		return
	}
	rec, err := s.eng.Decide(r.Context(), actor, r.PathValue("id"), req.Grant, req.Signature, req.Basis)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) tasks(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tasks": s.eng.Tasks(actor.Role, engine.TaskStatus(status)),
	})
}
