package engine

import (
	"strings"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
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

func (e *Engine) evalComputed(expr, selfID string, values map[string]any) (any, bool) {
	expr = strings.TrimSpace(expr)
	if rest, ok := strings.CutPrefix(expr, "days_since ("); ok {
		path := strings.TrimSpace(strings.TrimSuffix(rest, ")"))
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
		return float64(int(e.now().UTC().Sub(t).Hours() / 24)), true
	}
	// aggregate: fn ( Entity[.field] where reffield = $self )
	if i := strings.IndexByte(expr, '('); i > 0 && aggFuncs[strings.TrimSpace(expr[:i])] {
		return e.evalAggregate(strings.TrimSpace(expr[:i]), expr[i+1:], selfID)
	}
	return e.resolvePath(expr, values)
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
		n, ok := toFloat(rec.Values[field])
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
func (e *Engine) ctxFor(selfID, actorID string, values map[string]any) evalCtx {
	return evalCtx{
		values:  values,
		actorID: actorID,
		selfID:  selfID,
		now:     e.now(),
		resolve: func(path string) (any, bool) { return e.resolvePath(path, values) },
	}
}
