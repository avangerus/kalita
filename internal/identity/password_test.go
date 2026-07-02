package identity

import (
	"encoding/json"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/avangerus/kalita/internal/eventstore"
)

// TestPasswordSetAndLogin: SetPassword journals only the bcrypt hash;
// LoginWithPassword verifies it and mints a fresh, working bearer token.
func TestPasswordSetAndLogin(t *testing.T) {
	store := eventstore.NewMemStore(nil)
	reg := NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	if _, err := reg.RegisterWithToken(ctx, root, "mike", eventstore.ActorHuman, "Owner", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetPassword(ctx, root, "mike", "hunter2 hunter2 hunter2", nil); err != nil {
		t.Fatal(err)
	}
	token, err := reg.LoginWithPassword(ctx, "mike", "hunter2 hunter2 hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("expected a bearer token")
	}
	info, err := reg.Authenticate(ctx, token)
	if err != nil {
		t.Fatalf("the minted token must authenticate: %v", err)
	}
	if info.ID != "mike" {
		t.Fatalf("token must resolve to mike, got %s", info.ID)
	}

	// wrong password
	if _, err := reg.LoginWithPassword(ctx, "mike", "wrong"); err != ErrBadPassword {
		t.Fatalf("wrong password must return ErrBadPassword, got %v", err)
	}

	// an actor without a password set cannot log in
	if _, err := reg.RegisterWithToken(ctx, root, "nopass", eventstore.ActorHuman, "Owner", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.LoginWithPassword(ctx, "nopass", "anything"); err != ErrNoPassword {
		t.Fatalf("no password set must return ErrNoPassword, got %v", err)
	}
}

// TestPasswordSetRejectsShort: below the minimum length, SetPassword errors
// instead of silently accepting a weak credential (I-3: no silent failure).
func TestPasswordSetRejectsShort(t *testing.T) {
	store := eventstore.NewMemStore(nil)
	reg := NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	if _, err := reg.RegisterWithToken(ctx, root, "mike", eventstore.ActorHuman, "Owner", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetPassword(ctx, root, "mike", "short", nil); err == nil {
		t.Fatal("expected an error for a too-short password")
	}
}

// TestPasswordSetHumansOnly: agents cannot set passwords (mirrors Register's
// human-only rule — v0 has no agent identity administering other actors).
func TestPasswordSetHumansOnly(t *testing.T) {
	store := eventstore.NewMemStore(nil)
	reg := NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	if _, err := reg.RegisterWithToken(ctx, root, "mike", eventstore.ActorHuman, "Owner", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	agent := eventstore.Actor{Type: eventstore.ActorAgent, ID: "bot", Role: "Owner"}
	if err := reg.SetPassword(ctx, agent, "mike", "a long enough password", nil); err == nil {
		t.Fatal("expected an error: agents may not set passwords")
	}
}

// LLD-05 §4 test 3: TestPasswordVerifyOffline — `kalita verify`'s core
// primitive (eventstore.VerifyChain, the offline-provability check, I-5)
// passes on a journal that contains an actor.password_set event: the bcrypt
// event is chained and hashed exactly like every other event, no special
// case needed.
func TestPasswordVerifyOffline(t *testing.T) {
	store := eventstore.NewMemStore(nil)
	reg := NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	if _, err := reg.RegisterWithToken(ctx, root, "mike", eventstore.ActorHuman, "Owner", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetPassword(ctx, root, "mike", "correct horse battery staple", &eventstore.Basis{Type: "human", ID: "root"}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.LoginWithPassword(ctx, "mike", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}

	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := eventstore.VerifyChain(events); err != nil {
		t.Fatalf("chain with actor.password_set/actor.key_rotated (via password login) must verify offline: %v", err)
	}

	// I-4 sanity: no event anywhere in the chain carries the raw password.
	for _, e := range events {
		if strings.Contains(string(e.Payload), "correct horse battery staple") {
			t.Fatalf("seq %d payload leaks the raw password: %s", e.Seq, e.Payload)
		}
	}

	// and the bcrypt hash really is a bcrypt hash, not the password verbatim.
	found := false
	for _, e := range events {
		if e.Kind != eventstore.ActorPasswordSet {
			continue
		}
		found = true
		var p passwordPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if bcrypt.CompareHashAndPassword(p.BcryptHash, []byte("correct horse battery staple")) != nil {
			t.Fatal("stored hash must verify against the original password via bcrypt")
		}
	}
	if !found {
		t.Fatal("actor.password_set must be present in the chain")
	}
}
