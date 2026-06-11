package api

import (
	"bytes"
	"context"
	"encoding/json"
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
	for _, name := range []string{"pack.kal", "collections.kal"} {
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
	srv := httptest.NewServer(New(eng))
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

	// query with filter
	resp, list := anna.do("GET", "/api/records/Debtor?status=OnTime", nil)
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
