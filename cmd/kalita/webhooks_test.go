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

// Cycle-B1 (D-011) — the node's webhook runner: whitelist normalization and
// enforcement, at-least-once delivery with a deterministic idempotency_key,
// and settle semantics. deliver is injected; no network.

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

// TestRunnerSettlesDelivery: the runner delivers once with a payload carrying
// the deterministic idempotency_key, then settles the task completed.
func TestRunnerSettlesDelivery(t *testing.T) {
	e, _ := webhookRunnerEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	taskID := e.OpenWebhookTasks()[0].ID

	var mu sync.Mutex
	var gotURL string
	var gotPayload map[string]any
	n := runWebhookDeliveries(context.Background(), e, []string{"example.com"},
		func(_ context.Context, url string, payload []byte) error {
			mu.Lock()
			defer mu.Unlock()
			gotURL = url
			_ = json.Unmarshal(payload, &gotPayload)
			return nil
		})
	if n != 1 {
		t.Fatalf("runner must process 1 task, got %d", n)
	}
	if gotURL != "https://example.com/hook" {
		t.Fatalf("delivered url = %q", gotURL)
	}
	if gotPayload["idempotency_key"] != "webhook:"+taskID {
		t.Fatalf("idempotency_key = %v, want webhook:%s", gotPayload["idempotency_key"], taskID)
	}
	if gotPayload["record_id"] == nil {
		t.Fatal("payload must carry the record id")
	}
	if len(e.OpenWebhookTasks()) != 0 {
		t.Fatal("delivered task must be settled (left the open queue)")
	}
}

// TestRunnerWhitelistBlocksDelivery: a task whose URL is outside the allowlist
// is settled failed WITHOUT a delivery attempt.
func TestRunnerWhitelistBlocksDelivery(t *testing.T) {
	e, _ := webhookRunnerEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}
	calls := 0
	n := runWebhookDeliveries(context.Background(), e, []string{"other.com"},
		func(_ context.Context, _ string, _ []byte) error { calls++; return nil })
	if n != 1 || calls != 0 {
		t.Fatalf("blocked task must be settled without delivery: processed=%d calls=%d", n, calls)
	}
	if len(e.OpenWebhookTasks()) != 0 {
		t.Fatal("blocked task must leave the queue (settled failed)")
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
	n := runWebhookDeliveries(context.Background(), e, []string{"example.com"},
		func(_ context.Context, _ string, _ []byte) error {
			attempts++
			if attempts < 3 {
				return fmt.Errorf("transient")
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
	n := runWebhookDeliveries(context.Background(), e, []string{"example.com"},
		func(_ context.Context, _ string, payload []byte) error {
			attempts++
			var p map[string]any
			_ = json.Unmarshal(payload, &p)
			if k, _ := p["idempotency_key"].(string); k != "" {
				keys[k] = true
			}
			return fmt.Errorf("permanent")
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
}

// TestRunnerStopsOnContextCancel: a shutdown during delivery leaves the task
// OPEN (not failed), so the next boot retries it under the same deterministic
// idempotency_key — the receiver dedups any partial effect.
func TestRunnerStopsOnContextCancel(t *testing.T) {
	shortenBackoff(t)
	e, _ := webhookRunnerEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"}, &eventstore.Basis{Type: "human", ID: "o"}, ""); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	deliver := func(ctx context.Context, _ string, _ []byte) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	done := make(chan int, 1)
	go func() { done <- runWebhookDeliveries(ctx, e, []string{"example.com"}, deliver) }()
	<-started
	cancel()
	<-done

	if got := e.OpenWebhookTasks(); len(got) != 1 {
		t.Fatalf("a shutdown mid-delivery must leave the task open, got %d", len(got))
	}
}
