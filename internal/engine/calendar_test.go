package engine

import (
	"context"
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// anchorMonday returns a Monday 00:00 UTC, so the test is independent of which
// weekday a hardcoded date happens to be.
func anchorMonday() time.Time {
	m := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for m.Weekday() != time.Monday {
		m = m.AddDate(0, 0, 1)
	}
	return m
}

func TestBusinessCalendarMath(t *testing.T) {
	cal := defaultCalendar() // Mon-Fri 09:00-18:00
	mon := anchorMonday()
	fri16 := mon.AddDate(0, 0, 4).Add(16 * time.Hour) // Friday 16:00
	nextMon11 := mon.AddDate(0, 0, 7).Add(11 * time.Hour)

	// Fri 16:00→18:00 = 120 min, weekend = 0, next Mon 09:00→11:00 = 120 min
	if got := cal.businessMinutesBetween(fri16, nextMon11); got != 240 {
		t.Errorf("business minutes Fri16→Mon11 = %d, want 240 (weekend excluded)", got)
	}
	// calendar time over the same span is far larger — proves the weekend skip
	if cal := nextMon11.Sub(fri16).Minutes(); cal < 4000 {
		t.Errorf("sanity: calendar span should be ~4020 min, got %v", cal)
	}
	// one working day elapses (the next Monday)
	if got := cal.businessDaysBetween(fri16, nextMon11); got != 1 {
		t.Errorf("business days Fri→Mon = %d, want 1", got)
	}
	// a holiday on that Monday removes its working time
	hol := newBusinessCalendar(
		[]time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday},
		9*60, 18*60, []string{nextMon11.Format("2006-01-02")})
	if got := hol.businessMinutesBetween(fri16, nextMon11); got != 120 {
		t.Errorf("with the Monday a holiday, minutes = %d, want 120 (only Friday)", got)
	}
}

// A transferred working Saturday (Russia's production-calendar quirk) counts as
// a working day — extra_workdays adds time the weekend mask would have skipped.
func TestBusinessCalendarTransferredWorkday(t *testing.T) {
	mon := anchorMonday()
	fri16 := mon.AddDate(0, 0, 4).Add(16 * time.Hour)
	sat := mon.AddDate(0, 0, 5).Format("2006-01-02")
	nextMon11 := mon.AddDate(0, 0, 7).Add(11 * time.Hour)
	cal := newBusinessCalendar(
		[]time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}, 9*60, 18*60, nil)
	// without the transfer: Fri 2h + Mon 2h = 240
	if got := cal.businessMinutesBetween(fri16, nextMon11); got != 240 {
		t.Fatalf("baseline = %d, want 240", got)
	}
	cal.extra[sat] = true // that Saturday is a transferred working day (9h)
	if got := cal.businessMinutesBetween(fri16, nextMon11); got != 240+540 {
		t.Errorf("with a working Saturday = %d, want 780 (Fri 2h + Sat 9h + Mon 2h)", got)
	}
}

// business_minutes_since flows through a computed field with the engine clock.
func TestBusinessMinutesSinceComputed(t *testing.T) {
	src := `pack t
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1
entity Tkt:
    opened: datetime
    bmin: int computed = business_minutes_since(opened)
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
	mon := anchorMonday()
	now := mon.AddDate(0, 0, 7).Add(11 * time.Hour)        // next Monday 11:00
	opened := mon.AddDate(0, 0, 4).Add(16 * time.Hour)     // Friday 16:00
	eng, err := New(ctx, model, eventstore.NewMemStore(nil), WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	op := eventstore.Actor{Type: eventstore.ActorHuman, ID: "op", Role: "Op"}
	basis := &eventstore.Basis{Type: "human", ID: "op"}
	rec, err := eng.Create(ctx, op, "Tkt", map[string]any{"opened": opened.Format(time.RFC3339)}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := rec.Values["bmin"].(float64); b != 240 {
		t.Errorf("business_minutes_since = %v, want 240 (weekend excluded)", rec.Values["bmin"])
	}
}

// A named Calendar record (data) drives the computation: production_ru with the
// next Monday a holiday removes that day's working time. Proves calendars are a
// selectable system entity, not a single node setting.
func TestNamedCalendarFromRecord(t *testing.T) {
	// core.Calendar is a built-in system entity — no need to declare it.
	src := `pack t
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1
entity Tkt:
    opened: datetime
    bmin: int computed = business_minutes_since(opened, production_ru)
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
	mon := anchorMonday()
	now := mon.AddDate(0, 0, 7).Add(11 * time.Hour)    // next Monday 11:00
	opened := mon.AddDate(0, 0, 4).Add(16 * time.Hour) // Friday 16:00
	eng, err := New(ctx, model, eventstore.NewMemStore(nil), WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	op := eventstore.Actor{Type: eventstore.ActorHuman, ID: "op", Role: "Op"}
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	basis := &eventstore.Basis{Type: "human", ID: "op"}
	// core.Calendar is written by the node's Owner; the next Monday is a holiday
	if _, err := eng.Create(ctx, owner, "core.Calendar", map[string]any{
		"code": "production_ru", "name": "Production RU",
		"workdays":   []any{"Mon", "Tue", "Wed", "Thu", "Fri"},
		"work_start": 540, "work_end": 1080,
		"holidays": []any{now.Format("2006-01-02")},
	}, basis, ""); err != nil {
		t.Fatalf("create calendar: %v", err)
	}
	rec, err := eng.Create(ctx, op, "Tkt", map[string]any{"opened": opened.Format(time.RFC3339)}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	// only Friday 16:00→18:00 counts (Monday is a holiday) -> 120
	if b, _ := rec.Values["bmin"].(float64); b != 120 {
		t.Errorf("business_minutes_since(opened, production_ru) = %v, want 120 (Monday is a holiday)", rec.Values["bmin"])
	}
}
