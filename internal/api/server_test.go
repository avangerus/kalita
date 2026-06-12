package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Week 3 DoD: CRUD of the collections pack over REST, permission matrix
// answered with proper HTTP statuses.

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	files := map[string]string{}
	for _, name := range []string{"pack.dsl", "collections.dsl"} {
		raw, err := os.ReadFile("../../examples/collections/" + name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(raw)
	}
	model, errs := dsl.Compile(files)
	if len(errs) > 0 {
		t.Fatalf("collections must compile: %v", errs[0])
	}
	eng, err := engine.New(context.Background(), model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	// dev-header identity is an explicit opt-in (P0): these tests exercise it
	srv := httptest.NewServer(Secure(New(eng, nil, WithDevHeaders())))
	t.Cleanup(srv.Close)
	return srv
}

type apiClient struct {
	t    *testing.T
	base string
	id   string
	role string
}

func (c *apiClient) do(method, path string, body any) (*http.Response, map[string]any) {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, c.base+path, &buf)
	req.Header.Set("X-Actor-Id", c.id)
	req.Header.Set("X-Actor-Role", c.role)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func mutate(values map[string]any) map[string]any {
	return map[string]any{
		"values": values,
		"basis":  map[string]any{"type": "human", "id": "anna"},
	}
}

func TestRESTEndToEnd(t *testing.T) {
	srv := newTestServer(t)
	anna := &apiClient{t, srv.URL, "anna", "Accountant", }
	bot := &apiClient{t, srv.URL, "collector-1", "Collector"}

	// no identity → 401 with hint
	resp, body := (&apiClient{t, srv.URL, "", ""}).do("GET", "/api/system", nil)
	if resp.StatusCode != http.StatusUnauthorized || body["fix_hint"] == "" {
		t.Fatalf("anonymous must get 401 with fix_hint, got %d %v", resp.StatusCode, body)
	}

	// describe
	resp, body = anna.do("GET", "/api/system", nil)
	if resp.StatusCode != 200 || body["pack"] != "collections" {
		t.Fatalf("describe failed: %d %v", resp.StatusCode, body)
	}

	// create contract + debtor
	resp, contract := anna.do("POST", "/api/records/Contract",
		mutate(map[string]any{"company": "Vector LLC", "due_date": "2026-05-01", "amount": 100000}))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create contract: %d %v", resp.StatusCode, contract)
	}
	contractID := contract["id"].(string)

	resp, debtor := anna.do("POST", "/api/records/Debtor",
		mutate(map[string]any{"company": "Vector LLC", "contract": contractID, "debt": 100000}))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create debtor: %d %v", resp.StatusCode, debtor)
	}
	debtorID := debtor["id"].(string)

	// validation over REST: 422 with field and hint
	resp, errBody := anna.do("POST", "/api/records/Debtor",
		mutate(map[string]any{"company": "X", "contract": "ghost"}))
	if resp.StatusCode != http.StatusUnprocessableEntity || errBody["fix_hint"] == "" {
		t.Fatalf("validation must be 422 with fix_hint: %d %v", resp.StatusCode, errBody)
	}

	// conflict over REST: unique(company, contract)
	resp, _ = anna.do("POST", "/api/records/Debtor",
		mutate(map[string]any{"company": "Vector LLC", "contract": contractID}))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate must be 409, got %d", resp.StatusCode)
	}

	// update
	resp, _ = anna.do("PATCH", "/api/records/Debtor/"+debtorID,
		mutate(map[string]any{"debt": 50000}))
	if resp.StatusCode != 200 {
		t.Fatalf("update: %d", resp.StatusCode)
	}

	// query with filter: the auto transition has already moved the debtor to
	// Overdue (due_date is in the past) — the workflow engine at work
	resp, list := anna.do("GET", "/api/records/Debtor?status=Overdue", nil)
	if resp.StatusCode != 200 || len(list["records"].([]any)) != 1 {
		t.Fatalf("query: %d %v", resp.StatusCode, list)
	}

	// collector: denied field update → 403 naming the rule
	resp, errBody = bot.do("PATCH", "/api/records/Debtor/"+debtorID,
		mutate(map[string]any{"debt": 0}))
	if resp.StatusCode != http.StatusForbidden || errBody["rule"] == "" {
		t.Fatalf("collector update debt must be 403 with rule, got %d %v", resp.StatusCode, errBody)
	}

	// collector reads debtors (allowed)
	resp, _ = bot.do("GET", "/api/records/Debtor/"+debtorID, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("collector read debtor: %d", resp.StatusCode)
	}

	// classified contract invisible: 404, not 403
	resp, classified := anna.do("POST", "/api/records/Contract",
		mutate(map[string]any{"company": "Secret Corp", "due_date": "2026-05-01", "classified": true}))
	if resp.StatusCode != http.StatusCreated {
		t.Fatal("create classified")
	}
	resp, _ = bot.do("GET", "/api/records/Contract/"+classified["id"].(string), nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("classified must be 404 for collector, got %d", resp.StatusCode)
	}

	// basis required → 422
	resp, _ = anna.do("POST", "/api/records/Contract",
		map[string]any{"values": map[string]any{"company": "NoBasis Inc", "due_date": "2026-01-01"}})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("mutation without basis must be 422, got %d", resp.StatusCode)
	}
}

