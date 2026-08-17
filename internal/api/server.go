// Package api is the REST layer over the engine: same operations, same error
// codes, zero business logic of its own. The UI (week 7) and humans consume
// it; agents get the MCP gateway (week 6).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
	"github.com/avangerus/kalita/internal/transport"
)

// Server wraps the engine with HTTP. Identity: bearer tokens resolved through
// the registry — same mechanism as agents, no parallel access model (SECURITY
// rule #1). Dev headers (X-Actor-Id/Role) exist ONLY behind an explicit
// opt-in for local development and tests.
type Server struct {
	eng     *engine.Engine
	reg     *identity.Registry
	mux     *http.ServeMux
	devAuth bool
	rag     *ragConfig
	call    *callConfig
	boot    *bootstrapConfig
	brand   brandConfig
	// limiter is the per-actor REST rate limit (LLD-10 §4), the same
	// window-per-minute shape as internal/mcp/server.go, applied once per
	// request in ServeHTTP.
	limiter rateLimiter
	// nodeID is the node's own public key (LLD-02 §2 "node_id"): the
	// canonical string embeds it so a passkey signature is not portable
	// between nodes. Empty on a node started without WithNodeID — the
	// credential/decide endpoints still work, just with an empty node_id
	// component (fine for dev; production wiring sets it from node.pub).
	nodeID string
	// requireHumanSig mirrors engine.WithRequireHumanSignature (LLD-02 §5):
	// when set, a decision's human_signature verification also requires the
	// UV (user verification) flag.
	requireHumanSig bool
	// challenges holds short-lived WebAuthn ceremony state (registration
	// challenges, approval-decision challenges) — see credentials.go.
	challenges challengeStore
}

// brandConfig white-labels the UI: the product the customer sees, not "Kalita".
type brandConfig struct {
	Name    string `json:"name"`
	Accent  string `json:"accent,omitempty"`
	Tagline string `json:"tagline,omitempty"`
}

// Option configures the server.
type Option func(*Server)

// WithDevHeaders enables X-Actor-Id/X-Actor-Role identity. NEVER in production.
func WithDevHeaders() Option { return func(s *Server) { s.devAuth = true } }

// WithRAGSearch mounts POST /api/search, proxying to a search worker while the
// node keeps the permission boundary and journaling.
func WithRAGSearch(backend, scope, logEntity, role string) Option {
	return func(s *Server) {
		s.enableRAG(ragConfig{Backend: backend, Scope: scope, LogEntity: logEntity, Role: role})
	}
}

// WithCallProxy mounts GET /api/call/{name}, the generic read-only capability
// proxy. The connectorEntity holds the runtime records that bind an integration
// name to a backend URL (default "Connector"). The route is always mounted (a
// pack without integrations simply never grants `call`); this option only
// repoints the connector entity.
func WithCallProxy(connectorEntity string) Option {
	return func(s *Server) { s.call = &callConfig{Entity: connectorEntity} }
}

// WithBootstrap mounts POST /api/bootstrap so workers self-register with a
// shared secret, minting only the allowlisted roles.
func WithBootstrap(secret string, roles []string) Option {
	return func(s *Server) { s.enableBootstrap(secret, roles) }
}

// WithBrand white-labels the UI (product name shown instead of "Kalita").
func WithBrand(name, accent, tagline string) Option {
	return func(s *Server) {
		if name != "" {
			s.brand = brandConfig{Name: name, Accent: accent, Tagline: tagline}
		}
	}
}

// WithNodeID sets the node's public key (LLD-02 §2 "node_id") — the
// canonical string embeds it so a passkey signature is not portable between
// nodes. Pass the same base64 the node already writes to node.pub.
func WithNodeID(id string) Option { return func(s *Server) { s.nodeID = id } }

// WithRequireHumanSignature enforces UV (user verification) on every
// WebAuthn human_signature this server verifies (LLD-02 §5). Pair with
// engine.WithRequireHumanSignature on the SAME node — this option only
// changes what the API layer requires from a signature that IS present;
// the engine option is what rejects a decision that has none.
func WithRequireHumanSignature() Option { return func(s *Server) { s.requireHumanSig = true } }

