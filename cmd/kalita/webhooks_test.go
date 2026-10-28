package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Cycle-B1 (D-011, English-first release) — the node's webhook runner:
// whitelist normalization and enforcement, at-least-once delivery with the
// per-firing idempotency identity, failure classification, per-attempt
// receipts, runtime-secret injection, and settle semantics. deliver is
// injected; no network.

const webhookRunnerPack = `
entity Doc:
    title: string

roles:
    Owner

permissions:
    Owner:
        full [Doc]

automation:
    on create Doc:
        webhook out "https://example.com/hook"
`

func webhookRunnerEngine(t *testing.T) (*engine.Engine, eventstore.Store) {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"w.dsl": webhookRunnerPack})
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

// shortenBackoff shrinks the retry sleep for the slow tests.
func shortenBackoff(t *testing.T) {
	t.Helper()
	old := webhookBackoff
	webhookBackoff = 5 * time.Millisecond
	t.Cleanup(func() { webhookBackoff = old })
}

func TestNormalizeHost(t *testing.T) {
	cases := map[string]string{
		"Example.COM":     "example.com",
		"example.com.":    "example.com",
		"EXAMPLE.COM:443": "example.com",
		"":                "",
	}
	for in, want := range cases {
		if got := normalizeHost(in); got != want {
			t.Fatalf("normalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAllowed(t *testing.T) {
	domains := []string{"example.com"}
	if allowed(nil, "https://example.com/hook") {
		t.Fatal("empty allowlist must deny everything (fail-closed)")
	}
	if !allowed(domains, "https://Example.COM./hook") {
		t.Fatal("normalized host must match")
	}
	if allowed(domains, "https://example.com.evil.com/hook") {
		t.Fatal("prefix lookalike must NOT match")
	}
	if allowed(domains, "https://evil.com/hook") {
		t.Fatal("other domain must not match")
	}
	if allowed(domains, "://bad-url") {
		t.Fatal("unparseable url must deny")
	}
}

// TestAllowedSSRFEdgeCases pins the fail-closed behavior for host-form edge
// cases (Security Pro NIT-7): none of these may ever match the allowlist.
func TestAllowedSSRFEdgeCases(t *testing.T) {
	domains := []string{"example.com"}
	cases := []string{
		"https://user:pass@example.com/hook",  // userinfo
		"https://127.0.0.1/hook",              // ipv4 literal
		"https://[::1]/hook",                  // ipv6 literal
		"https://example.com\\@evil.com/hook", // backslash trick
		"https://%65xample.com/hook",          // percent-encoded host
		"https://пример.com/hook",             // IDN unicode form
	}
	for _, raw := range cases {
		if allowed(domains, raw) {
			t.Fatalf("allowlist must deny %q (fail-closed)", raw)
		}
	}
	// the allowlist is HOST-scoped by design: a different port on an
	// allowlisted host is still that host (Security Pro MINOR-1 documents
	// this as an over-grant vs origin pinning, not a bypass)
	if !allowed(domains, "https://example.com:8443/hook") {
		t.Fatal("host-only allowlist must match any port on an allowlisted host")
	}
}

// TestRunnerRejectsCleartextWithSecret: a credentialed delivery must never
// ride plain http (Security Pro MINOR-1 cleartext sub-case).
func TestRunnerRejectsCleartextWithSecret(t *testing.T) {
	const pack = `
entity Doc:
    title: string

roles:
    Owner

permissions:
    Owner:
        full [Doc]

automation:
    on create Doc:
        webhook out "http://example.com/hook"
`
	model, errs := dsl.Compile(map[string]string{"w.dsl": pack})
	if len(errs) > 0 {
		t.Fatalf("pack must compile: %v", errs[0])
	}
	e, err := engine.New(context.Background(), model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}

	calls := 0
	n := runWebhookDeliveries(context.Background(), e, []string{"example.com"},
		map[string]string{"example.com": "Bearer sk_x"}, true,
		func(_ context.Context, _ string, _ []byte, _ string) *webhookDeliveryError { calls++; return nil })
	if n != 1 || calls != 0 {
		t.Fatalf("cleartext credentialed delivery must fail closed: processed=%d calls=%d", n, calls)
	}
	receipts := e.WebhookReceipts()
	if len(receipts) != 1 || receipts[0].Category != "insecure_transport" {
		t.Fatalf("want one insecure_transport receipt, got %+v", receipts)
	}
	if len(e.FailedWebhookTasks()) != 1 {
		t.Fatal("the delivery must dead-letter")
	}
}

// TestRunnerRejectsUserinfoURL: a URL smuggling its own credentials must not
// be delivered (the runtime-secret channel is the only credential path).
func TestRunnerRejectsUserinfoURL(t *testing.T) {
	const pack = `
entity Doc:
    title: string

roles:
    Owner

permissions:
    Owner:
        full [Doc]

automation:
    on create Doc:
        webhook out "https://user:pass@example.com/hook"
`
	model, errs := dsl.Compile(map[string]string{"w.dsl": pack})
	if len(errs) > 0 {
		t.Fatalf("pack must compile: %v", errs[0])
	}
	e, err := engine.New(context.Background(), model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}

	calls := 0
	n := runWebhookDeliveries(context.Background(), e, []string{"example.com"}, nil, false,
		func(_ context.Context, _ string, _ []byte, _ string) *webhookDeliveryError { calls++; return nil })
	if n != 1 || calls != 0 {
		t.Fatalf("userinfo URL must fail closed: processed=%d calls=%d", n, calls)
	}
	receipts := e.WebhookReceipts()
	if len(receipts) != 1 || receipts[0].Category != "url_credentials" {
		t.Fatalf("want one url_credentials receipt, got %+v", receipts)
	}
}

// TestRunnerSettlesDelivery: the runner delivers once with a payload carrying
// the per-firing delivery identity (DeliveryID) as idempotency_key, journals
// a receipt, and settles the task completed.
func TestRunnerSettlesDelivery(t *testing.T) {
	e, _ := webhookRunnerEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	task := e.OpenWebhookTasks()[0]

	var mu sync.Mutex
	var gotURL string
	var gotBearer string
	var gotPayload map[string]any
	n := runWebhookDeliveries(context.Background(), e, []string{"example.com"}, nil, false,
		func(_ context.Context, url string, payload []byte, bearer string) *webhookDeliveryError {
			mu.Lock()
			defer mu.Unlock()
			gotURL = url
			gotBearer = bearer
			_ = json.Unmarshal(payload, &gotPayload)
			return nil
		})
	if n != 1 {
		t.Fatalf("runner must process 1 task, got %d", n)
	}
	if gotURL != "https://example.com/hook" {
		t.Fatalf("delivered url = %q", gotURL)
	}
	if gotPayload["idempotency_key"] != "webhook:"+task.DeliveryID {
		t.Fatalf("idempotency_key = %v, want webhook:%s", gotPayload["idempotency_key"], task.DeliveryID)
	}
	if gotPayload["record_id"] == nil {
		t.Fatal("payload must carry the record id")
	}
	if gotBearer != "" {
		t.Fatalf("no secrets configured: bearer must be empty, got %q", gotBearer)
	}
	if len(e.OpenWebhookTasks()) != 0 {
		t.Fatal("delivered task must be settled (left the open queue)")
	}
	receipts := e.WebhookReceipts()
	if len(receipts) != 1 || receipts[0].Outcome != "delivered" {
		t.Fatalf("want one delivered receipt, got %+v", receipts)
	}
}

// TestRunnerWhitelistBlocksDelivery: a task whose URL is outside the allowlist
// is settled failed WITHOUT a delivery attempt, with an allowlist receipt.
func TestRunnerWhitelistBlocksDelivery(t *testing.T) {
	e, _ := webhookRunnerEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	calls := 0
	n := runWebhookDeliveries(context.Background(), e, []string{"other.com"}, nil, false,
		func(_ context.Context, _ string, _ []byte, _ string) *webhookDeliveryError { calls++; return nil })
	if n != 1 || calls != 0 {
		t.Fatalf("blocked task must be settled without delivery: processed=%d calls=%d", n, calls)
	}
	if len(e.OpenWebhookTasks()) != 0 {
		t.Fatal("blocked task must leave the queue (settled failed)")
	}
	receipts := e.WebhookReceipts()
	if len(receipts) != 1 || receipts[0].Category != "allowlist" {
		t.Fatalf("want one allowlist-denial receipt, got %+v", receipts)
	}
}

// TestRunnerRetriesTransient: a transient failure (first two attempts fail)
// is retried and settles completed on the third.
func TestRunnerRetriesTransient(t *testing.T) {
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
				return &webhookDeliveryError{category: "network", err: fmt.Errorf("transient")}
			}
			return nil
		})
	if n != 1 || attempts != 3 {
		t.Fatalf("want 3 attempts then success, got %d attempts, %d processed", attempts, n)
	}
	if len(e.OpenWebhookTasks()) != 0 {
		t.Fatal("task must be settled completed after retries")
	}
}

