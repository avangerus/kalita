package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/avangerus/kalita/internal/eventstore"
)

// RAG search proxy: gives the UI an HTTP search endpoint WITHOUT putting any
// embedding/vector/LLM knowledge in the core. The node owns the two things a
// worker must not be trusted with — the permission boundary and the audit —
// and delegates the heavy lifting to a configured search worker.
//
// Flow: authenticate -> compute the record ids the actor may read of the
// scope entity (e.g. Workspace) -> POST {question, scope_ids} to the worker
// -> journal the query as a record -> return the worker's answer.
//
// Generic by config (knowvault is just the first user):
//   --search-backend  worker URL
//   --search-scope    entity whose visible records bound the search (Workspace)
//   --search-log      entity to journal each query into (SearchQuery)

type ragConfig struct {
	Backend  string
	Scope    string // entity bounding visibility (e.g. Workspace)
	LogEntity string // entity to record queries into (e.g. SearchQuery)
	Role     string // role written into the log record
}

func (s *Server) enableRAG(cfg ragConfig) {
	s.rag = &cfg
	s.mux.HandleFunc("POST /api/search", s.search)
}

type searchRequest struct {
	Question string `json:"question"`
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.actor(r)
	if !ok {
		writeAuthRequired(w)
		return
	}
	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Question == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "VALIDATION_ERROR",
			"message": "question is required", "fix_hint": `send {"question": "..."}`})
		return
	}

	// permission boundary stays in core: only records this actor may read
	scopeIDs, err := s.eng.VisibleRecordIDs(r.Context(), actor, s.rag.Scope)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(scopeIDs) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"answer": "No accessible workspaces.", "sources": []any{}})
		return
	}

	// delegate the heavy lifting to the worker
	body, _ := json.Marshal(map[string]any{"question": req.Question, "scope_ids": scopeIDs})
	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()
	proxyReq, _ := http.NewRequestWithContext(ctx, "POST", s.rag.Backend+"/search", bytes.NewReader(body))
	proxyReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"code": "SEARCH_UNAVAILABLE",
			"message": "search worker not reachable: " + err.Error(),
			"fix_hint": "the indexing/search service may be starting — try again shortly"})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var answer map[string]any
	_ = json.Unmarshal(raw, &answer)

	// journal the query as a record (provenance: who asked what)
	if s.rag.LogEntity != "" {
		nResults := 0
		if srcs, ok := answer["sources"].([]any); ok {
			nResults = len(srcs)
		}
		_, _ = s.eng.Create(r.Context(), actor, s.rag.LogEntity, map[string]any{
			"workspace": scopeIDs[0], "query": req.Question, "actor_role": actor.Role, "results": nResults,
		}, &eventstore.Basis{Type: "human", ID: actor.ID}, "")
	}
	writeJSON(w, http.StatusOK, answer)
}
