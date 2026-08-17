package api

import (
	"encoding/json"
	"net/http"

	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Password login (LLD-05 §3, PK-4): a UX wrapper over the existing bearer-
// token mechanism for a human who will not carry a token around. NOT a new
// identity model, NOT consumer-scale self-signup — the actor must already be
// registered (kalita user add / invite redemption); this only adds a second
// way to obtain that SAME actor's bearer token.

type setPasswordRequest struct {
	Password string `json:"password"`
}

// setPassword lets an authenticated human set a password credential for an
// existing actor (typically their own — mirrors the human-only gate already
// used by disableActor/createInvite in this v0 admin model).
func (s *Server) setPassword(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok || actor.Type != eventstore.ActorHuman {
		writeAuthRequired(w)
		return
	}
	var req setPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR",
			"message": "password is required", "fix_hint": `send {"password": "at least 8 characters"}`})
		return
	}
	id := r.PathValue("id")
	// D-014 (Cycle-A2): setting ANOTHER actor's password is an admin action.
	// Any human may set only their own password; the node admin (defApprover)
	// may reset anyone's. Denied BEFORE the registry call so a refusal
	// journals nothing.
	if id != actor.ID && actor.Role != s.eng.DefinitionApprover() {
		writeErr(w, &engine.Err{Code: engine.CodePermissionDenied,
			Message: "only the actor themself or the node admin (role " + s.eng.DefinitionApprover() + ") may set this password",
			Rule:    "identity admin boundary (self-or-admin)"})
		return
	}
	if err := s.reg.SetPassword(r.Context(), actor, id, req.Password,
		&eventstore.Basis{Type: "human", ID: actor.ID}); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "VALIDATION_ERROR", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type loginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

// login is a PUBLIC endpoint (like /api/register): exchange login+password
// for an ordinary bearer token of the SAME actor already registered on this
// node. Wrong login/password/no-password-set/disabled all collapse to a
// single 401 — no oracle for enumerating which usernames exist.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Login == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR",
			"message": "login and password are required", "fix_hint": `send {"login": "you", "password": "..."}`})
		return
	}
	if s.reg == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "LOGIN_FAILED", "message": "invalid login or password"})
		return
	}
	token, err := s.reg.LoginWithPassword(r.Context(), req.Login, req.Password)
	if err != nil {
		// identity.ErrActorUnknown / ErrActorDisabled / ErrNoPassword / ErrBadPassword
		// all read the same to the caller (no username-enumeration oracle).
		writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "LOGIN_FAILED", "message": "invalid login or password"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}