func New(eng *engine.Engine, reg *identity.Registry, opts ...Option) *Server {
	s := &Server{eng: eng, reg: reg, mux: http.NewServeMux(), brand: brandConfig{Name: "Kalita"}}
	for _, o := range opts {
		o(s)
	}
	// public: the login screen needs the brand before any auth
	s.mux.HandleFunc("GET /api/brand", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.brand)
	})
	// public: liveness/readiness for orchestrators (k8s probes, docker healthcheck,
	// load balancers). No auth. /healthz is pure liveness; /readyz confirms a pack
	// is loaded (the node can actually serve).
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if s.eng == nil || s.eng.Model() == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "starting"})
			return
		}
		// readiness must include the journal itself: a dead Postgres pool is
		// NOT ready, even though the in-memory projection is up (LLD-09 §3).
		if err := s.eng.Ping(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "reason": "journal unreachable"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	s.mux.HandleFunc("GET /metrics", s.metrics)
	s.mux.HandleFunc("GET /api/system", s.describe)
	s.mux.HandleFunc("GET /api/meta", s.meta)
	s.mux.HandleFunc("GET /api/records/{entity}", s.query)
	s.mux.HandleFunc("POST /api/query/{entity}", s.queryV2)
	s.mux.HandleFunc("GET /api/records/{entity}/{id}", s.get)
	s.mux.HandleFunc("POST /api/records/{entity}", s.create)
	s.mux.HandleFunc("PATCH /api/records/{entity}/{id}", s.update)
	s.mux.HandleFunc("POST /api/records/{entity}/{id}/act", s.act)
	s.mux.HandleFunc("GET /api/records/{entity}/{id}/journal", s.journal)
	s.mux.HandleFunc("GET /api/records/{entity}/{id}/diff", s.diff)
	s.mux.HandleFunc("POST /api/records/{entity}/{id}/revert", s.revert)
	s.mux.HandleFunc("POST /api/records/{entity}/{id}/rewind", s.rewind)
	s.mux.HandleFunc("GET /api/approvals", s.approvals)
	s.mux.HandleFunc("GET /api/approvals/{id}/challenge", s.approvalChallenge)
	s.mux.HandleFunc("POST /api/approvals/{id}/decide", s.decide)
	s.mux.HandleFunc("POST /api/credentials/begin", s.credentialsBegin)
	s.mux.HandleFunc("POST /api/credentials/finish", s.credentialsFinish)
	s.mux.HandleFunc("GET /api/credentials", s.listCredentials)
	s.mux.HandleFunc("DELETE /api/credentials/{id}", s.revokeCredential)
	s.mux.HandleFunc("GET /api/tasks", s.tasks)
	s.mux.HandleFunc("GET /api/proposals", s.proposals)
	s.mux.HandleFunc("POST /api/proposals/{id}/decide", s.decideProposal)
	s.mux.HandleFunc("GET /api/agent-board", s.agentBoard)
	s.mux.HandleFunc("GET /api/actors", s.actors)
	s.mux.HandleFunc("POST /api/actors/{id}/disable", s.disableActor)
	s.mux.HandleFunc("POST /api/invites", s.createInvite)
	s.mux.HandleFunc("POST /api/register", s.register)
	s.mux.HandleFunc("POST /api/actors/{id}/password", s.setPassword)
	s.mux.HandleFunc("POST /api/login", s.login)
	s.mux.HandleFunc("POST /api/files", s.uploadFile)
	s.mux.HandleFunc("GET /api/files/{hash}", s.downloadFile)
	s.mux.HandleFunc("GET /api/records/{entity}/{id}/comments", s.listComments)
	s.mux.HandleFunc("POST /api/records/{entity}/{id}/comments", s.postComment)
	s.mux.HandleFunc("GET /api/dashboards", s.dashboards)
	s.mux.HandleFunc("GET /api/dashboards/{name}", s.dashboard)
	s.mux.HandleFunc("GET /api/records/{entity}/{id}/links", s.listLinks)
	s.mux.HandleFunc("POST /api/records/{entity}/{id}/links", s.addLink)
	s.mux.HandleFunc("DELETE /api/records/{entity}/{id}/links", s.removeLink)
	// capability proxy is always available; an opt-in WithCallProxy only changes
	// the connector entity. Defaults applied in enableCallProxy.
	cfg := callConfig{}
	if s.call != nil {
		cfg = *s.call
	}
	s.enableCallProxy(cfg)
	return s
}

