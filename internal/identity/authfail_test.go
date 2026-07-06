package identity

import (
	"encoding/json"
	"testing"

	"github.com/avangerus/kalita/internal/eventstore"
)

// TestAuthThrottle: N failed authentications from one IP journal exactly one
// auth.throttled event at the threshold crossing, not one per attempt (LLD-10
// §4, acceptance test 5) — a journal flood is itself the T6 DoS this guards
// against.
func TestAuthThrottle(t *testing.T) {
	store := eventstore.NewMemStore(nil)
	r := NewRegistry(store)

	for i := 0; i < AuthFailThreshold*2; i++ {
		r.RecordAuthFailure(ctx, "10.0.0.5")
	}

	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var throttled []*eventstore.Event
	for _, e := range events {
		if e.Kind == eventstore.AuthThrottled {
			throttled = append(throttled, e)
		}
	}
	if len(throttled) != 1 {
		t.Fatalf("want exactly 1 auth.throttled event for %d failures over threshold %d, got %d",
			AuthFailThreshold*2, AuthFailThreshold, len(throttled))
	}
	if throttled[0].SchemaV != eventstore.CurrentSchemaV {
		t.Fatalf("auth.throttled must be written with SchemaV %d, got %d", eventstore.CurrentSchemaV, throttled[0].SchemaV)
	}
	var p authThrottledPayload
	if err := json.Unmarshal(throttled[0].Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.IP != "10.0.0.5" {
		t.Fatalf("want ip 10.0.0.5, got %q", p.IP)
	}
	if p.Count != AuthFailThreshold {
		t.Fatalf("want count %d (the crossing point), got %d", AuthFailThreshold, p.Count)
	}
}

// TestAuthThrottlePerIPIndependent: two different IPs get independent
// counters — one IP crossing the threshold must not affect another's budget.
func TestAuthThrottlePerIPIndependent(t *testing.T) {
	store := eventstore.NewMemStore(nil)
	r := NewRegistry(store)

	for i := 0; i < AuthFailThreshold; i++ {
		r.RecordAuthFailure(ctx, "10.0.0.1")
	}
	for i := 0; i < AuthFailThreshold-1; i++ {
		r.RecordAuthFailure(ctx, "10.0.0.2")
	}

	events, _ := store.All(ctx)
	count := 0
	for _, e := range events {
		if e.Kind == eventstore.AuthThrottled {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("only the IP that crossed its own threshold should fire, got %d auth.throttled events", count)
	}
}
