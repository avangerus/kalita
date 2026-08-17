package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Milestone B exit criterion (B2): the reference scenario System X → Kalita →
// approval → System Y survives a temporary Y error, retries, a duplicate
// delivery attempt, and a Kalita restart — WITHOUT a double business effect.
// The mock Y dedups by the deterministic idempotency_key, exactly as the real
// payment endpoint must (owner decision G2: "business effect обязан быть
// idempotent").

// mockPayment is a reference "system Y": it records every POST it receives and
// counts DISTINCT idempotency keys (one key = one business effect). failNext
// makes it return 500 for the next N requests, simulating a temporary outage.
type mockPayment struct {
	mu       sync.Mutex
	failNext int
	effects  map[string]int // idempotency_key -> received count
	total    int
}

func newMockPayment() *mockPayment { return &mockPayment{effects: map[string]int{}} }

func (m *mockPayment) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.total++
	if m.failNext > 0 {
		m.failNext--
		m.mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if k, _ := body["idempotency_key"].(string); k != "" {
		m.effects[k]++
	}
	m.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (m *mockPayment) distinctEffects() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.effects)
}

func (m *mockPayment) setFail(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failNext = n
}

const webhookB2Pack = `
entity Doc:
    title: string

roles:
    Owner

permissions:
    Owner:
        full [Doc]

automation:
    on create Doc:
        webhook out "%s/hook"
`

func webhookB2Engine(t *testing.T, yURL string) (*engine.Engine, eventstore.Store) {
	t.Helper()
	pack := fmt.Sprintf(webhookB2Pack, yURL)
	model, errs := dsl.Compile(map[string]string{"b2.dsl": pack})
	if len(errs) > 0 {
		t.Fatalf("pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	e, err := engine.New(context.Background(), model, store)
	if err != nil {
		t.Fatal(err)
	}
	return e, store
}

func b2Host(t *testing.T, yURL string) string {
	t.Helper()
	u, err := url.Parse(yURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Hostname()
}

// TestReferenceScenarioSurvivesOutageRetryDuplicateRestart is the Milestone B
// exit test: one business effect through a real HTTP delivery, a temporary
// outage, a duplicate delivery attempt, and a node restart.
func TestReferenceScenarioSurvivesOutageRetryDuplicateRestart(t *testing.T) {
	shortenBackoff(t)
	y := newMockPayment()
	srv := httptest.NewServer(y)
	defer srv.Close()

	// temporary outage on the FIRST delivery attempt (Y down when the runner
	// first tries), then recovers
	y.setFail(1)

	e, store := webhookB2Engine(t, srv.URL)
	domains := []string{b2Host(t, srv.URL)}
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}

	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	taskID := e.OpenWebhookTasks()[0].ID

	// first runner pass: Y fails once, retry succeeds — one distinct effect
	n := runWebhookDeliveries(context.Background(), e, domains, deliverOnce)
	if n != 1 {
		t.Fatalf("runner processed %d tasks, want 1", n)
	}
	if got := y.distinctEffects(); got != 1 {
		t.Fatalf("want exactly ONE business effect after outage+retry, got %d", got)
	}

	// duplicate delivery attempt: nothing is open, so no second effect
	if got := runWebhookDeliveries(context.Background(), e, domains, deliverOnce); got != 0 {
		t.Fatalf("re-running the runner must process 0 tasks, got %d", got)
	}
	if got := y.distinctEffects(); got != 1 {
		t.Fatalf("duplicate attempt must not add an effect, got %d", got)
	}

	// node restart: replay from the journal — the settled task is gone
	e2, err := engine.New(context.Background(), e.Model(), store)
	if err != nil {
		t.Fatal(err)
	}
	if got := e2.OpenWebhookTasks(); len(got) != 0 {
		t.Fatalf("after restart the delivered task must not reappear, got %d", len(got))
	}

	// the same idempotency_key was used across the failed and successful attempt
	y.mu.Lock()
	count, ok := y.effects["webhook:"+taskID]
	y.mu.Unlock()
	if !ok || count != 1 {
		t.Fatalf("mock Y must have recorded exactly one receipt for key webhook:%s (count=%d)", taskID, count)
	}
}
