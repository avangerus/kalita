package engine

import (
	"context"
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// K-BUG-02 regression: a computed field that references another computed field
// declared LATER must still evaluate (dependency order, not declaration order).
func TestComputedDependencyOrderIndependent(t *testing.T) {
	build := func(body string) map[string]any {
		src := "pack t\nversion 0.1.0\nrequires kalita >= 0.1\ndepends core >= 0.1\nentity E:\n" +
			"    opened: datetime default=$now\n" + body +
			"roles:\n    Op\npermissions:\n    Op:\n        full [E]\n"
		model, errs := dsl.Compile(map[string]string{"t.dsl": src})
		if len(errs) > 0 {
			t.Fatalf("compile: %v", errs[0])
		}
		at := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
		eng, err := New(context.Background(), model, eventstore.NewMemStore(nil), WithClock(func() time.Time { return at }))
		if err != nil {
			t.Fatal(err)
		}
		op := eventstore.Actor{Type: eventstore.ActorHuman, ID: "op", Role: "Op"}
		rec, err := eng.Create(context.Background(), op, "E", map[string]any{}, &eventstore.Basis{Type: "human", ID: "op"}, "")
		if err != nil {
			t.Fatal(err)
		}
		return rec.Values
	}
	// a = days_since(opened) = 0 at $now; b = a+1 = 1; c = b+1 = 2.
	// Declared in REVERSE dependency order (c, b, a) — the worst case for the old bug.
	rev := build("    c: int computed = b + 1\n    b: int computed = a + 1\n    a: int computed = days_since(opened)\n")
	for k, want := range map[string]float64{"a": 0, "b": 1, "c": 2} {
		got, ok := toFloat(rev[k])
		if !ok || got != want {
			t.Errorf("reverse order: %s = %v (present=%v), want %v", k, rev[k], func() bool { _, p := rev[k]; return p }(), want)
		}
	}
	// Forward dependency order (a, b, c) must give the same result.
	fwd := build("    a: int computed = days_since(opened)\n    b: int computed = a + 1\n    c: int computed = b + 1\n")
	for k, want := range map[string]float64{"a": 0, "b": 1, "c": 2} {
		if got, ok := toFloat(fwd[k]); !ok || got != want {
			t.Errorf("forward order: %s = %v, want %v", k, fwd[k], want)
		}
	}
}
