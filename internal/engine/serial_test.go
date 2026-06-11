package engine

import (
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

func TestSerialAndMoney(t *testing.T) {
	src := `
entity Invoice:
    number: serial format="INV-{year}-{seq:5}" unique
    total: money

roles:
    Owner

permissions:
    Owner:
        full [Invoice]
`
	model, errs := dsl.Compile(map[string]string{"t.kal": src})
	if len(errs) > 0 {
		t.Fatalf("compile: %v", errs[0])
	}
	clock := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	store := eventstore.NewMemStore(nil)
	e, err := New(ctx, model, store, WithClock(func() time.Time { return clock }))
	if err != nil {
		t.Fatal(err)
	}
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}

	// auto-assigned serials, monotonic, formatted
	a, _ := e.Create(ctx, owner, "Invoice", map[string]any{"total": 1000.0}, basis, "")
	b, _ := e.Create(ctx, owner, "Invoice", map[string]any{
		"total": map[string]any{"amount": 500.0, "currency": "USD"}}, basis, "")
	if a.Values["number"] != "INV-2026-00001" {
		t.Fatalf("first serial: %v", a.Values["number"])
	}
	if b.Values["number"] != "INV-2026-00002" {
		t.Fatalf("second serial: %v", b.Values["number"])
	}

	// money: bare number and {amount,currency} both ok; bad currency rejected
	if _, err := e.Create(ctx, owner, "Invoice", map[string]any{
		"total": map[string]any{"amount": 1.0, "currency": "rubles"}}, basis, ""); err == nil {
		t.Fatal("bad currency must be rejected")
	}

	// replay resumes the counter (no stored cursor)
	e2, _ := New(ctx, model, store, WithClock(func() time.Time { return clock }))
	c, _ := e2.Create(ctx, owner, "Invoice", map[string]any{"total": 1.0}, basis, "")
	if c.Values["number"] != "INV-2026-00003" {
		t.Fatalf("serial must resume after replay, got %v", c.Values["number"])
	}
}
