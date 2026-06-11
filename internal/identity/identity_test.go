package identity

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"path/filepath"
	"testing"

	"github.com/avangerus/kalita/internal/eventstore"
)

var ctx = context.Background()

func ed25519Sign(priv ed25519.PrivateKey, msg []byte) []byte {
	return ed25519.Sign(priv, msg)
}

var admin = eventstore.Actor{Type: eventstore.ActorHuman, ID: "mike", Role: "Owner"}

func newRegistry() *Registry {
	return NewRegistry(eventstore.NewMemStore(nil))
}

func TestKeySaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	pub, priv, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent.key")
	if err := SaveKey(path, priv); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadKey(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded.Public().(ed25519.PublicKey), pub) {
		t.Fatal("loaded key must equal saved key")
	}
}

func TestNodeKeyCreateThenLoad(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadOrCreateNodeKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateNodeKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Equal(second) {
		t.Fatal("second start must load the same node key, not generate a new one")
	}
}

func TestRegisterAndVerifySignature(t *testing.T) {
	r := newRegistry()
	pub, priv, _ := GenerateKey()

	err := r.Register(ctx, admin, "collector-1", eventstore.ActorAgent, "Collector", pub,
		&eventstore.Basis{Type: "human", ID: "mike"})
	if err != nil {
		t.Fatal(err)
	}

	msg := []byte("propose_change: add field Debtor.email")
	sig := ed25519Sign(priv, msg)
	if err := r.VerifySignature(ctx, "collector-1", msg, sig); err != nil {
		t.Fatalf("valid signature must verify: %v", err)
	}
	if err := r.VerifySignature(ctx, "collector-1", []byte("another message"), sig); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("want ErrBadSignature, got %v", err)
	}
}

func TestDuplicateRegistrationRejected(t *testing.T) {
	r := newRegistry()
	pub, _, _ := GenerateKey()
	if err := r.Register(ctx, admin, "a-1", eventstore.ActorAgent, "Collector", pub, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(ctx, admin, "a-1", eventstore.ActorAgent, "Collector", pub, nil); !errors.Is(err, ErrActorExists) {
		t.Fatalf("want ErrActorExists, got %v", err)
	}
}

func TestAgentCannotRegisterAgents(t *testing.T) {
	r := newRegistry()
	pub, _, _ := GenerateKey()
	agent := eventstore.Actor{Type: eventstore.ActorAgent, ID: "sneaky", Role: "Collector"}
	if err := r.Register(ctx, agent, "a-2", eventstore.ActorAgent, "Collector", pub, nil); err == nil {
		t.Fatal("agents must not be able to register actors in v0")
	}
}

func TestKeyRotation(t *testing.T) {
	r := newRegistry()
	pub1, priv1, _ := GenerateKey()
	pub2, priv2, _ := GenerateKey()
	if err := r.Register(ctx, admin, "a-3", eventstore.ActorAgent, "Collector", pub1, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.RotateKey(ctx, admin, "a-3", pub2, nil); err != nil {
		t.Fatal(err)
	}
	msg := []byte("hello")
	if err := r.VerifySignature(ctx, "a-3", msg, ed25519Sign(priv1, msg)); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("old key must stop verifying after rotation, got %v", err)
	}
	if err := r.VerifySignature(ctx, "a-3", msg, ed25519Sign(priv2, msg)); err != nil {
		t.Fatalf("new key must verify: %v", err)
	}
}

func TestDisabledActorRejected(t *testing.T) {
	r := newRegistry()
	pub, priv, _ := GenerateKey()
	if err := r.Register(ctx, admin, "a-4", eventstore.ActorAgent, "Collector", pub, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.Disable(ctx, admin, "a-4", nil); err != nil {
		t.Fatal(err)
	}
	msg := []byte("hello")
	if err := r.VerifySignature(ctx, "a-4", msg, ed25519Sign(priv, msg)); !errors.Is(err, ErrActorDisabled) {
		t.Fatalf("disabled actor must not verify, got %v", err)
	}
}

func TestUnknownActor(t *testing.T) {
	r := newRegistry()
	if err := r.VerifySignature(ctx, "ghost", []byte("x"), nil); !errors.Is(err, ErrActorUnknown) {
		t.Fatalf("want ErrActorUnknown, got %v", err)
	}
}