// TestRunnerGivesUp: a permanent failure exhausts the retry cap and settles
// the task failed (idempotency_key stays identical across attempts).
func TestRunnerGivesUp(t *testing.T) {
	shortenBackoff(t)
	e, _ := webhookRunnerEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	keys := map[string]bool{}
	n := runWebhookDeliveries(context.Background(), e, []string{"example.com"}, nil, false,
		func(_ context.Context, _ string, payload []byte, _ string) *webhookDeliveryError {
			attempts++
			var p map[string]any
			_ = json.Unmarshal(payload, &p)
			if k, _ := p["idempotency_key"].(string); k != "" {
				keys[k] = true
			}
			return &webhookDeliveryError{category: "http_5xx", status: 503, err: fmt.Errorf("permanent")}
		})
	if n != 1 || attempts != webhookAttempts {
		t.Fatalf("want %d attempts, got %d (processed %d)", webhookAttempts, attempts, n)
	}
	if len(keys) != 1 {
		t.Fatalf("idempotency_key must be IDENTICAL across retries, got %d distinct keys", len(keys))
	}
	if len(e.OpenWebhookTasks()) != 0 {
		t.Fatal("task must be settled failed after giving up")
	}
	if got := e.FailedWebhookTasks(); len(got) != 1 {
		t.Fatalf("task must appear in the DLQ listing, got %d", len(got))
	}
}

