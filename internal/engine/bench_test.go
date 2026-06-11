package engine

import (
	"fmt"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// Benchmarks: the storm insurance. CI runs these as smoke (-benchtime=1x);
// locally `go test -bench=. ./internal/engine` gives the real numbers.
// Regression policy: an order-of-magnitude drop is a bug, not a vibe.

func benchEngine(b *testing.B) *Engine {
	b.Helper()
	model, errs := dsl.Compile(map[string]string{"t.kal": testPack})
	if len(errs) > 0 {
		b.Fatal(errs[0])
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		b.Fatal(err)
	}
	return e
}

func BenchmarkCreate(b *testing.B) {
	e := benchEngine(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.Create(ctx, admin, "Doc", map[string]any{"title": fmt.Sprintf("d-%d", i)}, basis, "")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQueryOver10k(b *testing.B) {
	e := benchEngine(b)
	for i := 0; i < 10_000; i++ {
		_, _ = e.Create(ctx, admin, "Doc", map[string]any{"title": fmt.Sprintf("d-%d", i)}, basis, "")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, err := e.Query(ctx, admin, "Doc", QueryOpts{Filter: map[string]any{"status": "Draft"}, Limit: 25})
		if err != nil || len(rows) != 25 {
			b.Fatalf("%v %d", err, len(rows))
		}
	}
}

func BenchmarkGetWithComputed(b *testing.B) {
	e := benchEngine(b)
	doc, _ := e.Create(ctx, admin, "Doc", map[string]any{"title": "x"}, basis, "")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Get(ctx, admin, "Doc", doc.ID); err != nil {
			b.Fatal(err)
		}
	}
}

// The hot path of every request: token auth against a journal with history.
func BenchmarkAuthenticateOver10kEvents(b *testing.B) {
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	token, err := reg.RegisterWithToken(ctx, registrar, "bot", eventstore.ActorAgent, "Bot", nil, nil, nil)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 10_000; i++ {
		_, _ = store.Append(ctx, eventstore.AppendInput{
			Actor: registrar, Kind: eventstore.RecordCreated,
			Subject: eventstore.Subject{Entity: "Doc", RecordID: fmt.Sprintf("d-%d", i)},
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := reg.Authenticate(ctx, token); err != nil {
			b.Fatal(err)
		}
	}
}
