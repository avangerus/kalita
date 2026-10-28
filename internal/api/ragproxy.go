package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
)

// RAG search proxy: gives the UI an HTTP search endpoint, and (via
// SearchPerimeter below) internal/mcp's search_perimeter node-tool an agent
// search capability — WITHOUT putting any embedding/vector/LLM knowledge in
// the core. The node owns the two things a worker must not be trusted with —
// the permission boundary and the audit — and delegates the heavy lifting to
// a configured search worker.
//
// Flow: authenticate -> compute the record ids the actor may read of the
// scope entity (e.g. Workspace) -> POST {question, scope_ids} to the worker
// -> journal the query as a record -> return the worker's answer.
//
// Generic by config (knowvault is just the first user):
//   --search-backend  worker URL
//   --search-scope    entity whose visible records bound the search (Workspace)
//   --search-log      entity to journal each query into (SearchQuery)
//
// packs/knowvault/README.md §1.2/§2 requires ONE governance core reachable
// from both the human REST path and the agent MCP path — never two
// implementations of the permission filter or the audit trail. SearchPerimeter
// below is that core; the REST handler (search) is a thin HTTP wrapper over
// it, and internal/mcp's search_perimeter tool calls the exact same function.

type ragConfig struct {
	Backend   string
	Scope     string // entity bounding visibility (e.g. Workspace)
	LogEntity string // entity to record queries into (e.g. SearchQuery)
	Role      string // role written into the log record
}

// RAGConfig is the exported mirror of ragConfig. internal/mcp lives in a
// separate package and cannot reach the Server's unexported rag field, so its
// search_perimeter tool is configured with its own RAGConfig (populated from
// the same --search-backend/--search-scope/--search-log/role values as
// api.WithRAGSearch) and passes it straight into SearchPerimeter. This is
// still a single implementation — RAGConfig carries only configuration, all
// the governance logic lives in SearchPerimeter itself.
type RAGConfig struct {
	Backend   string
	Scope     string
	LogEntity string
	Role      string
}

func (s *Server) enableRAG(cfg ragConfig) {
	s.rag = &cfg
	s.mux.HandleFunc("POST /api/search", s.search)
}

type searchRequest struct {
	Question  string `json:"question"`
	Workspace string `json:"workspace,omitempty"` // optional: scope to one project
	Limit     int    `json:"limit,omitempty"`     // optional: cap the worker's result count
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR",
			"message": "question is required", "fix_hint": `send {"question": "..."}`})
		return
	}

	answer, errObj := SearchPerimeter(r.Context(), s.eng, actor, RAGConfig{
		Backend: s.rag.Backend, Scope: s.rag.Scope, LogEntity: s.rag.LogEntity, Role: s.rag.Role,
	}, req.Question, req.Workspace, req.Limit)
	if errObj != nil {
		writeRAGError(w, errObj)
		return
	}
	writeJSON(w, http.StatusOK, answer)
}

// writeRAGError maps SearchPerimeter's error shape to HTTP status, preserving
// the exact statuses the REST endpoint used before it was split out into a
// shared core: an *engine.Err (e.g. PERMISSION_DENIED from the visibility
// check) goes through the normal writeErr/statusByCode path used everywhere
// else in this package; the worker/validation ad hoc codes are not core
// errors and were never in statusByCode — VALIDATION_ERROR keeps its 400,
// everything worker-side (SEARCH_UNAVAILABLE, SEARCH_WORKER_ERROR) keeps 502.
func writeRAGError(w http.ResponseWriter, errObj any) {
	switch e := errObj.(type) {
	case *engine.Err:
		writeErr(w, e)
	case map[string]any:
		status := http.StatusBadGateway
		if e["code"] == "VALIDATION_ERROR" {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, e)
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL", "message": "unexpected error shape"})
	}
}