// TestRunnerStopsOnContextCancel: a shutdown during delivery leaves the task
// OPEN (not failed), so the next boot retries it under the same delivery
// identity — the receiver dedups any partial effect.
func TestRunnerStopsOnContextCancel(t *testing.T) {
	shortenBackoff(t)
	e, _ := webhookRunnerEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	deliver := func(ctx context.Context, _ string, _ []byte, _ string) *webhookDeliveryError {
		close(started)
		<-ctx.Done()
		return &webhookDeliveryError{category: "timeout", err: ctx.Err()}
	}
	done := make(chan int, 1)
	go func() { done <- runWebhookDeliveries(ctx, e, []string{"example.com"}, nil, false, deliver) }()
	<-started
	cancel()
	<-done

	if got := e.OpenWebhookTasks(); len(got) != 1 {
		t.Fatalf("a shutdown mid-delivery must leave the task open, got %d", len(got))
	}
}

// TestWebhookDeliveryID: the delivery identity is the per-firing DeliveryID;
// legacy tasks without one fall back to the task id.
func TestWebhookDeliveryID(t *testing.T) {
	if got := webhookDeliveryID(&engine.Task{ID: "legacy"}); got != "legacy" {
		t.Fatalf("legacy task must fall back to task id, got %q", got)
	}
	if got := webhookDeliveryID(&engine.Task{ID: "legacy", DeliveryID: "del-1"}); got != "del-1" {
		t.Fatalf("hardened task must use its DeliveryID, got %q", got)
	}
}

