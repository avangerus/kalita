package api

import (
	"encoding/json"
	"net/http"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Comment thread on a record. Visibility is enforced by the engine: an external
// customer sees the conversation but not internal staff notes.

func (s *Server) listComments(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	comments, err := s.eng.CommentsOf(actor, r.PathValue("entity"), r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": comments})
}

func (s *Server) postComment(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req struct {
		Body     string            `json:"body"`
		Internal bool              `json:"internal"`
		Basis    *eventstore.Basis `json:"basis"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR", "message": "bad json"})
		return
	}
	c, err := s.eng.Comment(r.Context(), actor, r.PathValue("entity"), r.PathValue("id"), req.Body, req.Internal, req.Basis)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}