// SearchPerimeter is the ONE governance core for perimeter RAG search
// (packs/knowvault/README.md §1.2, §2): filter to the actor's visible scope,
// optionally narrow to one workspace within that scope, proxy the question to
// the search worker, and journal a SearchQuery record — unless the worker
// errored (BUG-10: a search error must never produce a spurious SearchQuery,
// so it is logged distinctly from a valid empty result).
//
// Both the REST handler above (search) and internal/mcp's search_perimeter
// tool call this exact function — there is no second implementation of the
// permission boundary, the worker proxy, or the audit trail. The second
// return value is nil on success; otherwise it is either an *engine.Err (a
// real engine/permission error, e.g. from the visibility check) or a
// map[string]any ad hoc error (VALIDATION_ERROR / SEARCH_UNAVAILABLE /
// SEARCH_WORKER_ERROR) — callers type-switch on it exactly like
// writeRAGError and internal/mcp's dispatch already do for every other tool.
func SearchPerimeter(ctx context.Context, eng *engine.Engine, actor eventstore.Actor, cfg RAGConfig, question, workspace string, limit int) (map[string]any, any) {
	if question == "" {
		return nil, map[string]any{"code": "VALIDATION_ERROR",
			"message":  "search text is required",
			"fix_hint": "pass a non-empty question (REST /api/search) or query (MCP search_perimeter)"}
	}

	// permission boundary stays in core: only records this actor may read
	scopeIDs, err := eng.VisibleRecordIDs(ctx, actor, cfg.Scope)
	if err != nil {
		var e *engine.Err
		if errors.As(err, &e) {
			return nil, e
		}
		return nil, map[string]any{"code": "INTERNAL", "message": err.Error()}
	}
	// search one project if asked — but only if it is among the visible scope,
	// so a narrowed search can never reach beyond the actor's permissions
	if workspace != "" {
		narrowed := scopeIDs[:0]
		for _, id := range scopeIDs {
			if id == workspace {
				narrowed = append(narrowed, id)
			}
		}
		scopeIDs = narrowed
	}
	if len(scopeIDs) == 0 {
		return map[string]any{"answer": "No accessible workspaces.", "sources": []any{}}, nil
	}

	// delegate the heavy lifting to the worker
	payload := map[string]any{"question": question, "scope_ids": scopeIDs}
	if limit > 0 {
		payload["limit"] = limit
	}
	body, _ := json.Marshal(payload)
	reqCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	proxyReq, _ := http.NewRequestWithContext(reqCtx, "POST", cfg.Backend+"/search", bytes.NewReader(body))
	proxyReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		return nil, map[string]any{"code": "SEARCH_UNAVAILABLE",
			"message":  "search worker not reachable: " + err.Error(),
			"fix_hint": "the indexing/search service may be starting — try again shortly"}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var answer map[string]any
	_ = json.Unmarshal(raw, &answer)

	// BUG-10: worker signals failure with a non-200 status (HTTP 500 + "error": true).
	// A search error must be logged DISTINCTLY from a valid empty result (results=0).
	// Previously the worker returned HTTP 200 for errors, so node logs were ambiguous.
	if resp.StatusCode != http.StatusOK {
		errMsg := "search worker error"
		if m, ok := answer["message"].(string); ok && m != "" {
			errMsg = m
		}
		// Do NOT create a SearchQuery journal record — this is not a valid query result.
		return nil, map[string]any{
			"code":     "SEARCH_WORKER_ERROR",
			"message":  errMsg,
			"fix_hint": "check the search service logs for details",
		}
	}

	// journal the query as a record (provenance: who asked what — same for a
	// human via REST or an agent via MCP, actor.Role carries which)
	if cfg.LogEntity != "" {
		nResults := 0
		if srcs, ok := answer["sources"].([]any); ok {
			nResults = len(srcs)
		}
		_, _ = eng.Create(ctx, actor, cfg.LogEntity, map[string]any{
			"workspace": scopeIDs[0], "query": question, "actor_role": actor.Role, "results": nResults,
		}, &eventstore.Basis{Type: "human", ID: actor.ID}, "")
	}
	return answer, nil
}