// The audit/time-travel endpoints the UI's history panel drives: diff (read),
// revert and rewind (supervisory, gated on `full`).
func TestRevertRewindDiffREST(t *testing.T) {
	srv := newTestServer(t)
	anna := &apiClient{t, srv.URL, "anna", "Accountant"} // full [Debtor] → may revert
	bot := &apiClient{t, srv.URL, "collector-1", "Collector"}

	// meta exposes can_revert per entity (the UI shows the controls only when set)
	_, meta := anna.do("GET", "/api/meta", nil)
	debtorCanRevert := false
	for _, e := range meta["entities"].([]any) {
		if m := e.(map[string]any); m["name"] == "Debtor" {
			debtorCanRevert, _ = m["can_revert"].(bool)
		}
	}
	if !debtorCanRevert {
		t.Fatal("Accountant has full[Debtor] → Debtor.can_revert must be true")
	}

	_, c := anna.do("POST", "/api/records/Contract", mutate(map[string]any{"company": "Acme", "due_date": "2026-05-01"}))
	cid := mustCreated(t, c)
	_, d := anna.do("POST", "/api/records/Debtor", mutate(map[string]any{"company": "Acme", "contract": cid, "debt": 100000}))
	id := mustCreated(t, d)

	// find the creation seq
	_, j := anna.do("GET", "/api/records/Debtor/"+id+"/journal", nil)
	var createSeq float64
	for _, ev := range j["events"].([]any) {
		if e := ev.(map[string]any); e["kind"] == "record.created" {
			createSeq = e["seq"].(float64)
		}
	}

	// a genuine edit, then diff sees it net
	anna.do("PATCH", "/api/records/Debtor/"+id, mutate(map[string]any{"debt": 50000}))
	_, diff := anna.do("GET", fmt.Sprintf("/api/records/Debtor/%s/diff?from=%d", id, int(createSeq)), nil)
	sawDebt := false
	for _, f := range diff["fields"].([]any) {
		if fm := f.(map[string]any); fm["field"] == "debt" {
			sawDebt = true
		}
	}
	if !sawDebt {
		t.Fatalf("diff must report the debt change, got %v", diff["fields"])
	}

	// a collector (no full) may not revert → 403
	resp, _ := bot.do("POST", "/api/records/Debtor/"+id+"/revert", map[string]any{"basis": map[string]any{"type": "human", "id": "collector-1"}})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("collector revert must be 403, got %d", resp.StatusCode)
	}

	// anna reverts the last edit → debt back to 100000
	resp, _ = anna.do("POST", "/api/records/Debtor/"+id+"/revert", map[string]any{"basis": map[string]any{"type": "human", "id": "anna"}})
	if resp.StatusCode != 200 {
		t.Fatalf("revert: %d", resp.StatusCode)
	}
	_, got := anna.do("GET", "/api/records/Debtor/"+id, nil)
	if v := got["values"].(map[string]any); v["debt"].(float64) != 100000 {
		t.Fatalf("after revert debt = %v, want 100000", v["debt"])
	}

	// rewind to creation restores the original state
	resp, _ = anna.do("POST", "/api/records/Debtor/"+id+"/rewind",
		map[string]any{"to_seq": int(createSeq), "basis": map[string]any{"type": "human", "id": "anna"}})
	if resp.StatusCode != 200 {
		t.Fatalf("rewind: %d", resp.StatusCode)
	}
}

func mustCreated(t *testing.T, rec map[string]any) string {
	t.Helper()
	id, ok := rec["id"].(string)
	if !ok {
		t.Fatalf("expected a created record with an id, got %v", rec)
	}
	return id
}
