package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
)

// The D-011 failure-injection acceptance harness: scenarios A–H from
// docs/ACCEPTANCE.md, each named exactly as in the plan, each
// run against a controlled mock destination Y (mockPayment, above in
// webhooks_integration_test.go). Every scenario asserts the DELIVERY CONTRACT,
// not implementation details: at-least-once, idempotent business effect,
// fail-closed egress, secrets never weakening auth, restart safety.

// dropOnceStore wraps the journal and drops the next append of a given kind —
// the honest emulation of a node crash BETWEEN send and settle: the delivery
// went out, but the settle event never reached the journal. The engine's live
// projection still believes it settled (exactly as a real process would right
// before dying); the journal — the only durable truth — disagrees.
type dropOnceStore struct {
	eventstore.Store
	mu   sync.Mutex
	kind eventstore.Kind
	left int
}

func (d *dropOnceStore) dropNext(kind eventstore.Kind, n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.kind, d.left = kind, n
}

func (d *dropOnceStore) Append(ctx context.Context, in eventstore.AppendInput) (*eventstore.Event, error) {
	d.mu.Lock()
	if d.left > 0 && in.Kind == d.kind {
		d.left--
		d.mu.Unlock()
		// settle paths ignore the returned event; a plausible dummy suffices
		return &eventstore.Event{EventID: "dropped-" + fmt.Sprint(time.Now().UnixNano()), Kind: in.Kind, Subject: in.Subject, TS: time.Now()}, nil
	}
	d.mu.Unlock()
	return d.Store.Append(ctx, in)
}

func newMockY(t *testing.T) (*mockPayment, *httptest.Server, []string) {
	t.Helper()
	y := newMockPayment()
	srv := httptest.NewServer(y)
	t.Cleanup(srv.Close)
	return y, srv, []string{b2Host(t, srv.URL)}
}

// Scenario A: the mock Y answers 500 on the first attempt — the runner
// classifies it retryable, retries, and the receiver collapses both attempts
// to ONE business effect (one distinct idempotency key).
func TestFailInjectA_Transient500(t *testing.T) {
	shortenBackoff(t)
	y, srv, domains := newMockY(t)
	y.setFail(1)

	e, _ := webhookB2Engine(t, srv.URL)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	task := e.OpenWebhookTasks()[0]

	if n := runWebhookDeliveries(context.Background(), e, domains, nil, false, deliverOnce); n != 1 {
		t.Fatalf("processed %d tasks, want 1", n)
	}
	if got := y.distinctEffects(); got != 1 {
		t.Fatalf("A: want ONE business effect after 500+retry, got %d", got)
	}
	y.mu.Lock()
	count := y.effects["webhook:"+task.DeliveryID]
	y.mu.Unlock()
	if count != 1 {
		t.Fatalf("A: Y must record exactly one POST for the delivery identity, got %d", count)
	}
	if len(e.OpenWebhookTasks()) != 0 {
		t.Fatal("A: the delivery must be settled")
	}
}

// Scenario B: the destination times out (deadline exceeded) — classified as a
// retryable transport failure, retried, and settled on success.
func TestFailInjectB_Timeout(t *testing.T) {
	shortenBackoff(t)
	e, _ := webhookRunnerEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	n := runWebhookDeliveries(context.Background(), e, []string{"example.com"}, nil, false,
		func(_ context.Context, _ string, _ []byte, _ string) *webhookDeliveryError {
			attempts++
			if attempts < 3 {
				return &webhookDeliveryError{category: "timeout", err: context.DeadlineExceeded}
			}
			return nil
		})
	if n != 1 || attempts != 3 {
		t.Fatalf("B: want 3 attempts (2 timeouts + success), got %d (processed %d)", attempts, n)
	}
	if len(e.OpenWebhookTasks()) != 0 {
		t.Fatal("B: the delivery must settle after the timeout storm")
	}
}

// Scenario C: a duplicate runner pass over an already-settled delivery makes
// NO second request — nothing is open, the effect count cannot grow.
func TestFailInjectC_DuplicateDelivery(t *testing.T) {
	y, srv, domains := newMockY(t)
	e, _ := webhookB2Engine(t, srv.URL)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	if n := runWebhookDeliveries(context.Background(), e, domains, nil, false, deliverOnce); n != 1 {
		t.Fatalf("first pass processed %d, want 1", n)
	}
	before := y.distinctEffects()
	for i := 0; i < 3; i++ {
		if n := runWebhookDeliveries(context.Background(), e, domains, nil, false, deliverOnce); n != 0 {
			t.Fatalf("duplicate pass %d processed %d tasks, want 0", i, n)
		}
	}
	if got := y.distinctEffects(); got != before || got != 1 {
		t.Fatalf("C: duplicates must not grow effects: %d -> %d", before, got)
	}
}

