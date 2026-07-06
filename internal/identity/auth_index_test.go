package identity

import (
	"errors"
	"fmt"
	"testing"

	"github.com/avangerus/kalita/internal/eventstore"
)

// TestAuthenticateFindsCorrectActorAmongMany: Authenticate must still resolve
// to the RIGHT actor once lookup goes through tokenIndex instead of a linear
// scan (LLD-12 §2) — registering many actors and authenticating each one's
// own token must never cross-resolve to a neighbor.
func TestAuthenticateFindsCorrectActorAmongMany(t *testing.T) {
	r := newRegistry()
	const n = 200
	tokens := make([]string, n)
	for i := 0; i < n; i++ {
		token, err := r.RegisterWithToken(ctx, admin, fmt.Sprintf("actor-%d", i),
			eventstore.ActorAgent, "Collector", nil, nil, nil)
		if err != nil {
			t.Fatalf("register actor-%d: %v", i, err)
		}
		tokens[i] = token
	}
	for i := 0; i < n; i++ {
		info, err := r.Authenticate(ctx, tokens[i])
		if err != nil {
			t.Fatalf("authenticate actor-%d: %v", i, err)
		}
		want := fmt.Sprintf("actor-%d", i)
		if info.ID != want {
			t.Fatalf("token %d resolved to actor %q, want %q", i, info.ID, want)
		}
	}
}

// TestAuthenticateWrongTokenRejected: a syntactically valid but never-issued
// token must be refused (ErrActorUnknown), not accidentally matched to
// whichever actor the index happens to hash near.
func TestAuthenticateWrongTokenRejected(t *testing.T) {
	r := newRegistry()
	if _, err := r.RegisterWithToken(ctx, admin, "actor-1", eventstore.ActorAgent, "Collector", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Authenticate(ctx, "0000000000000000000000000000000000000000000000000000000000000000"); !errors.Is(err, ErrActorUnknown) {
		t.Fatalf("want ErrActorUnknown for a never-issued token, got %v", err)
	}
}

// TestAuthenticateTokenReplacedOnReRegister: EnsureActor re-registering an
// existing id (and LoginWithPassword/RotateKey's own token re-issue) must
// retire the OLD token hash from the index — otherwise a stale tokenIndex
// entry would keep authenticating a token that was supposed to have been
// replaced. This is the exact correctness edge the incremental fold in
// foldLocked's "delete old hash" step exists for.
func TestAuthenticateTokenReplacedOnReRegister(t *testing.T) {
	r := newRegistry()
	oldToken, err := r.EnsureActor(ctx, "bootstrap-worker", eventstore.ActorAgent, "Collector", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Authenticate(ctx, oldToken); err != nil {
		t.Fatalf("old token must authenticate before re-registration: %v", err)
	}

	newToken, err := r.EnsureActor(ctx, "bootstrap-worker", eventstore.ActorAgent, "Collector", nil)
	if err != nil {
		t.Fatal(err)
	}
	if newToken == oldToken {
		t.Fatal("EnsureActor must re-issue a fresh token on re-registration")
	}

	if _, err := r.Authenticate(ctx, oldToken); !errors.Is(err, ErrActorUnknown) {
		t.Fatalf("old token must stop authenticating once replaced, got %v", err)
	}
	info, err := r.Authenticate(ctx, newToken)
	if err != nil {
		t.Fatalf("new token must authenticate: %v", err)
	}
	if info.ID != "bootstrap-worker" {
		t.Fatalf("new token resolved to %q, want bootstrap-worker", info.ID)
	}
}

// TestAuthenticateDisabledStillRejectedThroughIndex: the disabled check must
// still apply to the actor the index resolves to — the index only changes
// HOW the actor is found, not what happens after (LLD-12 §2: "РЕЗУЛЬТАТ
// Authenticate неизменен").
func TestAuthenticateDisabledStillRejectedThroughIndex(t *testing.T) {
	r := newRegistry()
	token, err := r.RegisterWithToken(ctx, admin, "actor-1", eventstore.ActorAgent, "Collector", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Disable(ctx, admin, "actor-1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Authenticate(ctx, token); !errors.Is(err, ErrActorDisabled) {
		t.Fatalf("want ErrActorDisabled, got %v", err)
	}
}