// ServeHTTP resolves the caller identity exactly once per request — stashed
// in context for actor() below to read — then applies the REST rate limit
// (LLD-10 §4, per-actor, same shape as internal/mcp/server.go's 240/min)
// before handing off to the route mux. Resolving once here (rather than
// leaving every handler to call Authenticate independently, as they still do
// via actor()) matters beyond avoiding repeat work: a failed bearer token
// records exactly one auth-fail count per request (see resolveActor) — if
// resolution ran again inside the handler, that count would double.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a, ok := s.resolveActor(r)
	r = r.WithContext(context.WithValue(r.Context(), actorCtxKey, resolvedActor{actor: a, ok: ok}))
	if ok && s.rateLimited(a.ID) {
		writeRateLimited(w)
		return
	}
	s.mux.ServeHTTP(w, r)
}

// --- identity -------------------------------------------------------------------

type ctxKey struct{ name string }

var actorCtxKey = ctxKey{"api.actor"}

// resolvedActor is what ServeHTTP stashes in the request context so actor()
// below reads a cached result rather than re-authenticating (see ServeHTTP).
type resolvedActor struct {
	actor eventstore.Actor
	ok    bool
}

// actor returns the caller resolved by ServeHTTP for this request. The
// fallback to resolveActor directly exists only for callers that reach a
// handler without going through Server.ServeHTTP (there are none in this
// binary's own request path today, but a direct unit test invoking a handler
// would otherwise panic on a type-assertion of a context value that was never
// set) — that path re-authenticates and so may double-count an auth failure;
// it is not the hot path.
func (s *Server) actor(r *http.Request) (eventstore.Actor, bool) {
	if ra, ok := r.Context().Value(actorCtxKey).(resolvedActor); ok {
		return ra.actor, ra.ok
	}
	return s.resolveActor(r)
}

// resolveActor does the actual identity resolution: bearer token via the
// registry; dev headers only when explicitly enabled. A bearer token that is
// present but does not resolve counts as one auth failure against the
// caller's IP (LLD-10 §4) — aggregated, not journaled per attempt (see
// identity.Registry.RecordAuthFailure).
func (s *Server) resolveActor(r *http.Request) (eventstore.Actor, bool) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") && s.reg != nil {
		info, err := s.reg.Authenticate(r.Context(), strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			s.reg.RecordAuthFailure(r.Context(), clientIP(r))
			return eventstore.Actor{}, false
		}
		return transport.ActorFromInfo(info), true
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
	if r.PathValue("entity") == coreUserEntity {
		s.serveCoreUserList(w, r, r.URL.Query().Get("search"), atoiOr(r.URL.Query().Get("limit"), 0))
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

func (s *Server) dashboards(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.actor(r); !ok {
		writeAuthRequired(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dashboards": s.eng.Dashboards()})
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	res, e := s.eng.Dashboard(r.Context(), actor, r.PathValue("name"))
	if e != nil {
		writeErr(w, e)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	if r.PathValue("entity") == coreUserEntity {
		s.serveCoreUserGet(w, r, r.PathValue("id"))
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
	var req transport.MutationRequest
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
	var req transport.MutationRequest
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
	engine.CodePermissionDenied:  http.StatusForbidden,
	engine.CodeValidation:        http.StatusUnprocessableEntity,
	engine.CodeNotFound:          http.StatusNotFound,
	engine.CodeConflict:          http.StatusConflict,
	engine.CodeBasisRequired:     http.StatusUnprocessableEntity,
	engine.CodeSignatureRequired: http.StatusForbidden,
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
