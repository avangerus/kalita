package engine

import (
	"fmt"
	"strings"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Permission engine. Semantics (DSL-SPEC-v0 §5):
//
//	default deny — no rule, no access
//	deny > allow — a matching deny always wins
//	full = read+create+update+delete on the target
//	row-level: `where` on allow restricts, on deny extends the denial
//	field-level: Entity.field / Entity.* targets
//
// Every decision is pure over (model, role, verb, entity, field, record) —
// the same function answers the API, the journal and (later) the simulator.

type decision struct {
	allowed bool
	rule    string // human-readable rule that decided, for PERMISSION_DENIED
}

// can decides verb on entity for a role. record may be nil (create, or
// schema-level checks): row-level conditions on a nil record are treated as
// matching for deny (fail closed) and as satisfied for allow only when the
// allow has no condition.
func (e *Engine) can(actor eventstore.Actor, verb, entity, field string, record map[string]any) decision {
	role := actor.Role
	// built-in core reference data (core.Calendar…): every authenticated actor
	// reads it; only the node's definition approver writes it. Packs do not
	// grant permissions on core.* entities — the kernel owns the policy.
	if strings.HasPrefix(entity, "core.") {
		if verb == "read" {
			return decision{true, ""}
		}
		if role == e.defApprover {
			return decision{true, ""}
		}
		return decision{false, "core reference data is managed by the " + e.defApprover + " role"}
	}

	pb, ok := e.model.Perms[role]
	if !ok {
		return decision{false, "no permissions block for role " + role}
	}

	// pass 1: denies
	for _, rule := range pb.Rules {
		if rule.Verb != "deny" {
			continue
		}
		for _, item := range rule.Items {
			if itemMatches(item, verb, entity, field) && e.whereMatchesForDeny(item, record, actor) {
				return decision{false, denyRuleText(item)}
			}
		}
	}

	// pass 2: allows
	for _, rule := range pb.Rules {
		verbs := []string{rule.Verb}
		if rule.Verb == "full" {
			verbs = []string{"read", "create", "update", "delete"}
		}
		for _, v := range verbs {
			if v != verb {
				continue
			}
			for _, item := range rule.Items {
				it := item
				it.Verb = v
				if !itemMatches(it, verb, entity, field) {
					continue
				}
				if item.Where == "" {
					return decision{true, ""}
				}
				if record != nil && evalWhere(item.Where, e.ctxFor("", actor, record)) {
					return decision{true, ""}
				}
			}
		}
	}
	return decision{false, "default deny: no allow rule for " + verb + " " + entity}
}

// itemMatches checks verb/entity/field match, ignoring where.
func itemMatches(item dsl.PermItem, verb, entity, field string) bool {
	if item.Verb != verb && !(item.Verb == "full" && isCrud(verb)) {
		return false
	}
	if item.All {
		return true
	}
	if item.Entity != entity {
		return false
	}
	switch item.Field {
	case "":
		return true // whole entity
	case "*":
		return true // any field of entity
	default:
		// field-scoped rule applies to that field only; a whole-record check
		// (field == "") is not constrained by field-scoped denies
		return field == item.Field
	}
}

func isCrud(v string) bool {
	return v == "read" || v == "create" || v == "update" || v == "delete"
}

// whereMatchesForDeny: a conditional deny without a record fails closed only
// for reads of concrete records; for record==nil (create/schema checks) a
// conditional deny cannot be evaluated and does not match.
func (e *Engine) whereMatchesForDeny(item dsl.PermItem, record map[string]any, actor eventstore.Actor) bool {
	if item.Where == "" {
		return true
	}
	if record == nil {
		return false
	}
	return evalWhere(item.Where, e.ctxFor("", actor, record))
}

func denyRuleText(item dsl.PermItem) string {
	t := "deny " + item.Verb
	switch {
	case item.All:
		t += " *"
	case item.Field != "":
		t += " " + item.Entity + "." + item.Field
	case item.Entity != "":
		t += " " + item.Entity
	}
	if item.Where != "" {
		t += " where " + item.Where
	}
	return t
}

// maskFields strips fields the actor may not read from a record copy.
func (e *Engine) maskFields(actor eventstore.Actor, entity string, values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for k, v := range values {
		if d := e.can(actor, "read", entity, k, values); d.allowed {
			out[k] = v
		}
	}
	return out
}

// checkFieldWrites verifies every written field is allowed for the actor.
func (e *Engine) checkFieldWrites(actor eventstore.Actor, verb, entity string, fields map[string]any, record map[string]any) *Err {
	for f := range fields {
		if d := e.can(actor, verb, entity, f, record); !d.allowed {
			return &Err{
				Code:    CodePermissionDenied,
				Message: fmt.Sprintf("role %s may not %s field %s.%s", actor.Role, verb, entity, f),
				Rule:    d.rule,
			}
		}
	}
	return nil
}
