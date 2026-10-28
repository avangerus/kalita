package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// TestSearchPerimeter* — the search_perimeter MCP tool (packs/knowvault/README.md
// §1.2/§2) must run through the SAME governance core as the human REST
// /api/search endpoint (internal/api/ragproxy.go's SearchPerimeter): results
// filtered by VisibleRecordIDs BEFORE they come back, one SearchQuery
// journaled per successful call, and BUG-10 preserved — a worker error
// journals nothing. testSearchPack grants Searcher a row-scoped
// `read Workspace where owner = $me`, the ABAC mechanism (not a new tool
// grant) the fixture uses to give two Searcher actors disjoint visible
// workspaces, so the "cannot reach another actor's workspace" tests are
// real permission checks, not just distinct fixture ids.
const testSearchPack = `pack testsearch
version 0.1.0

entity Workspace:
    name: string required unique
    owner: string

entity SearchQuery:
    workspace: ref[Workspace] on_delete=restrict
    query: string required
    actor_role: string
    results: int

roles:
    Searcher agent
    Auditor

permissions:
    Searcher:
        read Workspace where owner = $me
        create [Workspace, SearchQuery]
        deny   [delete *, update Workspace.*]
    Auditor:
        read [Workspace, SearchQuery]
        deny [delete *, update Workspace.*, update SearchQuery.*, create *]
`

// searchWorker is a mock RAG worker recording the last request body it saw
// (so tests can assert scope_ids never carries a workspace the caller could
// not see) and toggleable to answer as a failure (BUG-10 acceptance).
type searchWorker struct {
	*httptest.Server
	mu       sync.Mutex
	lastBody map[string]any
	fail     bool
}

func newSearchWorker(t *testing.T) *searchWorker {
	t.Helper()
	sw := &searchWorker{}
	sw.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		sw.mu.Lock()
		sw.lastBody = body
		fail := sw.fail
		sw.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": true, "message": "vector store unreachable"})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"answer":  "here is what I found",
			"sources": []any{map[string]any{"title": "doc1", "url": "kv://doc1"}},
		})
	}))
	t.Cleanup(sw.Close)
	return sw
}

func (sw *searchWorker) body() map[string]any {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.lastBody
}

func (sw *searchWorker) setFail(v bool) {
	sw.mu.Lock()
	sw.fail = v
	sw.mu.Unlock()
}

// searchFixture boots an MCP node from testSearchPack, wired to a mock
// worker via WithRAGSearch, with two Searcher actors each owning one
// Workspace — searcher-a can see only ws-a, searcher-b only ws-b.
type searchFixture struct {
	mcpURL         string
	worker         *searchWorker
	tokenA, tokenB string
	auditorToken   string
	wsA, wsB       string
}

