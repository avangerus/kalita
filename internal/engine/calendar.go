package engine

import "time"

// businessCalendar defines working time for SLA and HR business-time math:
// which weekdays are worked, the daily working-hours window, and holidays.
// The default is Mon-Fri 09:00-18:00 with no holidays; a node can override it
// (WithBusinessCalendar). All arithmetic is in UTC — wall-clock policy is a
// deployment concern, kept simple and deterministic here.
type businessCalendar struct {
	workday  [7]bool         // indexed by time.Weekday (Sunday = 0)
	startMin int             // working day start, minutes from midnight
	endMin   int             // working day end, minutes from midnight
	holidays map[string]bool // "YYYY-MM-DD" → non-working
}

// defaultCalendar: Monday–Friday, 09:00–18:00, no holidays.
func defaultCalendar() businessCalendar {
	c := businessCalendar{startMin: 9 * 60, endMin: 18 * 60, holidays: map[string]bool{}}
	for _, d := range []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday} {
		c.workday[d] = true
	}
	return c
}

// newBusinessCalendar builds a calendar from working weekdays, an HH:MM window
// (start/end minutes from midnight) and a set of holiday dates ("YYYY-MM-DD").
func newBusinessCalendar(workdays []time.Weekday, startMin, endMin int, holidays []string) businessCalendar {
	c := businessCalendar{startMin: startMin, endMin: endMin, holidays: map[string]bool{}}
	for _, d := range workdays {
		c.workday[d] = true
	}
	for _, h := range holidays {
		c.holidays[h] = true
	}
	return c
}

var weekdayByName = map[string]time.Weekday{
	"Sun": time.Sunday, "Mon": time.Monday, "Tue": time.Tuesday, "Wed": time.Wednesday,
	"Thu": time.Thursday, "Fri": time.Friday, "Sat": time.Saturday,
}

// calendarFromRecord builds a calendar from a Calendar entity record:
//
//	workdays: array[enum[Mon..Sun]]   work_start/work_end: int (minutes)   holidays: array[date]
//
// Calendars are data — a node can hold several (production_ru, production_us…)
// and a computed picks one by code. Unspecified fields fall back to Mon-Fri 9-18.
func calendarFromRecord(values map[string]any) businessCalendar {
	c := businessCalendar{startMin: 9 * 60, endMin: 18 * 60, holidays: map[string]bool{}}
	if v, ok := numberScalar(values["work_start"]); ok {
		c.startMin = int(v)
	}
	if v, ok := numberScalar(values["work_end"]); ok {
		c.endMin = int(v)
	}
	if wd, ok := values["workdays"].([]any); ok && len(wd) > 0 {
		for _, d := range wd {
			if s, _ := d.(string); s != "" {
				if w, ok := weekdayByName[s]; ok {
					c.workday[w] = true
				}
			}
		}
	} else {
		for _, w := range []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday} {
			c.workday[w] = true
		}
	}
	if hs, ok := values["holidays"].([]any); ok {
		for _, h := range hs {
			if s, _ := h.(string); s != "" {
				c.holidays[s] = true
			}
		}
	}
	return c
}

// calendarByCode looks up a Calendar entity record by its code and builds a
// calendar from it. Returns false if no Calendar entity / record matches.
func (e *Engine) calendarByCode(code string) (businessCalendar, bool) {
	for _, rec := range e.records["core.Calendar"] {
		if c, _ := rec.Values["code"].(string); c == code {
			return calendarFromRecord(rec.Values), true
		}
	}
	return businessCalendar{}, false
}

func (c businessCalendar) isWorkday(t time.Time) bool {
	return c.workday[t.Weekday()] && !c.holidays[t.Format("2006-01-02")]
}

// businessMinutesBetween counts working minutes in (from, to], clamped to the
// daily working-hours window and skipping weekends and holidays.
func (c businessCalendar) businessMinutesBetween(from, to time.Time) int {
	from, to = from.UTC(), to.UTC()
	if !to.After(from) {
		return 0
	}
	total := 0
	day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	for guard := 0; day.Before(to) && guard < 4000; guard++ {
		if c.isWorkday(day) {
			s := day.Add(time.Duration(c.startMin) * time.Minute)
			e := day.Add(time.Duration(c.endMin) * time.Minute)
			if from.After(s) {
				s = from
			}
			if to.Before(e) {
				e = to
			}
			if e.After(s) {
				total += int(e.Sub(s).Minutes())
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return total
}

// businessDaysBetween counts whole working days whose date falls in (from, to].
func (c businessCalendar) businessDaysBetween(from, to time.Time) int {
	from, to = from.UTC(), to.UTC()
	if !to.After(from) {
		return 0
	}
	count := 0
	day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	end := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	for guard := 0; !day.After(end) && guard < 4000; guard++ {
		if c.isWorkday(day) {
			count++
		}
		day = day.AddDate(0, 0, 1)
	}
	return count
}