// TestRunnerClassificationNonRetryable: 3xx and non-retryable 4xx settle the
// task failed after exactly ONE attempt (no pointless retries).
func TestRunnerClassificationNonRetryable(t *testing.T) {
	shortenBackoff(t)
	for _, status := range []int{301, 302, 400, 401, 403, 404, 410, 418} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			e, _ := webhookRunnerEngine(t)
			owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
			if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
				t.Fatal(err)
			}
			attempts := 0
			n := runWebhookDeliveries(context.Background(), e, []string{"example.com"}, nil, false,
				func(_ context.Context, _ string, _ []byte, _ string) *webhookDeliveryError {
					attempts++
					cat := "http_4xx"
					if status >= 300 && status < 400 {
						cat = "redirect"
					}
					return &webhookDeliveryError{status: status, category: cat, err: fmt.Errorf("HTTP %d", status)}
				})
			if n != 1 || attempts != 1 {
				t.Fatalf("non-retryable %d must fail after one attempt, got %d attempts (processed %d)", status, attempts, n)
			}
			if len(e.FailedWebhookTasks()) != 1 {
				t.Fatal("task must be dead-lettered")
			}
		})
	}
}

// TestRunnerClassificationRetryable: 408/425/429 and 5xx ARE retried.
func TestRunnerClassificationRetryable(t *testing.T) {
	shortenBackoff(t)
	for _, status := range []int{408, 425, 429, 500, 502, 503} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			e, _ := webhookRunnerEngine(t)
			owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
			if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
				t.Fatal(err)
			}
			attempts := 0
			n := runWebhookDeliveries(context.Background(), e, []string{"example.com"}, nil, false,
				func(_ context.Context, _ string, _ []byte, _ string) *webhookDeliveryError {
					attempts++
					if attempts == 1 {
						return &webhookDeliveryError{status: status, category: "http_5xx", err: fmt.Errorf("HTTP %d", status)}
					}
					return nil
				})
			if n != 1 || attempts != 2 {
				t.Fatalf("retryable %d must be retried and settle completed, got %d attempts (processed %d)", status, attempts, n)
			}
			if len(e.OpenWebhookTasks()) != 0 {
				t.Fatal("task must be settled completed after the retry")
			}
		})
	}
}

// TestRunnerAuthMissingFailsClosed: when the operator configured an egress
// secrets file, a destination WITHOUT a credential entry fails closed — no
// unauthenticated delivery may silently leave the perimeter.
func TestRunnerAuthMissingFailsClosed(t *testing.T) {
	e, _ := webhookRunnerEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	calls := 0
	secrets := map[string]string{"other.com": "Bearer sk_other"} // example.com missing
	n := runWebhookDeliveries(context.Background(), e, []string{"example.com"}, secrets, true,
		func(_ context.Context, _ string, _ []byte, _ string) *webhookDeliveryError { calls++; return nil })
	if n != 1 || calls != 0 {
		t.Fatalf("auth_missing must fail closed without delivery: processed=%d calls=%d", n, calls)
	}
	if len(e.FailedWebhookTasks()) != 1 {
		t.Fatal("task must be dead-lettered")
	}
	receipts := e.WebhookReceipts()
	if len(receipts) != 1 || receipts[0].Category != "auth_missing" {
		t.Fatalf("want one auth_missing receipt, got %+v", receipts)
	}
}

// TestRunnerSendsBearer: a configured destination credential is injected into
// the Authorization header, from runtime secrets only (never the DSL).
func TestRunnerSendsBearer(t *testing.T) {
	e, _ := webhookRunnerEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	var gotBearer string
	secrets := map[string]string{"example.com": "Bearer sk_live_test"}
	n := runWebhookDeliveries(context.Background(), e, []string{"example.com"}, secrets, true,
		func(_ context.Context, _ string, _ []byte, bearer string) *webhookDeliveryError {
			gotBearer = bearer
			return nil
		})
	if n != 1 || gotBearer != "Bearer sk_live_test" {
		t.Fatalf("want injected bearer on delivery, got %q (processed %d)", gotBearer, n)
	}
}
