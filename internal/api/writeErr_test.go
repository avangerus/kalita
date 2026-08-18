package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// Regression: a GUARD_FAILED engine error over REST used to hit
// statusByCode[e.Code] == 0 and panic the handler goroutine with
// "invalid WriteHeader code 0" (found by the English clean-room acceptance,
// amount-ladder approve above the actor's threshold). Every engine code must
// map to a real HTTP status, and an unmapped future code must degrade to 500 —
// never panic.

const guardFailPack = `
entity Invoice:
    title: string required
    amount: money required
    status: enum[Draft, Approved] default=Draft

roles:
    Owner
    Manager

permissions:
    Owner:
        full [Invoice]
    Manager:
        read all
        act [approve]

workflow Invoice on status:
    Draft -> Approved: approve when amount <= 100k requires approval(Manager)
`

func TestGuardFailedDoesNotPanic(t *testing.T) {
	ctx := context.Background()
	model, errs := dsl.Compile(map[string]string{"g.dsl": guardFailPack})
	if len(errs) > 0 {
		t.Fatalf("pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	_, err := reg.RegisterWithToken(ctx, registrar, "boss", eventstore.ActorHuman, "Owner", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	mgrTok, err := reg.RegisterWithToken(ctx, registrar, "mg", eventstore.ActorHuman, "Manager", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "boss", Role: "Owner"}
	rec, err := eng.Create(ctx, owner, "Invoice",
		map[string]any{"title": "big", "amount": map[string]any{"amount": 500000, "currency": "RUB"}},
		&eventstore.Basis{Type: "human", ID: "boss"}, "")
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()

	// Manager fires approve on an invoice ABOVE their threshold: guard fails.
	req, err := http.NewRequest(http.MethodPost,
		srv.URL+"/api/records/Invoice/"+rec.ID+"/act",
		strings.NewReader(`{"action":"approve","basis":{"type":"human","id":"mg"},"idempotency_key":"guard-fail-0001"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+mgrTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err) // a panic would have killed the connection instead
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("guard failure status = %d, want 409", resp.StatusCode)
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body["code"] != engine.CodeGuardFailed {
		t.Fatalf("want GUARD_FAILED code, got %v", body)
	}
}
