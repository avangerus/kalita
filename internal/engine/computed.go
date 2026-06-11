package engine

import (
	"strings"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
)

// Computed field evaluation. v0 closed list of forms (DSL-SPEC-v0 §7):
//
//	days_since(path)   — whole days from a date field to now
//	path               — plain field or one-hop ref path (contract.due_date)
//
// Determinism note: guards and automation must see the same computed values
// that get recorded; the engine clock is injectable for tests and replay.

// withComputed returns a copy of values extended with evaluated computed fields.
func (e *Engine) withComputed(decl *dsl.EntityDecl, values map[string]any) map[string]any {
	out := make(map[string]any, len(values)+2)
	for k, v := range values {
		out[k] = v
	}
	for _, f := range decl.Fields {
		if f.Computed == "" {
			continue
		}
		if v, ok := e.evalComputed(f.Computed, out); ok {
			out[f.Name] = v
		}
	}
	return out
}

func (e *Engine) evalComputed(expr string, values map[string]any) (any, bool) {
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
	return e.resolvePath(expr, values)
}

// resolvePath reads `field` or `field . sub` (one ref hop) from values.
func (e *Engine) resolvePath(path string, values map[string]any) (any, bool) {
	parts := strings.Split(strings.ReplaceAll(path, " ", ""), ".")
	switch len(parts) {
	case 1:
		v, ok := values[parts[0]]
		return v, ok
	case 2:
		refID, _ := values[parts[0]].(string)
		if refID == "" {
			return nil, false
		}
		for _, rows := range e.records {
			if rec, ok := rows[refID]; ok {
				v, ok := rec.Values[parts[1]]
				return v, ok
			}
		}
		return nil, false
	default:
		return nil, false // deeper paths are not expressible in v0
	}
}
