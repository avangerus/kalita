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

// Milestone B exit criterion (B2, English-first release): the reference
// scenario System X → Kalita → approval → System Y survives a temporary Y
// error, retries, a duplicate delivery attempt, and a Kalita restart —
// WITHOUT a double business effect. The mock Y dedups by the per-firing
// delivery identity carried as idempotency_key, exactly as the real payment
// endpoint must (at-least-once + idempotent effect; exactly-once is never
// promised).

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
	return webhookB2EngineOver(t, pack, eventstore.NewMemStore(nil))
}

// webhookB2EngineOver builds the engine over a caller-supplied store (the
// failure-injection harness's crash-emulating store wrapper uses this).
func webhookB2EngineOver(t *testing.T, pack string, store eventstore.Store) (*engine.Engine, eventstore.Store) {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"b2.dsl": pack})
	if len(errs) > 0 {
		t.Fatalf("pack must compile: %v", errs[0])
	}
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
	task := e.OpenWebhookTasks()[0]
	deliveryID := task.DeliveryID
	if deliveryID == "" {
		t.Fatal("a hardened webhook task must carry a per-firing DeliveryID")
	}

	// first runner pass: Y fails once, retry succeeds — one distinct effect
	n := runWebhookDeliveries(context.Background(), e, domains, nil, false, deliverOnce)
	if n != 1 {
		t.Fatalf("runner processed %d tasks, want 1", n)
	}
	if got := y.distinctEffects(); got != 1 {
		t.Fatalf("want exactly ONE business effect after outage+retry, got %d", got)
	}

	// duplicate delivery attempt: nothing is open, so no second effect
	if got := runWebhookDeliveries(context.Background(), e, domains, nil, false, deliverOnce); got != 0 {
		t.Fatalf("re-running the runner must process 0 tasks, got %d", got)
	}
	if got := y.distinctEffects(); got != 1 {
		t.Fatalf("duplicate attempt must not add an effect, got %d", got)
	}

	// node restart: replay from the journal — the settled task is gone AND
	// the per-firing delivery identity survived the replay fold.
	e2, err := engine.New(context.Background(), e.Model(), store)
	if err != nil {
		t.Fatal(err)
	}
	if got := e2.OpenWebhookTasks(); len(got) != 0 {
		t.Fatalf("after restart the delivered task must not reappear, got %d", len(got))
	}
	if got := e2.WebhookReceipts(); len(got) < 1 {
		t.Fatalf("delivery receipts must survive the restart (journal replay), got %d", len(got))
	}

	// the same idempotency_key was used across the failed and successful attempt
	y.mu.Lock()
	count, ok := y.effects["webhook:"+deliveryID]
	y.mu.Unlock()
	if !ok || count != 1 {
		t.Fatalf("mock Y must have recorded exactly one receipt for key webhook:%s (count=%d)", deliveryID, count)
	}
}

// TestDeliveryCarriesIdempotencyKeyAcrossManualRetry: after the delivery
// dead-letters, the operator's DLQ retry redelivers under the SAME delivery
// identity — the receiver collapses it to the same one business effect.
func TestDeliveryCarriesIdempotencyKeyAcrossManualRetry(t *testing.T) {
	shortenBackoff(t)
	y := newMockPayment()
	srv := httptest.NewServer(y)
	defer srv.Close()

	e, _ := webhookB2Engine(t, srv.URL)
	domains := []string{b2Host(t, srv.URL)}
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	first := e.OpenWebhookTasks()[0]

	// Y stays down the whole pass: the delivery exhausts retries and lands in the DLQ
	y.setFail(webhookAttempts)
	if n := runWebhookDeliveries(context.Background(), e, domains, nil, false, deliverOnce); n != 1 {
		t.Fatalf("runner processed %d tasks, want 1", n)
	}
	if got := y.distinctEffects(); got != 0 {
		t.Fatalf("Y was down: no effect yet, got %d", got)
	}
	dlq := e.FailedWebhookTasks()
	if len(dlq) != 1 {
		t.Fatalf("want one dead-lettered task, got %d", len(dlq))
	}

	// the operator retries from the DLQ: same DeliveryID is carried forward
	retried, err := e.RetryWebhookTask(context.Background(), owner, dlq[0].ID, &eventstore.Basis{Type: "human", ID: "o"})
	if err != nil {
		t.Fatal(err)
	}
	if retried.DeliveryID != first.DeliveryID {
		t.Fatalf("retry must carry the same DeliveryID forward: %q vs %q", retried.DeliveryID, first.DeliveryID)
	}
	if got := y.distinctEffects(); got != 0 {
		t.Fatalf("retry task creation must not deliver by itself, got %d effects", got)
	}

	// Y recovered: the retry delivers and the receiver sees ONE business effect
	if n := runWebhookDeliveries(context.Background(), e, domains, nil, false, deliverOnce); n != 1 {
		t.Fatalf("retry pass processed %d tasks, want 1", n)
	}
	if got := y.distinctEffects(); got != 1 {
		t.Fatalf("the original delivery and its DLQ retry must collapse to ONE effect, got %d", got)
	}
	y.mu.Lock()
	count, ok := y.effects["webhook:"+first.DeliveryID]
	y.mu.Unlock()
	if !ok || count != 1 {
		t.Fatalf("mock Y must record exactly one receipt for webhook:%s (count=%d)", first.DeliveryID, count)
	}
}
