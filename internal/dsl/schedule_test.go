package dsl

import "testing"

// ParseSchedule is the ONE parser for an `on schedule ...` trigger's raw
// text, shared by semantic.go (compile-time validation, E059) and
// internal/engine/automation.go's Tick (runtime dispatch) — see schedule.go.
// This test pins down its verdict independent of full pack compilation,
// including the token-fragmentation quirk of "HH:MM" (the lexer has no
// special case for ':' the way it does for a duration's trailing unit
// letter, so "09:00" arrives as three tokens joined with spaces).
func TestParseSchedule(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantKind   ScheduleKind
		wantInterv string
		wantHour   int
		wantMin    int
		wantOK     bool
	}{
		{"every 5m", "every 5m", ScheduleEvery, "5m", 0, 0, true},
		{"every 2h", "every 2h", ScheduleEvery, "2h", 0, 0, true},
		{"every 1d", "every 1d", ScheduleEvery, "1d", 0, 0, true},
		{"every zero rejected", "every 0m", "", "", 0, 0, false},
		{"every bad unit", "every 5x", "", "", 0, 0, false},
		{"every missing amount", "every", "", "", 0, 0, false},
		{"daily bare", "daily", ScheduleDaily, "", 0, 0, true},
		// the real shape the parser hands ParseSchedule for "daily at 09:00":
		// tokens "daily" "at" "09" ":" "00" joined with single spaces.
		{"daily at fragmented tokens", "daily at 09 : 00", ScheduleDaily, "", 9, 0, true},
		{"daily at single token", "daily at 09:00", ScheduleDaily, "", 9, 0, true},
		{"daily at midnight", "daily at 00:00", ScheduleDaily, "", 0, 0, true},
		{"daily at end of day", "daily at 23:59", ScheduleDaily, "", 23, 59, true},
		{"daily at hour out of range", "daily at 25 : 00", "", "", 0, 0, false},
		{"daily at minute out of range", "daily at 09 : 70", "", "", 0, 0, false},
		{"daily at missing clock", "daily at", "", "", 0, 0, false},
		{"empty", "", "", "", 0, 0, false},
		{"garbage cron text", "0 9 * * 1 - 5", "", "", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form, ok := ParseSchedule(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (form=%+v)", ok, tc.wantOK, form)
			}
			if !ok {
				return
			}
			if form.Kind != tc.wantKind || form.Interval != tc.wantInterv || form.Hour != tc.wantHour || form.Min != tc.wantMin {
				t.Fatalf("got %+v, want kind=%v interval=%q hour=%d min=%d", form, tc.wantKind, tc.wantInterv, tc.wantHour, tc.wantMin)
			}
		})
	}
}
