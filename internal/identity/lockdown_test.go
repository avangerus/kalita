package identity

import (
	"errors"
	"strings"
	"testing"

	"github.com/avangerus/kalita/internal/eventstore"
)

// TestLockdownInvalidatesAllButEmergency: after node.lockdown, every ordinary
// token — even a valid, never-disabled one — is rejected, and only the fresh
// emergency admin token authenticates (LLD-10 §3, acceptance test 3, minus
// the transport-layer half which is exercised in internal/api).
func TestLockdownInvalidatesAllButEmergency(t *testing.T) {
	r := newRegistry()
	pub, _, _ := GenerateKey()
	if err := r.Register(ctx, admin, "agent-1", eventstore.ActorAgent, "Collector", pub, nil); err != nil {
		t.Fatal(err)
	}
	token, err := r.RegisterWithToken(ctx, admin, "human-1", eventstore.ActorHuman, "Owner", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Authenticate(ctx, token); err != nil {
		t.Fatalf("token must authenticate before lockdown: %v", err)
	}

	emergency, err := r.Lockdown(ctx, admin, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := r.Authenticate(ctx, token); !errors.Is(err, ErrLockdown) {
		t.Fatalf("previously valid token must be rejected during lockdown, got %v", err)
	}
	info, err := r.Authenticate(ctx, emergency)
	if err != nil {
		t.Fatalf("emergency admin token must authenticate during lockdown: %v", err)
	}
	if info.Role != "Owner" {
		t.Fatalf("emergency admin defaults to Owner role, got %q", info.Role)
	}

	if err := r.LiftLockdown(ctx, admin, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Authenticate(ctx, token); err != nil {
		t.Fatalf("token must authenticate again after lockdown is lifted, got %v", err)
	}
	if _, err := r.Authenticate(ctx, emergency); !errors.Is(err, ErrActorUnknown) {
		t.Fatalf("emergency token must stop working once lockdown is lifted (it was never a registered actor), got %v", err)
	}
}

// TestLockdownOnlyHumansTrigger: agents cannot declare an emergency (same
// rule as Register/Disable — no self-service emergency admin).
func TestLockdownOnlyHumansTrigger(t *testing.T) {
	r := newRegistry()
	agent := eventstore.Actor{Type: eventstore.ActorAgent, ID: "sneaky", Role: "Collector"}
	if _, err := r.Lockdown(ctx, agent, "", nil); err == nil {
		t.Fatal("agents must not be able to trigger lockdown")
	}
	if err := r.LiftLockdown(ctx, agent, nil); err == nil {
		t.Fatal("agents must not be able to lift lockdown")
	}
}

// TestLockdownSecretNotInJournal: the journal holds only the hash of the
// emergency admin token, never the token itself (I-4, LLD-10 §3, acceptance
// test 4).
func TestLockdownSecretNotInJournal(t *testing.T) {
	store := eventstore.NewMemStore(nil)
	r := NewRegistry(store)
	token, err := r.Lockdown(ctx, admin, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Kind != eventstore.NodeLockdown {
			continue
		}
		found = true
		if e.SchemaV != eventstore.CurrentSchemaV {
			t.Fatalf("node.lockdown must be written with SchemaV %d, got %d", eventstore.CurrentSchemaV, e.SchemaV)
		}
		if string(e.Payload) == "" {
			t.Fatal("expected a payload")
		}
		if strings.Contains(string(e.Payload), token) {
			t.Fatal("the raw emergency admin token must never appear in the journal, only its hash")
		}
	}
	if !found {
		t.Fatal("expected a node.lockdown event")
	}
}