// Scenario D: node restart with a still-open delivery — the intent survives
// the journal replay and the redelivery carries the SAME per-firing identity.
func TestFailInjectD_NodeRestart(t *testing.T) {
	y, srv, domains := newMockY(t)
	e, store := webhookB2Engine(t, srv.URL)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	first := e.OpenWebhookTasks()[0]

	// "crash" before the runner ever fires: a fresh engine over the same journal
	e2, err := engine.New(context.Background(), e.Model(), store)
	if err != nil {
		t.Fatal(err)
	}
	again := e2.OpenWebhookTasks()
	if len(again) != 1 || again[0].DeliveryID != first.DeliveryID {
		t.Fatalf("D: the open delivery must survive the restart under the same identity: %+v", again)
	}
	if n := runWebhookDeliveries(context.Background(), e2, domains, nil, false, deliverOnce); n != 1 {
		t.Fatalf("D: restarted node processed %d, want 1", n)
	}
	if got := y.distinctEffects(); got != 1 {
		t.Fatalf("D: want one effect after restart delivery, got %d", got)
	}
}

// Scenario E: crash BETWEEN send and settle — Y got the POST, the settle event
// never reached the journal. On restart the delivery is still open; the
// redelivery repeats the SAME idempotency key and the receiver collapses it:
// two POSTs, ONE business effect. Exactly-once is never promised; at-least-once
// with an idempotent effect is what the contract delivers.
func TestFailInjectE_CrashBetweenSendAndSettle(t *testing.T) {
	shortenBackoff(t)
	y, srv, domains := newMockY(t)

	wrapped := &dropOnceStore{Store: eventstore.NewMemStore(nil)}
	pack := fmt.Sprintf(webhookB2Pack, srv.URL)
	e, store := webhookB2EngineOver(t, pack, wrapped)

	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	first := e.OpenWebhookTasks()[0]

	// the settle event will be lost: send succeeded, journal never hears about it
	wrapped.dropNext(eventstore.TaskCompleted, 1)
	if n := runWebhookDeliveries(context.Background(), e, domains, nil, false, deliverOnce); n != 1 {
		t.Fatalf("E: first pass processed %d, want 1", n)
	}
	y.mu.Lock()
	posts := y.total
	y.mu.Unlock()
	if posts != 1 {
		t.Fatalf("E: Y must have received the POST before the crash, got %d", posts)
	}

	// restart: the journal still shows the delivery OPEN
	e2, err := engine.New(context.Background(), e.Model(), store)
	if err != nil {
		t.Fatal(err)
	}
	open := e2.OpenWebhookTasks()
	if len(open) != 1 || open[0].DeliveryID != first.DeliveryID {
		t.Fatalf("E: after restart the delivery must be open under the same identity: %+v", open)
	}
	if n := runWebhookDeliveries(context.Background(), e2, domains, nil, false, deliverOnce); n != 1 {
		t.Fatalf("E: redelivery pass processed %d, want 1", n)
	}
	if got := y.distinctEffects(); got != 1 {
		t.Fatalf("E: two POSTs must collapse to ONE business effect, got %d distinct keys", got)
	}
	y.mu.Lock()
	count := y.effects["webhook:"+first.DeliveryID]
	y.mu.Unlock()
	if count != 2 {
		t.Fatalf("E: Y must record exactly two POSTs for the identity (at-least-once), got %d", count)
	}
}

