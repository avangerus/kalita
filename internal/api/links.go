package api

import (
	"encoding/json"
	"net/http"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Record links (named bidirectional relations). The link is one fact; both
// records see it under their own name.

type linkRequest struct {
	Name    string            `json:"name"`     // the relation name from this record's side
	OtherID string            `json:"other_id"` // the record to link to
	Basis   *eventstore.Basis `json:"basis"`
}

func (s *Server) listLinks(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	links := s.eng.LinksOf(actor, r.PathValue("entity"), r.PathValue("id"))
	writeJSON(w, http.StatusOK, map[string]any{"links": links})
}

func (s *Server) addLink(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req linkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR", "message": "bad json"})
		return
	}
	if err := s.eng.Link(r.Context(), actor, r.PathValue("entity"), r.PathValue("id"), req.Name, req.OtherID, req.Basis); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) removeLink(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req linkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR", "message": "bad json"})
		return
	}
	if err := s.eng.Unlink(r.Context(), actor, r.PathValue("entity"), r.PathValue("id"), req.Name, req.OtherID, req.Basis); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