func newSearchFixture(t *testing.T) *searchFixture {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"testsearch.dsl": testSearchPack})
	if len(errs) > 0 {
		t.Fatalf("compile testSearchPack: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}

	tokenA, err := reg.RegisterWithToken(context.Background(), registrar,
		"searcher-a", eventstore.ActorAgent, "Searcher", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tokenB, err := reg.RegisterWithToken(context.Background(), registrar,
		"searcher-b", eventstore.ActorAgent, "Searcher", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	auditorToken, err := reg.RegisterWithToken(context.Background(), registrar,
		"auditor-1", eventstore.ActorHuman, "Auditor", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	eng, err := engine.New(context.Background(), model, store)
	if err != nil {
		t.Fatal(err)
	}

	worker := newSearchWorker(t)
	srv := httptest.NewServer(New(eng, reg, WithRAGSearch(worker.URL, "Workspace", "SearchQuery", "Searcher")))
	t.Cleanup(srv.Close)

	wsA, isErr := call(t, srv.URL, tokenA, "create_record", map[string]any{
		"entity": "Workspace", "values": map[string]any{"name": "ws-a", "owner": "searcher-a"},
		"basis": map[string]any{"type": "task", "id": "setup-a"},
	})
	if isErr {
		t.Fatalf("create Workspace ws-a: %v", wsA)
	}
	wsB, isErr := call(t, srv.URL, tokenB, "create_record", map[string]any{
		"entity": "Workspace", "values": map[string]any{"name": "ws-b", "owner": "searcher-b"},
		"basis": map[string]any{"type": "task", "id": "setup-b"},
	})
	if isErr {
		t.Fatalf("create Workspace ws-b: %v", wsB)
	}

	return &searchFixture{
		mcpURL: srv.URL, worker: worker,
		tokenA: tokenA, tokenB: tokenB, auditorToken: auditorToken,
		wsA: wsA["id"].(string), wsB: wsB["id"].(string),
	}
}

// searchQueryCount lets a test assert exactly how many SearchQuery records
// exist via the Auditor role's ordinary read grant — a black-box check
// (through the MCP query tool, not an engine-internal reach-around).
func (f *searchFixture) searchQueryCount(t *testing.T) []any {
	t.Helper()
	out, isErr := call(t, f.mcpURL, f.auditorToken, "query", map[string]any{"entity": "SearchQuery"})
	if isErr {
		t.Fatalf("query SearchQuery as Auditor: %v", out)
	}
	recs, _ := out["records"].([]any)
	return recs
}

// TestSearchPerimeterReturnsWorkerAnswerAndJournals: a Searcher scoped to its
// own workspace gets the worker's answer back verbatim (same body shape as
// REST /api/search would return) and exactly one SearchQuery is journaled,
// carrying the actor's own role and the visible workspace — provenance,
// point 4 of the contract.
func TestSearchPerimeterReturnsWorkerAnswerAndJournals(t *testing.T) {
	f := newSearchFixture(t)

	out, isErr := call(t, f.mcpURL, f.tokenA, "search_perimeter", map[string]any{
		"query": "find contract terms",
	})
	if isErr {
		t.Fatalf("search_perimeter must succeed: %v", out)
	}
	if out["answer"] != "here is what I found" {
		t.Fatalf("must return the worker's answer verbatim: %v", out)
	}
	srcs, _ := out["sources"].([]any)
	if len(srcs) != 1 {
		t.Fatalf("must return the worker's sources verbatim: %v", out)
	}

	body := f.worker.body()
	scopeIDs, _ := body["scope_ids"].([]any)
	if len(scopeIDs) != 1 || scopeIDs[0] != f.wsA {
		t.Fatalf("worker must see only the caller's visible workspace, got scope_ids=%v", scopeIDs)
	}

	recs := f.searchQueryCount(t)
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 SearchQuery journaled, got %d: %v", len(recs), recs)
	}
	rec := recs[0].(map[string]any)["values"].(map[string]any)
	if rec["query"] != "find contract terms" || rec["actor_role"] != "Searcher" || rec["workspace"] != f.wsA {
		t.Fatalf("unexpected SearchQuery record: %v", rec)
	}
}

// TestSearchPerimeterCannotReachOtherActorsWorkspace: naming another actor's
// workspace id directly must not leak it — the workspace filter narrows to
// the visible scope, so an out-of-scope id yields the same "no accessible
// workspaces" result as no access at all, the worker is never called, and no
// SearchQuery is journaled. This is the filtration-before-return contract
// (point 3): permission enforcement, not merely disjoint test fixtures.
func TestSearchPerimeterCannotReachOtherActorsWorkspace(t *testing.T) {
	f := newSearchFixture(t)

	out, isErr := call(t, f.mcpURL, f.tokenA, "search_perimeter", map[string]any{
		"query": "find contract terms", "workspace": f.wsB,
	})
	if isErr {
		t.Fatalf("an out-of-scope workspace must be a benign empty result, not a tool error: %v", out)
	}
	if out["answer"] != "No accessible workspaces." {
		t.Fatalf("must not fall back to the caller's own scope or leak ws-b: %v", out)
	}
	if f.worker.body() != nil {
		t.Fatalf("worker must never be called when the requested workspace is out of scope: %v", f.worker.body())
	}
	if recs := f.searchQueryCount(t); len(recs) != 0 {
		t.Fatalf("an out-of-scope search must not journal a SearchQuery, got %v", recs)
	}
}

// TestSearchPerimeterWorkerErrorDoesNotJournal is BUG-10's acceptance test
// replayed against the MCP path: a worker error must be reported as a tool
// error AND must not create a SearchQuery — an error is not a valid query
// result and must never be indistinguishable from one in the audit trail.
func TestSearchPerimeterWorkerErrorDoesNotJournal(t *testing.T) {
	f := newSearchFixture(t)
	f.worker.setFail(true)

	out, isErr := call(t, f.mcpURL, f.tokenA, "search_perimeter", map[string]any{
		"query": "find contract terms",
	})
	if !isErr || out["code"] != "SEARCH_WORKER_ERROR" {
		t.Fatalf("a worker error must surface as SEARCH_WORKER_ERROR: %v", out)
	}
	if recs := f.searchQueryCount(t); len(recs) != 0 {
		t.Fatalf("BUG-10: a worker error must journal no SearchQuery, got %v", recs)
	}
}

// TestSearchPerimeterValidation: an empty query is rejected before anything
// else runs (no worker call, no journal) — same as REST's empty-question 400.
func TestSearchPerimeterValidation(t *testing.T) {
	f := newSearchFixture(t)

	out, isErr := call(t, f.mcpURL, f.tokenA, "search_perimeter", map[string]any{})
	if !isErr || out["code"] != "VALIDATION_ERROR" {
		t.Fatalf("empty query must be VALIDATION_ERROR: %v", out)
	}
	if f.worker.body() != nil {
		t.Fatalf("worker must never be called for an invalid request: %v", f.worker.body())
	}
}

// TestSearchPerimeterNotConfigured: a node started without WithRAGSearch
// answers NOT_CONFIGURED rather than panicking on a nil rag pointer — most
// kalita nodes have no search backend at all, and describe_system/tools/list
// still advertises the tool (it is in the closed toolDefs list unconditionally),
// so a caller must get a clean, self-correcting error rather than a crash.
func TestSearchPerimeterNotConfigured(t *testing.T) {
	model, errs := dsl.Compile(map[string]string{"testsearch.dsl": testSearchPack})
	if len(errs) > 0 {
		t.Fatalf("compile testSearchPack: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	token, err := reg.RegisterWithToken(context.Background(), registrar,
		"searcher-a", eventstore.ActorAgent, "Searcher", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(context.Background(), model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg)) // no WithRAGSearch
	t.Cleanup(srv.Close)

	out, isErr := call(t, srv.URL, token, "search_perimeter", map[string]any{"query": "anything"})
	if !isErr || out["code"] != "NOT_CONFIGURED" {
		t.Fatalf("search_perimeter without WithRAGSearch must be NOT_CONFIGURED: %v", out)
	}
}