// Scenario F: persistent failure dead-letters the delivery; the operator's
// DLQ recovery redelivers under the SAME identity and the receiver applies
// the effect exactly once overall.
func TestFailInjectF_DLQRecovery(t *testing.T) {
	shortenBackoff(t)
	y, srv, domains := newMockY(t)
	y.setFail(webhookAttempts) // Y stays down the whole first pass

	e, _ := webhookB2Engine(t, srv.URL)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	if n := runWebhookDeliveries(context.Background(), e, domains, nil, false, deliverOnce); n != 1 {
		t.Fatalf("F: first pass processed %d, want 1", n)
	}
	dlq := e.FailedWebhookTasks()
	if len(dlq) != 1 {
		t.Fatalf("F: want one dead-lettered delivery, got %d", len(dlq))
	}
	attempts := 0
	for _, r := range e.WebhookReceipts() {
		if r.TaskID == dlq[0].ID {
			attempts++
		}
	}
	if attempts != webhookAttempts {
		t.Fatalf("F: want %d attempt receipts, got %d", webhookAttempts, attempts)
	}

	retried, err := e.RetryWebhookTask(context.Background(), owner, dlq[0].ID, &eventstore.Basis{Type: "human", ID: "o"})
	if err != nil {
		t.Fatal(err)
	}
	if retried.DeliveryID != dlq[0].DeliveryID {
		t.Fatalf("F: recovery must keep the delivery identity")
	}
	if n := runWebhookDeliveries(context.Background(), e, domains, nil, false, deliverOnce); n != 1 {
		t.Fatalf("F: recovery pass processed %d, want 1", n)
	}
	if got := y.distinctEffects(); got != 1 {
		t.Fatalf("F: want exactly ONE business effect after recovery, got %d", got)
	}
	// the DLQ lists what still needs a decision: the recovered delivery leaves
	// it (its history stays in the journal and the attempt receipts), and the
	// superseded original cannot be retried a second time
	if got := len(e.FailedWebhookTasks()); got != 0 {
		t.Fatalf("F: a recovered delivery must leave the DLQ, %d entries remain", got)
	}
	if e.EgressMetrics(time.Now()).DeadLettered != 0 {
		t.Fatal("F: kalita_egress_dead_lettered must not count a recovered delivery")
	}
	if _, err := e.RetryWebhookTask(context.Background(), owner, dlq[0].ID, &eventstore.Basis{Type: "human", ID: "o"}); err == nil {
		t.Fatal("F: retrying the superseded original must be refused")
	}
}

// Scenario G: a destination outside KALITA_EGRESS_DOMAINS is refused
// fail-closed — no request is ever made, the delivery dead-letters with an
// allowlist receipt.
func TestFailInjectG_AllowlistDenyFailClosed(t *testing.T) {
	y, srv, _ := newMockY(t)
	// domains deliberately DO NOT include Y's host
	e, _ := webhookB2Engine(t, srv.URL)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	n := runWebhookDeliveries(context.Background(), e, []string{"allowed.example.com"}, nil, false, deliverOnce)
	if n != 1 {
		t.Fatalf("G: processed %d, want 1", n)
	}
	y.mu.Lock()
	posts := y.total
	y.mu.Unlock()
	if posts != 0 {
		t.Fatalf("G: the denied destination must receive ZERO requests, got %d", posts)
	}
	if len(e.FailedWebhookTasks()) != 1 {
		t.Fatal("G: the denied delivery must dead-letter")
	}
	receipts := e.WebhookReceipts()
	if len(receipts) != 1 || receipts[0].Category != "allowlist" {
		t.Fatalf("G: want one allowlist receipt, got %+v", receipts)
	}
}

// Scenario H: runtime-injected destination secrets — a missing credential
// fails SAFELY (no unauthenticated send) when the operator configured a
// secrets file, and a present one rides the Authorization header.
func TestFailInjectH_SecretsFailSafe(t *testing.T) {
	e, _ := webhookRunnerEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}

	calls := 0
	secrets := map[string]string{"other.example.com": "Bearer sk_other"}
	n := runWebhookDeliveries(context.Background(), e, []string{"example.com"}, secrets, true,
		func(_ context.Context, _ string, _ []byte, _ string) *webhookDeliveryError { calls++; return nil })
	if n != 1 || calls != 0 {
		t.Fatalf("H: missing credential must fail closed: processed=%d calls=%d", n, calls)
	}
	receipts := e.WebhookReceipts()
	if len(receipts) != 1 || receipts[0].Category != "auth_missing" {
		t.Fatalf("H: want one auth_missing receipt, got %+v", receipts)
	}

	// the failed task dead-lettered; retry it with the credential now present
	dlq := e.FailedWebhookTasks()
	secrets = map[string]string{"example.com": "Bearer sk_live_test"}
	var gotBearer string
	retried, err := e.RetryWebhookTask(context.Background(), owner, dlq[0].ID, &eventstore.Basis{Type: "human", ID: "o"})
	if err != nil {
		t.Fatal(err)
	}
	_ = retried
	n = runWebhookDeliveries(context.Background(), e, []string{"example.com"}, secrets, true,
		func(_ context.Context, _ string, _ []byte, bearer string) *webhookDeliveryError {
			gotBearer = bearer
			return nil
		})
	if n != 1 || gotBearer != "Bearer sk_live_test" {
		t.Fatalf("H: the retry must deliver WITH the injected credential, got %q (processed %d)", gotBearer, n)
	}
}
