package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"testing"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Cycle-A4 (LLD-10 PR-1/PR-2) regressions: atomic registration and the O(1)
// invite projection. The pre-fix code checked existence and then appended
// WITHOUT holding r.mu, so concurrent registrations of the same id could both
// pass the nil-check and double-append (minting an orphan token whose hash
// never landed in the journal).

var a4Registrar = eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}

// TestRegisterConcurrentSameID: N concurrent registrations of the same id must
// produce exactly one actor.registered, exactly one working token, and N-1
// ErrActorExists — no orphan token, no double registration.
func TestRegisterConcurrentSameID(t *testing.T) {
	ctx := context.Background()
	store := eventstore.NewMemStore(nil)
	r := NewRegistry(store)

	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)
	tokens := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tok, err := r.RegisterWithToken(ctx, a4Registrar, "same-id", eventstore.ActorHuman, "Manager", nil, nil, nil)
			errs[i], tokens[i] = err, tok
		}(i)
	}
	wg.Wait()

	ok, exists := 0, 0
	winToken := ""
	for i := 0; i < n; i++ {
		switch {
		case errs[i] == nil:
			ok++
			if tokens[i] == "" {
				t.Fatal("a successful registration must return a token")
			}
			winToken = tokens[i]
		case errs[i] == ErrActorExists:
			exists++
		default:
			t.Fatalf("unexpected error: %v", errs[i])
		}
	}
	if ok != 1 || exists != n-1 {
		t.Fatalf("want 1 success + %d ErrActorExists, got ok=%d exists=%d", n-1, ok, exists)
	}

	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	regs := 0
	for _, e := range events {
		if e.Kind == eventstore.ActorRegistered {
			regs++
		}
	}
	if regs != 1 {
		t.Fatalf("want exactly 1 actor.registered, got %d", regs)
	}
	if _, err := r.Authenticate(ctx, winToken); err != nil {
		t.Fatalf("the winning token must authenticate: %v", err)
	}
}

// TestEnsureActorConcurrentWithRegister: a bootstrap EnsureActor racing a
// human Register of the same id must not double-register — exactly one
// actor.registered for that id lands in the journal.
func TestEnsureActorConcurrentWithRegister(t *testing.T) {
	ctx := context.Background()
	store := eventstore.NewMemStore(nil)
	r := NewRegistry(store)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = r.EnsureActor(ctx, "worker-1", eventstore.ActorAgent, "APChecker", nil)
	}()
	go func() {
		defer wg.Done()
		_, _ = r.RegisterWithToken(ctx, a4Registrar, "worker-1", eventstore.ActorHuman, "Manager", nil, nil, nil)
	}()
	wg.Wait()

	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	regs := 0
	for _, e := range events {
		if e.Kind == eventstore.ActorRegistered && e.Subject.ActorID == "worker-1" {
			regs++
		}
	}
	if regs != 1 {
		t.Fatalf("want exactly 1 actor.registered for worker-1, got %d", regs)
	}
}

// scanInvite reproduces the PRE-fix full-journal fold for comparison: the O(1)
// projection must return byte-identical results for any create/redeem mix.
func scanInvite(events []*eventstore.Event, hash []byte) *Invite {
	var found *Invite
	for _, e := range events {
		if e.Kind != eventstore.InviteCreated && e.Kind != eventstore.InviteRedeemed {
			continue
		}
		var inv Invite
		if json.Unmarshal(e.Payload, &inv) != nil {
			continue
		}
		if hex.EncodeToString(inv.CodeHash) != hex.EncodeToString(hash) {
			continue
		}
		if e.Kind == eventstore.InviteCreated {
			cp := inv
			found = &cp
		} else if found != nil {
			found.Redeemed = true
			found.ActorID = inv.ActorID
		}
	}
	return found
}

