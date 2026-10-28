// Package mcp is the agent gateway: the MCP (Model Context Protocol) server
// over HTTP JSON-RPC. It is a first-class interface equal to the human UI —
// same engine, same permission checks, same journal (MCP-CONTRACT-v0).
//
// v0 implements the protocol subset every MCP client uses: initialize,
// tools/list, tools/call over streamable HTTP (single JSON responses).
package mcp

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/avangerus/kalita/internal/api"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
	"github.com/avangerus/kalita/internal/transport"
)

const protocolVersion = "2025-06-18"

type Server struct {
	eng *engine.Engine
	reg *identity.Registry

	// rag configures the search_perimeter node-tool (packs/knowvault/README.md
	// §2). nil means the tool is not enabled on this node — dispatch answers
	// with NOT_CONFIGURED rather than a nil-pointer panic. Set via
	// WithRAGSearch, the same four values api.WithRAGSearch takes, so a node
	// that mounts both REST /api/search and the MCP tool configures identical
	// backend/scope/log settings for both — one perimeter, two doors.
	rag *api.RAGConfig

	mu      sync.Mutex
	windows map[string]*rateWindow
	perMin  int

	// version is reported in serverInfo at the initialize handshake — the
	// SAME release version as the binary (injected via -X main.version,
	// OPS-1). A hardcoded fallback here would silently diverge from the
	// binary's own version, so it defaults empty and the node wires it.
	version string
}

type rateWindow struct {
	count int
	reset time.Time
}

// Option configures the Server. Mirrors internal/api's Option pattern.
type Option func(*Server)

// WithVersion sets the version reported in the MCP initialize handshake.
func WithVersion(v string) Option { return func(s *Server) { s.version = v } }

// WithRAGSearch enables the search_perimeter MCP tool, delegating to the
// exact same api.SearchPerimeter governance core the REST /api/search
// endpoint uses (packs/knowvault/README.md §1.2/§2) — pass the same
// backend/scope/logEntity/role a co-located api.WithRAGSearch call uses.
func WithRAGSearch(backend, scope, logEntity, role string) Option {
	return func(s *Server) {
		s.rag = &api.RAGConfig{Backend: backend, Scope: scope, LogEntity: logEntity, Role: role}
	}
}

func New(eng *engine.Engine, reg *identity.Registry, opts ...Option) *Server {
	s := &Server{eng: eng, reg: reg, windows: map[string]*rateWindow{}, perMin: 240}
	for _, o := range opts {
		o(s)
	}
	return s
}

// --- JSON-RPC plumbing --------------------------------------------------------

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPC(w, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
		return
	}
	// notifications get no response body
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	switch req.Method {
	case "initialize":
		writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "kalita", "version": s.version},
			// instructions ride once at handshake, not per request: point the
			// agent at the grammar — the compact normative summary of the whole
			// language — plus a canonical example to pattern-match against.
			"instructions": "To author a pack, call `get_grammar` once: it returns the kalita DSL grammar summary (every block and type) plus a canonical example. Write packs against it, then `validate_dsl` to compile-check (fix by the structured errors), and `propose_change` to apply (a human signs). Field syntax is `name: type [modifiers]`. For structured authoring without DSL text you may instead use `compose_pack` (JSON in, validated DSL out).",
		}})
	case "ping":
		writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})
	case "tools/list":
		writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": toolDefs}})
	case "tools/call":
		s.toolsCall(w, r, &req)
	default:
		writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}})
	}
}

func (s *Server) toolsCall(w http.ResponseWriter, r *http.Request, req *rpcRequest) {
	actor, errObj := s.authenticate(r)
	if errObj != nil {
		writeToolError(w, req.ID, errObj)
		return
	}
	if !s.allowRate(actor.ID) {
		writeToolError(w, req.ID, map[string]any{
			"code": "RATE_LIMITED", "message": "rate limit exceeded", "retry_after": 60})
		return
	}

	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &call); err != nil {
		writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: req.ID,
			Error: &rpcError{Code: -32602, Message: "invalid params"}})
		return
	}
	result, terr := s.dispatch(r, actor, call.Name, call.Arguments)
	if terr != nil {
		writeToolError(w, req.ID, terr)
		return
	}
	writeToolResult(w, req.ID, result, false)
}

// authenticate maps Bearer token → actor. Anonymous agents do not exist.
func (s *Server) authenticate(r *http.Request) (eventstore.Actor, map[string]any) {
	h := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(h, "Bearer ")
	if !ok || token == "" {
		return eventstore.Actor{}, map[string]any{
			"code": "AUTH_REQUIRED", "message": "missing bearer token",
			"fix_hint": "register the agent on the node (kalita agent add) and pass Authorization: Bearer <token>"}
	}
	info, err := s.reg.Authenticate(r.Context(), token)
	if err != nil {
		return eventstore.Actor{}, map[string]any{
			"code": "AUTH_REQUIRED", "message": "token does not resolve to an active actor",
			"fix_hint": "the token may be revoked; ask the node admin to re-issue it"}
	}
	return transport.ActorFromInfo(info), nil
}

func (s *Server) allowRate(actorID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	wdw := s.windows[actorID]
	now := time.Now()
	if wdw == nil || now.After(wdw.reset) {
		s.windows[actorID] = &rateWindow{count: 1, reset: now.Add(time.Minute)}
		return true
	}
	wdw.count++
	return wdw.count <= s.perMin
}

// --- response helpers ------------------------------------------------------------

func writeRPC(w http.ResponseWriter, resp rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// writeToolResult wraps a value as MCP tool content (single text block of JSON).
func writeToolResult(w http.ResponseWriter, id json.RawMessage, v any, isErr bool) {
	text, _ := json.Marshal(v)
	writeRPC(w, rpcResponse{JSONRPC: "2.0", ID: id, Result: map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(text)}},
		"isError": isErr,
	}})
}

// writeToolError reports a structured, self-correction-grade error as tool
// output (isError=true), not as a protocol failure: the agent reads it.
func writeToolError(w http.ResponseWriter, id json.RawMessage, errObj any) {
	if e, ok := errObj.(*engine.Err); ok {
		writeToolResult(w, id, e, true)
		return
	}
	writeToolResult(w, id, errObj, true)
}
