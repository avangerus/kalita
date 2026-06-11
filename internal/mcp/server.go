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

	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

const protocolVersion = "2025-06-18"

type Server struct {
	eng *engine.Engine
	reg *identity.Registry

	mu      sync.Mutex
	windows map[string]*rateWindow
	perMin  int
}

type rateWindow struct {
	count int
	reset time.Time
}

func New(eng *engine.Engine, reg *identity.Registry) *Server {
	return &Server{eng: eng, reg: reg, windows: map[string]*rateWindow{}, perMin: 240}
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
			"serverInfo":      map[string]any{"name": "kalita", "version": "0.1.0"},
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
	return eventstore.Actor{Type: info.Type, ID: info.ID, Role: info.Role}, nil
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
