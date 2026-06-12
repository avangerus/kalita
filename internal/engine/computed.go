package engine

import (
	"strconv"
	"strings"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Computed field evaluation. Closed list of forms (DSL-SPEC §7):
//
//	days_since(path)              — whole days from a date field to now
//	path                          — plain field or one-hop ref path
//	count(Entity where f = $self) — number of related records
//	sum|avg|min|max(Entity.field where f = $self) — roll-up over related records
//
// Aggregates are evaluated on read (projections are in memory), so they stay
// consistent without invalidation — the cost is a scan, fine at SMB scale.
//
// Determinism note: guards and automation must see the same computed values
// that get recorded; the engine clock is injectable for tests and replay.

// withComputed returns a copy of values extended with evaluated computed
// fields. selfID is this record's id, exposed to aggregates as $self.
func (e *Engine) withComputed(decl *dsl.EntityDecl, selfID string, values map[string]any) map[string]any {
	out := make(map[string]any, len(values)+2)
	for k, v := range values {
		out[k] = v
	}
	for _, f := range decl.Fields {
		if f.Computed == "" {
			continue
		}
		if v, ok := e.evalComputed(f.Computed, selfID, out); ok {
			out[f.Name] = v
		}
	}
	return out
}

var aggFuncs = map[string]bool{"count": true, "sum": true, "avg": true, "min": true, "max": true}

// evalComputed evaluates a computed-field expression. It is an arithmetic
// expression ( + - * / and parentheses ) whose atoms are: number literals,
// field/ref-paths (resolved to numbers), and the function terms days_since(...)
// and the aggregates count/sum/avg/min/max(...). One closed, checkable grammar
// — NOT arbitrary code.
func (e *Engine) evalComputed(expr, selfID string, values map[string]any) (any, bool) {
	// the DSL tokenizer spaces out ref paths ("sla_policy . resolution_minutes");
	// collapse them so a dotted path is one atom (same fix as evalWhere).
	expr = reDotSpace.ReplaceAllString(strings.TrimSpace(expr), ".")
	p := &arith{s: expr, e: e, selfID: selfID, values: values}
	v, ok := p.parseExpr()
	p.skipSpace()
	if !ok || p.pos != len(p.s) {
		return nil, false
	}
	return v, true
}

// arith is a recursive-descent evaluator over the computed expression string.
type arith struct {
	s      string
	pos    int
	e      *Engine
	selfID string
	values map[string]any
}

func (p *arith) skipSpace() {
	for p.pos < len(p.s) && (p.s[p.pos] == ' ' || p.s[p.pos] == '\t') {
		p.pos++
	}
}

func (p *arith) parseExpr() (float64, bool) {
	left, ok := p.parseTerm()
	if !ok {
		return 0, false
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.s) || (p.s[p.pos] != '+' && p.s[p.pos] != '-') {
			return left, true
		}
		op := p.s[p.pos]
		p.pos++
		right, ok := p.parseTerm()
		if !ok {
			return 0, false
		}
		if op == '+' {
			left += right
		} else {
			left -= right
		}
	}
}

func (p *arith) parseTerm() (float64, bool) {
	left, ok := p.parseFactor()
	if !ok {
		return 0, false
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.s) || (p.s[p.pos] != '*' && p.s[p.pos] != '/') {
			return left, true
		}
		op := p.s[p.pos]
		p.pos++
		right, ok := p.parseFactor()
		if !ok {
			return 0, false
		}
		if op == '*' {
			left *= right
		} else {
			if right == 0 {
				return 0, false // division by zero fails the computed value
			}
			left /= right
		}
	}
}

func (p *arith) parseFactor() (float64, bool) {
	p.skipSpace()
	if p.pos >= len(p.s) {
		return 0, false
	}
	c := p.s[p.pos]
	if c == '(' {
		p.pos++
		v, ok := p.parseExpr()
		p.skipSpace()
		if !ok || p.pos >= len(p.s) || p.s[p.pos] != ')' {
			return 0, false
		}
		p.pos++
		return v, true
	}
	if c == '-' {
		p.pos++
		v, ok := p.parseFactor()
		return -v, ok
	}
	if c >= '0' && c <= '9' || c == '.' {
		return p.parseNumber()
	}
	return p.parseAtom()
}

