package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"

	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// Bootstrap lets workers self-register on first start using a node shared
// secret from the deployment (docker compose / k8s secret), instead of a human
// running `kalita agent add` for each. Hardened: the secret only mints roles
// on an allowlist, so a leaked secret cannot create an Owner.
//
//   POST /api/bootstrap {secret, id, role, model?}  ->  {token, role}

type bootstrapConfig struct {
	secret string
	roles  map[string]bool // allowlisted worker roles
}

func (s *Server) enableBootstrap(secret string, roles []string) {
	allow := map[string]bool{}
	for _, r := range roles {
		allow[r] = true
	}
	s.boot = &bootstrapConfig{secret: secret, roles: allow}
	s.mux.HandleFunc("POST /api/bootstrap", s.bootstrap)
}

func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Secret string `json:"secret"`
		ID     string `json:"id"`
		Role   string `json:"role"`
		Model  string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR", "message": "bad json"})
		return
	}
	// constant-time secret check; generic failure (don't reveal which part)
	if s.boot == nil || subtle.ConstantTimeCompare([]byte(req.Secret), []byte(s.boot.secret)) != 1 {
		writeJSON(w, http.StatusForbidden, map[string]string{"code": "BOOTSTRAP_DENIED",
			"message": "bootstrap secret invalid"})
		return
	}
	if req.ID == "" || !s.boot.roles[req.Role] {
		writeJSON(w, http.StatusForbidden, map[string]string{"code": "BOOTSTRAP_DENIED",
			"message": "id required and role must be a worker role",
			"fix_hint": "bootstrap only mints the worker roles configured on the node"})
		return
	}
	token, err := s.reg.EnsureActor(r.Context(), req.ID, eventstore.ActorAgent, req.Role,
		&identity.ActorMeta{Model: req.Model, Description: "bootstrapped worker"})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"token": token, "role": req.Role})
}
