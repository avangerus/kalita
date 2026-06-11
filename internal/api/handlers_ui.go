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
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	m := s.eng.MetaFor(actor.ID, actor.Role)
	// surface optional node capabilities the UI keys off (e.g. the search tab)
	out, _ := json.Marshal(m)
	var withCaps map[string]any
	_ = json.Unmarshal(out, &withCaps)
	withCaps["search"] = s.rag != nil
	writeJSON(w, http.StatusOK, withCaps)
}

type actRequest struct {
	Action         string            `json:"action"`
	Basis          *eventstore.Basis `json:"basis"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
}

func (s *Server) act(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
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
	actor, ok := s.actor(r)
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
	actor, ok := s.actor(r)
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
	actor, ok := s.actor(r)
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
	actor, ok := s.actor(r)
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

// actors: the directory behind the Agents screen. Humans only — agents do
// not enumerate or manage each other.
func (s *Server) actors(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok || actor.Type != eventstore.ActorHuman {
		writeAuthRequired(w)
		return
	}
	if s.reg == nil {
		writeJSON(w, http.StatusOK, map[string]any{"actors": []any{}})
		return
	}
	list, err := s.reg.List(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"actors": list})
}

func (s *Server) disableActor(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok || actor.Type != eventstore.ActorHuman {
		writeAuthRequired(w)
		return
	}
	err := s.reg.Disable(r.Context(), actor, r.PathValue("id"),
		&eventstore.Basis{Type: "human", ID: actor.ID})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type inviteRequest struct {
	Role      string `json:"role"`
	Entity    string `json:"entity,omitempty"`
	RecordID  string `json:"record_id,omitempty"`
	BindField string `json:"bind_field,omitempty"`
}

// createInvite issues a one-time registration code (humans only).
func (s *Server) createInvite(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok || actor.Type != eventstore.ActorHuman {
		writeAuthRequired(w)
		return
	}
	var req inviteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Role == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR",
			"message": "role is required", "fix_hint": `send {"role": "Customer", "entity": "Customer", "record_id": "..."}`})
		return
	}
	code, err := s.reg.CreateInvite(r.Context(), actor, req.Role, req.Entity, req.RecordID, req.BindField,
		&eventstore.Basis{Type: "human", ID: actor.ID})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"invite_code": code})
}

// register is the only PUBLIC endpoint: redeem an invite, become an actor.
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Invite string `json:"invite"`
		ID     string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Invite == "" || req.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR",
			"message": "invite and id are required", "fix_hint": `send {"invite": "<code>", "id": "you@example.com"}`})
		return
	}
	token, inv, err := s.reg.Redeem(r.Context(), req.ID, req.Invite)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"code": "INVITE_INVALID", "message": err.Error()})
		return
	}
	resp := map[string]any{"token": token, "role": inv.Role}
	// bind the record to the new actor (convention: a field holding the actor
	// id powers `where <field> = $me` row-level visibility in the pack)
	if inv.Entity != "" && inv.RecordID != "" {
		_, err := s.eng.Update(r.Context(), inv.CreatedBy, inv.Entity, inv.RecordID,
			map[string]any{inv.BindField: req.ID},
			&eventstore.Basis{Type: "human", ID: inv.CreatedBy.ID}, "")
		if err != nil {
			resp["bind_warning"] = err.Error() // registered, but the pack must bind manually
		} else {
			resp["bound"] = inv.Entity + "/" + inv.RecordID
		}
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) proposals(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.actor(r); !ok {
		writeAuthRequired(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposals": s.eng.PendingProposals()})
}

func (s *Server) decideProposal(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req decideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR", "message": "bad json"})
		return
	}
	p, err := s.eng.DecideProposal(r.Context(), actor, r.PathValue("id"), req.Grant, req.Signature, req.Basis)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}
