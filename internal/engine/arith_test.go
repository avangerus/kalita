package engine

import (
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Arithmetic in computed fields: weighted forecast (amount * probability / 100),
// remaining balance (limit - used aggregate), and arithmetic combined with an
// aggregate term.
func TestComputedArithmetic(t *testing.T) {
	src := `
entity Quota:
    limit_days: int
    used: int computed = sum(Leave.days where quota = $self)
    remaining: int computed = limit_days - sum(Leave.days where quota = $self)

entity Leave:
    quota: ref[Quota]
    days: int

entity Deal:
    amount: money
    probability: percent
    forecast: float computed = amount * probability / 100

roles:
    Owner

permissions:
    Owner:
        full [Quota, Leave, Deal]
`
	model, errs := dsl.Compile(map[string]string{"t.dsl": src})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	o := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}

	// weighted forecast: 200000 * 30 / 100 = 60000
	deal, _ := e.Create(ctx, o, "Deal", map[string]any{"amount": 200000.0, "probability": 30.0}, basis, "")
	g, _ := e.Get(ctx, o, "Deal", deal.ID)
	if g.Values["forecast"] != 60000.0 {
		t.Fatalf("forecast = amount*probability/100 must be 60000, got %v", g.Values["forecast"])
	}

	// remaining balance: 28 - (10 + 5) = 13
	q, _ := e.Create(ctx, o, "Quota", map[string]any{"limit_days": 28.0}, basis, "")
	_, _ = e.Create(ctx, o, "Leave", map[string]any{"quota": q.ID, "days": 10.0}, basis, "")
	_, _ = e.Create(ctx, o, "Leave", map[string]any{"quota": q.ID, "days": 5.0}, basis, "")
	gq, _ := e.Get(ctx, o, "Quota", q.ID)
	if gq.Values["used"] != 15.0 {
		t.Fatalf("used aggregate must be 15, got %v", gq.Values["used"])
	}
	if gq.Values["remaining"] != 13.0 {
		t.Fatalf("remaining = limit - sum must be 13, got %v", gq.Values["remaining"])
	}
}