func (p *arith) parseNumber() (float64, bool) {
	start := p.pos
	for p.pos < len(p.s) && (p.s[p.pos] >= '0' && p.s[p.pos] <= '9' || p.s[p.pos] == '.') {
		p.pos++
	}
	n, err := strconv.ParseFloat(p.s[start:p.pos], 64)
	return n, err == nil
}

// parseAtom reads an identifier; if followed by '(', it is a function term
// (days_since or an aggregate) whose balanced-paren body is dispatched to the
// existing handlers. Otherwise it is a field/ref-path resolved to a number.
func (p *arith) parseAtom() (float64, bool) {
	start := p.pos
	for p.pos < len(p.s) {
		ch := p.s[p.pos]
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '.' {
			p.pos++
			continue
		}
		break
	}
	name := p.s[start:p.pos]
	if name == "" {
		return 0, false
	}
	p.skipSpace()
	if p.pos < len(p.s) && p.s[p.pos] == '(' {
		// capture balanced parens
		depth, j := 0, p.pos
		for j < len(p.s) {
			if p.s[j] == '(' {
				depth++
			} else if p.s[j] == ')' {
				depth--
				if depth == 0 {
					break
				}
			}
			j++
		}
		if depth != 0 {
			return 0, false
		}
		body := p.s[p.pos+1 : j]
		p.pos = j + 1
		var v any
		var ok bool
		switch {
		case name == "days_since":
			v, ok = p.e.evalSince(strings.TrimSpace(body), p.values, 24*time.Hour)
		case name == "hours_since":
			v, ok = p.e.evalSince(strings.TrimSpace(body), p.values, time.Hour)
		case name == "minutes_since":
			v, ok = p.e.evalSince(strings.TrimSpace(body), p.values, time.Minute)
		case name == "business_days_since":
			v, ok = p.e.evalBusinessSince(strings.TrimSpace(body), p.values, "day")
		case name == "business_hours_since":
			v, ok = p.e.evalBusinessSince(strings.TrimSpace(body), p.values, "hour")
		case name == "business_minutes_since":
			v, ok = p.e.evalBusinessSince(strings.TrimSpace(body), p.values, "min")
		case aggFuncs[name]:
			v, ok = p.e.evalAggregate(name, body+")", p.selfID)
		default:
			return 0, false
		}
		f, fok := toFloat(v)
		return f, ok && fok
	}
	// a path: resolve and coerce to number
	raw, ok := p.e.resolvePath(name, p.values)
	if !ok {
		return 0, false
	}
	f, fok := toFloat(raw)
	return f, fok
}

// fieldDecl finds a field declaration by name (nil-safe).
func fieldDecl(decl *dsl.EntityDecl, name string) *dsl.FieldDecl {
	if decl == nil {
		return nil
	}
	for _, f := range decl.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// evalSince computes whole elapsed units (unit = 24h/1h/1m) from a date or
// datetime field to now. days_since/hours_since/minutes_since share it; the
// finer units are what sub-day SLA timers need (response/resolution minutes).
func (e *Engine) evalSince(path string, values map[string]any, unit time.Duration) (any, bool) {
	raw, ok := e.resolvePath(path, values)
	if !ok {
		return nil, false
	}
	s, _ := raw.(string)
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		if t, err = time.Parse(time.RFC3339, s); err != nil {
			return nil, false
		}
	}
	return float64(int(e.now().UTC().Sub(t) / unit)), true
}

// evalBusinessSince counts elapsed WORKING time from a date/datetime field to
// now, per a business calendar (skips weekends/holidays/off-hours). The body is
// `field` or `field, calendar_code` — the optional code selects a Calendar
// entity record (production_ru, production_us…); without it, the node default
// applies. unit is "day", "hour" or "min".
func (e *Engine) evalBusinessSince(path string, values map[string]any, unit string) (any, bool) {
	cal := e.cal
	if datePath, arg, ok := strings.Cut(path, ","); ok {
		path = strings.TrimSpace(datePath)
		arg = strings.Trim(strings.TrimSpace(arg), `"`)
		// the selector may be a literal code, or a ref-path (e.g. sla_policy.calendar)
		// resolving to a calendar code or a core.Calendar id — so each contract/SLA
		// picks its own calendar.
		if v, ok := e.resolvePath(arg, values); ok {
			if s, _ := v.(string); s != "" {
				arg = s
			}
		}
		if c, found := e.calendarByCode(arg); found {
			cal = c
		} else if c, found := e.calendarByID(arg); found {
			cal = c
		}
	}
	raw, ok := e.resolvePath(path, values)
	if !ok {
		return nil, false
	}
	s, _ := raw.(string)
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		if t, err = time.Parse(time.RFC3339, s); err != nil {
			return nil, false
		}
	}
	now := e.now().UTC()
	switch unit {
	case "day":
		return float64(cal.businessDaysBetween(t, now)), true
	case "hour":
		return float64(cal.businessMinutesBetween(t, now) / 60), true
	default:
		return float64(cal.businessMinutesBetween(t, now)), true
	}
}

