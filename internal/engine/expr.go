package engine

import (
	"strconv"
	"strings"
)

// Minimal evaluator for v0 row-level `where` expressions and default values.
// Grammar (deliberately poor, DSL-SPEC-v0 §7): comparisons joined by `and`:
//
//	field = literal | field != literal | field = $me | field in [a, b]
//
// The full expression engine (guards, computed) arrives in week 4; this
// evaluator must stay a strict subset of it.

type evalCtx struct {
	values  map[string]any // record values
	actorID string         // $me
}

// evalWhere returns whether the expression matches the record. Unknown or
// malformed expressions evaluate to false — permission-relevant code fails
// closed, never open.
func evalWhere(expr string, c evalCtx) bool {
	for _, clause := range strings.Split(expr, " and ") {
		if !evalClause(strings.TrimSpace(clause), c) {
			return false
		}
	}
	return true
}

func evalClause(clause string, c evalCtx) bool {
	for _, op := range []string{"!=", "="} {
		if i := strings.Index(clause, op); i > 0 {
			field := strings.TrimSpace(clause[:i])
			lit := strings.TrimSpace(clause[i+len(op):])
			got, ok := c.values[field]
			if !ok {
				return false
			}
			eq := literalEquals(lit, got, c)
			if op == "=" {
				return eq
			}
			return !eq
		}
	}
	return false
}

func literalEquals(lit string, got any, c evalCtx) bool {
	want := evalLiteral(lit, c)
	switch w := want.(type) {
	case float64:
		if g, ok := toFloat(got); ok {
			return g == w
		}
		return false
	default:
		return got == want
	}
}

// evalLiteral resolves a literal token: $me, true/false, number, quoted or
// bare string (enum values are bare idents).
func evalLiteral(lit string, c evalCtx) any {
	switch lit {
	case "$me":
		return c.actorID
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.ParseFloat(lit, 64); err == nil {
		return n
	}
	return strings.Trim(lit, `"`)
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
