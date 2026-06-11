// Package api is the REST layer over the engine: same operations, same error
// codes, zero business logic of its own. The UI (week 7) and humans consume
// it; agents get the MCP gateway (week 6).
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// Server wraps the engine with HTTP. Identity: bearer tokens resolved through
// the registry — same mechanism as agents, no parallel access model (SECURITY
// rule #1). Dev headers (X-Actor-Id/Role) exist ONLY behind an explicit
// opt-in for local development and tests.
type Server struct {
	eng      *engine.Engine
	reg      *identity.Registry
	mux      *http.ServeMux
	devAuth  bool
}

// Option configures the server.
type Option func(*Server)

// WithDevHeaders enables X-Actor-Id/X-Actor-Role identity. NEVER in production.
func WithDevHeaders() Option { return func(s *Server) { s.devAuth = true } }

func New(eng *engine.Engine, reg *identity.Registry, opts ...Option) *Server {
	s := &Server{eng: eng, reg: reg, mux: http.NewServeMux()}
	for _, o := range opts {
		o(s)
	}
	s.mux.HandleFunc("GET /api/system", s.describe)
	s.mux.HandleFunc("GET /api/meta", s.meta)
	s.mux.HandleFunc("GET /api/records/{entity}", s.query)
	s.mux.HandleFunc("GET /api/records/{entity}/{id}", s.get)
	s.mux.HandleFunc("POST /api/records/{entity}", s.create)
	s.mux.HandleFunc("PATCH /api/records/{entity}/{id}", s.update)
	s.mux.HandleFunc("POST /api/records/{entity}/{id}/act", s.act)
	s.mux.HandleFunc("GET /api/records/{entity}/{id}/journal", s.journal)
	s.mux.HandleFunc("GET /api/approvals", s.approvals)
	s.mux.HandleFunc("POST /api/approvals/{id}/decide", s.decide)
	s.mux.HandleFunc("GET /api/tasks", s.tasks)
	s.mux.HandleFunc("GET /api/proposals", s.proposals)
	s.mux.HandleFunc("POST /api/proposals/{id}/decide", s.decideProposal)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// --- identity -------------------------------------------------------------------

// actor resolves the caller: bearer token via the registry; dev headers only
// when explicitly enabled.
func (s *Server) actor(r *http.Request) (eventstore.Actor, bool) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") && s.reg != nil {
		info, err := s.reg.Authenticate(r.Context(), strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			return eventstore.Actor{}, false
		}
		return eventstore.Actor{Type: info.Type, ID: info.ID, Role: info.Role}, true
	}
	if s.devAuth {
		a := eventstore.Actor{
			Type: eventstore.ActorType(r.Header.Get("X-Actor-Type")),
			ID:   r.Header.Get("X-Actor-Id"),
			Role: r.Header.Get("X-Actor-Role"),
		}
		if a.ID != "" && a.Role != "" {
			if a.Type == "" {
				a.Type = eventstore.ActorHuman
			}
			return a, true
		}
	}
	return eventstore.Actor{}, false
}

// --- payloads -------------------------------------------------------------------

type mutateRequest struct {
	Values         map[string]any    `json:"values"`
	Basis          *eventstore.Basis `json:"basis"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
}

type describeEntity struct {
	Name   string   `json:"name"`
	Fields []string `json:"fields"`
}

type describeResponse struct {
	Pack       string           `json:"pack"`
	DefVersion uint64           `json:"def_version"`
	Entities   []describeEntity `json:"entities"`
	Roles      []string         `json:"roles"`
}

// --- handlers ---------------------------------------------------------------------

func (s *Server) describe(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.actor(r); !ok {
		writeAuthRequired(w)
		return
	}
	m := s.eng.Model()
	resp := describeResponse{DefVersion: s.eng.DefVersion()}
	if m.Manifest != nil {
		resp.Pack = m.Manifest.Name
	}
	for _, name := range m.Order {
		de := describeEntity{Name: name}
		for _, f := range m.Entities[name].Fields {
			de.Fields = append(de.Fields, f.Name)
		}
		resp.Entities = append(resp.Entities, de)
	}
	for role := range m.Roles {
		resp.Roles = append(resp.Roles, role)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) query(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	opts := engine.QueryOpts{Filter: map[string]any{}}
	for k, vs := range r.URL.Query() {
		switch k {
		case "limit":
			opts.Limit, _ = strconv.Atoi(vs[0])
		case "offset":
			opts.Offset, _ = strconv.Atoi(vs[0])
		default:
			opts.Filter[k] = coerce(vs[0])
		}
	}
	rows, err := s.eng.Query(r.Context(), actor, r.PathValue("entity"), opts)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": rows})
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	rec, err := s.eng.Get(r.Context(), actor, r.PathValue("entity"), r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req mutateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, &engine.Err{Code: engine.CodeValidation,
			Message: "request body is not valid JSON", FixHint: "send {\"values\": {...}, \"basis\": {...}}"})
		return
	}
	rec, err := s.eng.Create(r.Context(), actor, r.PathValue("entity"), req.Values, req.Basis, req.IdempotencyKey)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req mutateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, &engine.Err{Code: engine.CodeValidation,
			Message: "request body is not valid JSON", FixHint: "send {\"values\": {...}, \"basis\": {...}}"})
		return
	}
	rec, err := s.eng.Update(r.Context(), actor, r.PathValue("entity"), r.PathValue("id"), req.Values, req.Basis, req.IdempotencyKey)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// --- plumbing -----------------------------------------------------------------------

// coerce turns query-string filter values into JSON-shaped ones.
func coerce(s string) any {
	switch s {
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return n
	}
	return s
}

var statusByCode = map[string]int{
	engine.CodePermissionDenied: http.StatusForbidden,
	engine.CodeValidation:       http.StatusUnprocessableEntity,
	engine.CodeNotFound:         http.StatusNotFound,
	engine.CodeConflict:         http.StatusConflict,
	engine.CodeBasisRequired:    http.StatusUnprocessableEntity,
}

func writeErr(w http.ResponseWriter, err error) {
	var e *engine.Err
	if errors.As(err, &e) {
		writeJSON(w, statusByCode[e.Code], e)
		return
	}
	writeJSON(w, http.StatusInternalServerError, &engine.Err{Code: "INTERNAL", Message: err.Error()})
}

func writeAuthRequired(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, &engine.Err{Code: "AUTH_REQUIRED",
		Message: "actor identity missing",
		FixHint: "v0 dev auth: set X-Actor-Id and X-Actor-Role headers"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