// evalAggregate computes count/sum/avg/min/max over records of a target entity
// whose reffield equals selfID. body is "Entity[.field] where reffield = $self".
func (e *Engine) evalAggregate(fn, body, selfID string) (any, bool) {
	body = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body), ")"))
	target, cond, ok := strings.Cut(body, " where ")
	if !ok {
		return nil, false
	}
	entity, field, _ := strings.Cut(strings.TrimSpace(target), ".")
	entity, field = strings.TrimSpace(entity), strings.TrimSpace(field)

	// condition: reffield = $self
	refField, want, ok := strings.Cut(cond, "=")
	refField = strings.TrimSpace(refField)
	want = strings.TrimSpace(want)
	if !ok || want != "$self" {
		return nil, false
	}

	rows := e.records[entity]
	count := 0
	var acc float64
	first := true
	for _, rec := range rows {
		if id, _ := rec.Values[refField].(string); id != selfID {
			continue
		}
		count++
		if fn == "count" {
			continue
		}
		// the aggregated field may itself be computed (e.g. an order line's
		// line_total) — evaluate it for this row, not just read raw storage.
		raw := rec.Values[field]
		if fd := fieldDecl(e.model.Entities[entity], field); fd != nil && fd.Computed != "" {
			if v, ok := e.evalComputed(fd.Computed, rec.ID, rec.Values); ok {
				raw = v
			}
		}
		n, ok := toFloat(raw)
		if !ok {
			continue
		}
		switch fn {
		case "sum", "avg":
			acc += n
		case "min":
			if first || n < acc {
				acc = n
			}
		case "max":
			if first || n > acc {
				acc = n
			}
		}
		first = false
	}
	switch fn {
	case "count":
		return float64(count), true
	case "sum":
		return acc, true
	case "avg":
		if count == 0 {
			return float64(0), true
		}
		return acc / float64(count), true
	case "min", "max":
		if first {
			return float64(0), true
		}
		return acc, true
	}
	return nil, false
}

// resolvePath reads a dotted path from values, following ref hops up to two
// levels (project.owner, contract.company.name). Each hop dereferences a ref
// field's id to the referenced record.
func (e *Engine) resolvePath(path string, values map[string]any) (any, bool) {
	parts := strings.Split(strings.ReplaceAll(path, " ", ""), ".")
	cur := values
	for i, part := range parts {
		v, ok := cur[part]
		if !ok {
			return nil, false
		}
		if i == len(parts)-1 {
			return v, true
		}
		// not the last part: must be a ref id we can dereference
		refID, _ := v.(string)
		if refID == "" {
			return nil, false
		}
		next := e.lookupAny(refID)
		if next == nil {
			return nil, false
		}
		cur = next
	}
	return nil, false
}

// lookupAny finds a record by id across all entities (refs are globally
// unique uuids), returning its values.
func (e *Engine) lookupAny(id string) map[string]any {
	for _, rows := range e.records {
		if rec, ok := rows[id]; ok {
			return rec.Values
		}
	}
	return nil
}

// ctxFor builds an evaluation context with ref-path resolution and the clock.
func (e *Engine) ctxFor(selfID string, actor eventstore.Actor, values map[string]any) evalCtx {
	return evalCtx{
		values:  values,
		actorID: actor.ID,
		selfID:  selfID,
		attrs:   actor.Attrs,
		now:     e.now(),
		resolve: func(path string) (any, bool) { return e.resolvePath(path, values) },
	}
}
