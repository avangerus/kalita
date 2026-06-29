package api

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/avangerus/kalita/internal/engine"
)

// Capability call proxy: a generic, read-only forward from the UI to a
// connector worker. It mirrors ragproxy.go — the node owns the two things a
// worker must not be trusted with, the permission boundary (ABAC `call`) and
// the resolution of WHERE the worker lives — and delegates the read itself.
//
// The connector's backend URL is RUNTIME DATA (a record), so a connector is
// added or repointed without a rebuild: the integration contract is in the
// pack, the endpoint is a record.
//
// Flow: authenticate -> ABAC `call <connector>.<fn>` -> resolve the backend
// from the connector record (name == <connector part>) -> GET
// <backend>/<fn>?<rawquery> -> return the worker's body and status verbatim.
// Read-only: nothing is journaled.
//
// Generic by config (knowvault is just the first user):
//
//	--connector-entity  entity holding the connector records (default Connector)
type callConfig struct {
	Entity       string // entity whose records bind a connector name to a backend URL
	NameField    string // field matching the integration name (default "name")
	BackendField string // field holding the worker URL (default "backend")
	EnabledField string // bool field gating the connector (default "enabled")
}

func (s *Server) enableCallProxy(cfg callConfig) {
	if cfg.Entity == "" {
		cfg.Entity = "Connector"
	}
	if cfg.NameField == "" {
		cfg.NameField = "name"
	}
	if cfg.BackendField == "" {
		cfg.BackendField = "backend"
	}
	if cfg.EnabledField == "" {
		cfg.EnabledField = "enabled"
	}
	s.call = &cfg
	s.mux.HandleFunc("GET /api/call/{name}", s.callCapability)
}

func (s *Server) callCapability(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	name := r.PathValue("name") // "<connector>.<fn>", e.g. files.list_dirs
	connector, fn, found := strings.Cut(name, ".")
	if !found || connector == "" || fn == "" {
		writeJSON(w, http.StatusBadRequest, &engine.Err{Code: engine.CodeValidation,
			Message: "capability must be <connector>.<function>",
			FixHint: "call e.g. GET /api/call/files.list_dirs"})
		return
	}

	// permission boundary stays in core: the actor must hold `call <name>`
	if !s.eng.CanCall(actor.Role, name) {
		writeJSON(w, http.StatusForbidden, &engine.Err{Code: engine.CodePermissionDenied,
			Message: "role " + actor.Role + " may not call " + name,
			Rule:    "call " + name})
		return
	}

	// resolve the backend from runtime data (node authority — the operator need
	// not be able to read the connector record to use the capability)
	rec, ok := s.eng.LookupByField(s.call.Entity, s.call.NameField, connector)
	if !ok {
		writeJSON(w, http.StatusBadGateway, &engine.Err{Code: "CONNECTOR_NOT_FOUND",
			Message: "no " + s.call.Entity + " record named " + connector,
			FixHint: "create a " + s.call.Entity + " record with " + s.call.NameField + "=" + connector})
		return
	}
	if en, ok := rec[s.call.EnabledField].(bool); ok && !en {
		writeJSON(w, http.StatusBadGateway, &engine.Err{Code: "CONNECTOR_DISABLED",
			Message: "connector " + connector + " is disabled",
			FixHint: "enable the " + s.call.Entity + " record to use this capability"})
		return
	}
	backend, _ := rec[s.call.BackendField].(string)
	if backend == "" {
		writeJSON(w, http.StatusBadGateway, &engine.Err{Code: "CONNECTOR_NO_BACKEND",
			Message: "connector " + connector + " has no backend URL",
			FixHint: "set " + s.call.BackendField + " on the " + s.call.Entity + " record"})
		return
	}

	target := strings.TrimRight(backend, "/") + "/" + fn
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	proxyReq, _ := http.NewRequestWithContext(ctx, "GET", target, nil)
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, &engine.Err{Code: "CONNECTOR_UNAVAILABLE",
			Message: "connector " + connector + " not reachable: " + err.Error(),
			FixHint: "the connector worker may be down — check the service"})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// read-only: forward the worker's body and status verbatim, do NOT journal
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}
