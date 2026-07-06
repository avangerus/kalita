package dsl

import (
	"strconv"
	"strings"
)

// This file holds the ONE parser for an `on schedule ...` trigger's raw text
// — shared by the compiler (semantic.go, catching an unsupported schedule
// form as a compile error) and the engine at runtime
// (internal/engine/automation.go's Tick, which calls ParseSchedule instead of
// re-parsing the string). One parser, so the two layers can never disagree
// about what a schedule means (I-3), mirroring the ParseAggregateBody
// precedent in aggregate.go (TECHDEBT "Ядро: automation / schedule" P1).
//
// raw is already in the parser's token-joined form (single space between
// tokens — see automationTrigger in parser_blocks.go, same convention
// findAggregateCalls documents in aggregate.go). The lexer has no special
// case for "HH:MM" the way it does for duration literals ("5m" folds into
// one token because a unit letter immediately follows the digits — see
// tokenize in lexer.go); ':' is plain punctuation, so "09:00" arrives here
// as THREE tokens: "09", ":", "00". ParseSchedule accounts for that shape
// rather than assuming a single clock token.

// ScheduleKind is the closed list of schedule trigger forms. A full cron
// grammar (minutes/hours/weekdays in combination) is deliberately out of
// scope — these two forms are what the runtime can honor honestly.
type ScheduleKind string

const (
	ScheduleEvery ScheduleKind = "every" // "every <dur>" — fires once per interval window
	ScheduleDaily ScheduleKind = "daily" // "daily" or "daily at HH:MM" — fires once per day, at/after that time of day
)

// ScheduleForm is the parsed form of a schedule trigger's raw text.
type ScheduleForm struct {
	Kind     ScheduleKind
	Interval string // raw duration text for Kind==ScheduleEvery, e.g. "5m" — engine.ParseDuration turns it into a time.Duration
	Hour     int    // wall-clock hour (0-23) for Kind==ScheduleDaily; 0 when "daily" carries no "at"
	Min      int    // wall-clock minute (0-59) for Kind==ScheduleDaily; 0 when "daily" carries no "at"
}

// scheduleDurationOK mirrors engine.parseDuration's closed unit set (10d /
// 48h / 30m) just enough to validate the FORM of "every <dur>" at compile
// time. Kept separate so this package does not depend on the engine — same
// reasoning as aggFuncNames in aggregate.go. The engine remains the one place
// that turns accepted text into an actual time.Duration.
func scheduleDurationOK(s string) bool {
	if s == "" {
		return false
	}
	unit := s[len(s)-1]
	if unit != 'd' && unit != 'h' && unit != 'm' {
		return false
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	return err == nil && n > 0
}

// ParseSchedule parses an `on schedule ...` trigger's raw text into its
// closed form. ok is false for anything but `every <dur>` (a positive integer
// with unit d/h/m) or `daily` / `daily at HH:MM` (HH/MM in range, 24h).
//
// The HH:MM clock is accepted both as one token ("09:00", should a future
// lexer change fold it like a duration literal) and as the three tokens the
// current lexer actually produces for it ("09", ":", "00") — rejoined here
// before splitting on ':' so the grammar does not depend on that lexer detail.
func ParseSchedule(raw string) (ScheduleForm, bool) {
	f := strings.Fields(raw)
	switch {
	case len(f) == 2 && f[0] == "every":
		if !scheduleDurationOK(f[1]) {
			return ScheduleForm{}, false
		}
		return ScheduleForm{Kind: ScheduleEvery, Interval: f[1]}, true
	case len(f) == 1 && f[0] == "daily":
		return ScheduleForm{Kind: ScheduleDaily}, true
	case len(f) >= 3 && f[0] == "daily" && f[1] == "at":
		clock := strings.Join(f[2:], "")
		hh, mm, found := strings.Cut(clock, ":")
		if !found || hh == "" || mm == "" {
			return ScheduleForm{}, false
		}
		h, errH := strconv.Atoi(hh)
		m, errM := strconv.Atoi(mm)
		if errH != nil || errM != nil || h < 0 || h > 23 || m < 0 || m > 59 {
			return ScheduleForm{}, false
		}
		return ScheduleForm{Kind: ScheduleDaily, Hour: h, Min: m}, true
	default:
		return ScheduleForm{}, false
	}
}
