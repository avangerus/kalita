package engine

import (
	"context"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Multi-currency money ({amount, currency}) must be numeric in computed
// arithmetic and aggregates — otherwise a price entered with a currency
// silently drops out of every total.
func TestMoneyObjectInComputed(t *testing.T) {
	src := `pack t
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1
entity Line:
    price: money
    qty:   int default=1
    total: money computed = price * qty
roles:
    Op
permissions:
    Op:
        full [Line]
`
	model, errs := dsl.Compile(map[string]string{"t.dsl": src})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	ctx := context.Background()
	eng, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	op := eventstore.Actor{Type: eventstore.ActorHuman, ID: "op", Role: "Op"}
	basis := &eventstore.Basis{Type: "human", ID: "op"}

	// price as a {amount, currency} object, qty 3 -> total 3000
	rec, err := eng.Create(ctx, op, "Line", map[string]any{
		"price": map[string]any{"amount": 1000, "currency": "USD"}, "qty": 3}, basis, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if total, _ := rec.Values["total"].(float64); total != 3000 {
		t.Errorf("total = %v, want 3000 (multi-currency money must compute)", rec.Values["total"])
	}
}
