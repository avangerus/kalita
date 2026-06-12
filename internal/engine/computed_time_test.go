package engine

import (
	"context"
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// days_since / hours_since / minutes_since over an injected clock, and the
// $now default landing at the real clock (not the zero time) — the pieces a
// sub-day SLA timer needs.
func TestTimeSinceAndNowDefault(t *testing.T) {
	src := `pack t
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1
entity Tkt:
    opened: datetime default=$now
    d: int computed = days_since(opened)
    h: int computed = hours_since(opened)
    m: int computed = minutes_since(opened)
roles:
    Op
permissions:
    Op:
        full [Tkt]
`
	model, errs := dsl.Compile(map[string]string{"t.dsl": src})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	ctx := context.Background()
	at := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	eng, err := New(ctx, model, eventstore.NewMemStore(nil), WithClock(func() time.Time { return at }))
	if err != nil {
		t.Fatal(err)
	}
	op := eventstore.Actor{Type: eventstore.ActorHuman, ID: "op", Role: "Op"}
	basis := &eventstore.Basis{Type: "human", ID: "op"}

	// (1) default=$now lands at the clock, so elapsed is zero at creation
	rec, err := eng.Create(ctx, op, "Tkt", map[string]any{}, basis, "")
	if err != nil {
		t.Fatalf("create with $now default: %v", err)
	}
	for _, k := range []string{"d", "h", "m"} {
		if f, _ := toFloat(rec.Values[k]); f != 0 {
			t.Errorf("%s at creation = %v, want 0 ($now default must use the clock)", k, rec.Values[k])
		}
	}

	// (2) an explicit opened 90 minutes in the past
	past := at.Add(-90 * time.Minute).Format(time.RFC3339)
	rec2, err := eng.Create(ctx, op, "Tkt", map[string]any{"opened": past}, basis, "")
	if err != nil {
		t.Fatalf("create with explicit opened: %v", err)
	}
	want := map[string]float64{"d": 0, "h": 1, "m": 90}
	for k, exp := range want {
		if f, _ := toFloat(rec2.Values[k]); f != exp {
			t.Errorf("%s_since 90min ago = %v, want %v", k, rec2.Values[k], exp)
		}
	}
}

// days_until / hours_until / minutes_until are the forward mirror of *_since:
// positive while the target is in the future, negative once it has passed — so
// "days_left = days_until(end_date)" reads the way a contract or SLA is spoken.
func TestTimeUntil(t *testing.T) {
	src := `pack t
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1
entity Sub:
    ends: datetime
    d: int computed = days_until(ends)
    h: int computed = hours_until(ends)
    m: int computed = minutes_until(ends)
roles:
    Op
permissions:
    Op:
        full [Sub]
`
	model, errs := dsl.Compile(map[string]string{"t.dsl": src})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	ctx := context.Background()
	at := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	eng, err := New(ctx, model, eventstore.NewMemStore(nil), WithClock(func() time.Time { return at }))
	if err != nil {
		t.Fatal(err)
	}
	op := eventstore.Actor{Type: eventstore.ActorHuman, ID: "op", Role: "Op"}
	basis := &eventstore.Basis{Type: "human", ID: "op"}

	// 90 minutes in the FUTURE -> positive
	future := at.Add(90 * time.Minute).Format(time.RFC3339)
	rec, err := eng.Create(ctx, op, "Sub", map[string]any{"ends": future}, basis, "")
	if err != nil {
		t.Fatalf("create future: %v", err)
	}
	for k, exp := range map[string]float64{"d": 0, "h": 1, "m": 90} {
		if f, _ := toFloat(rec.Values[k]); f != exp {
			t.Errorf("%s_until 90min ahead = %v, want %v", k, rec.Values[k], exp)
		}
	}

	// a date in the PAST -> negative (an expired subscription / overdue contract)
	past := at.Add(-48 * time.Hour).Format(time.RFC3339)
	rec2, err := eng.Create(ctx, op, "Sub", map[string]any{"ends": past}, basis, "")
	if err != nil {
		t.Fatalf("create past: %v", err)
	}
	if f, _ := toFloat(rec2.Values["d"]); f != -2 {
		t.Errorf("days_until 2 days ago = %v, want -2", rec2.Values["d"])
	}
}