// TestInviteProjectionMatchesScan: after a create/redeem mix, the O(1)
// projection (findInvite) and the old full-scan fold agree for every code.
func TestInviteProjectionMatchesScan(t *testing.T) {
	ctx := context.Background()
	store := eventstore.NewMemStore(nil)
	r := NewRegistry(store)

	codes := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		c, err := r.CreateInvite(ctx, a4Registrar, "Client", "", "", "", &eventstore.Basis{Type: "human", ID: "root"})
		if err != nil {
			t.Fatal(err)
		}
		codes = append(codes, c)
	}
	// redeem the middle one
	if _, _, err := r.Redeem(ctx, "user-1", codes[1]); err != nil {
		t.Fatal(err)
	}

	// fresh registry over the same journal: projection folds from scratch
	r2 := NewRegistry(store)
	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range codes {
		hash := sha256.Sum256([]byte(code))
		viaProj, err := r2.findInvite(ctx, hash[:])
		if err != nil {
			t.Fatalf("projection lookup failed for a known code: %v", err)
		}
		viaScan := scanInvite(events, hash[:])
		if viaScan == nil {
			t.Fatal("scan baseline must find the invite")
		}
		if viaProj.Redeemed != viaScan.Redeemed || viaProj.ActorID != viaScan.ActorID ||
			viaProj.Role != viaScan.Role || hex.EncodeToString(viaProj.CodeHash) != hex.EncodeToString(viaScan.CodeHash) {
			t.Fatalf("projection diverges from scan: proj=%+v scan=%+v", viaProj, viaScan)
		}
	}

	// a bogus code is still ErrInviteUnknown
	unknown := sha256.Sum256([]byte("does-not-exist"))
	if _, err := r2.findInvite(ctx, unknown[:]); err != ErrInviteUnknown {
		t.Fatalf("unknown code must be ErrInviteUnknown, got %v", err)
	}
}

// TestRedeemSingleUse: two concurrent redemptions of the same code — exactly
// one actor is registered, the other gets ErrInviteUsed (Cycle-A4 atomicity).
func TestRedeemSingleUse(t *testing.T) {
	ctx := context.Background()
	store := eventstore.NewMemStore(nil)
	r := NewRegistry(store)
	code, err := r.CreateInvite(ctx, a4Registrar, "Client", "", "", "", &eventstore.Basis{Type: "human", ID: "root"})
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := r.Redeem(ctx, "user-"+string(rune('a'+i)), code)
			errs[i] = err
		}(i)
	}
	wg.Wait()

	used := 0
	for _, err := range errs {
		if err == ErrInviteUsed {
			used++
		} else if err != nil {
			t.Fatalf("unexpected redeem error: %v", err)
		}
	}
	if used != 1 {
		t.Fatalf("want exactly one ErrInviteUsed on double redeem, got %d", used)
	}
	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	redeemed := 0
	for _, e := range events {
		if e.Kind == eventstore.InviteRedeemed {
			redeemed++
		}
	}
	if redeemed != 1 {
		t.Fatalf("want exactly 1 invite.redeemed, got %d", redeemed)
	}
}

// TestRotateAndDisableSerialized (PR-6): concurrent key rotations and a
// Disable of the same actor run under r.mu — every call returns, the journal
// holds exactly the appended events, and the actor ends disabled.
func TestRotateAndDisableSerialized(t *testing.T) {
	ctx := context.Background()
	store := eventstore.NewMemStore(nil)
	r := NewRegistry(store)
	if _, err := r.RegisterWithToken(ctx, a4Registrar, "bot", eventstore.ActorAgent, "Worker", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	const rotations = 8
	var wg sync.WaitGroup
	errs := make(chan error, rotations+1)
	for i := 0; i < rotations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- r.RotateKey(ctx, a4Registrar, "bot", nil, nil)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		errs <- r.Disable(ctx, a4Registrar, "bot", nil)
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	events, _ := store.All(ctx)
	rotated, disabled := 0, 0
	for _, e := range events {
		switch e.Kind {
		case eventstore.ActorKeyRotated:
			rotated++
		case eventstore.ActorDisabled:
			disabled++
		}
	}
	if rotated != rotations || disabled != 1 {
		t.Fatalf("journal: %d rotations, %d disables; want %d, 1", rotated, disabled, rotations)
	}
	info, err := r.Get(ctx, "bot")
	if err != nil || !info.Disabled {
		t.Fatalf("actor must end disabled: %+v %v", info, err)
	}
}
