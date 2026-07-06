package dsl

import "strings"

// This file holds the ONE parser for a computed-field aggregate call's body —
// shared by the compiler (semantic.go, catching an unsupported where-clause
// form as a compile error) and the engine at runtime
// (internal/engine/computed.go's evalAggregate, which calls ParseAggregateBody
// instead of re-parsing the string). One parser, so the two layers can never
// disagree about what is supported (I-3: "error is a signal, never silence").

// aggFuncNames is the closed list of aggregate computed-field functions
// (DSL-SPEC §7): count(Entity where ...), sum|avg|min|max(Entity.field where
// ...). Mirrors engine's own aggFuncs (internal/engine/computed.go), kept
// separate so this package does not depend on the engine.
var aggFuncNames = map[string]bool{"count": true, "sum": true, "avg": true, "min": true, "max": true}

// findAggregateCalls scans a computed-field expression for aggregate function
// calls and returns each call's body — the text between its balanced parens.
// A computed expression may combine an aggregate with arithmetic (e.g.
// `annual_quota - sum(LeaveRequest.days where employee = $self)`), so this
// does not assume the whole expression is one call.
//
// expr is expected in the parser's token-joined form (single space between
// tokens — see exprUntilKeyword in parser.go), which is also the form
// engine.evalComputed operates on. Splitting on a single space is therefore
// a safe, exact tokenization here (computed expressions carry no string
// literals that could contain spaces).
func findAggregateCalls(expr string) []string {
	toks := strings.Split(expr, " ")
	var bodies []string
	for i := 0; i < len(toks); i++ {
		if !aggFuncNames[toks[i]] || i+1 >= len(toks) || toks[i+1] != "(" {
			continue
		}
		depth := 0
		start := i + 2
		j := start
		for ; j < len(toks); j++ {
			if toks[j] == "(" {
				depth++
			} else if toks[j] == ")" {
				if depth == 0 {
					break
				}
				depth--
			}
		}
		if j < len(toks) { // found the matching close paren
			bodies = append(bodies, strings.Join(toks[start:j], " "))
		}
	}
	return bodies
}

// ParseAggregateBody parses an aggregate call's body — the text between its
// parens, e.g. "Finding where contract = $self" or
// "LeaveRequest . days where employee = $self" — into the target entity, the
// aggregated field (empty for count), and the ref field compared to $self.
// ok is false for anything but the one supported condition form:
// `reffield = $self` (DSL-SPEC §7). This is the exact parsing
// internal/engine/computed.go's evalAggregate used to do inline; moved here
// so semantic.go can validate the same form at compile time.
func ParseAggregateBody(body string) (entity, field, refField string, ok bool) {
	body = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body), ")"))
	target, cond, hasWhere := strings.Cut(body, " where ")
	if !hasWhere {
		return "", "", "", false
	}
	entity, field, _ = strings.Cut(strings.TrimSpace(target), ".")
	entity, field = strings.TrimSpace(entity), strings.TrimSpace(field)

	// condition: reffield = $self — the one supported form.
	refField, want, hasEq := strings.Cut(cond, "=")
	refField = strings.TrimSpace(refField)
	want = strings.TrimSpace(want)
	if !hasEq || want != "$self" {
		return entity, field, "", false
	}
	return entity, field, refField, true
}
