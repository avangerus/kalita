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

// canUpdateEntity is engine.Update's gate (1) (LLD-20): does role hold AT
// LEast one update-allow rule for entity, at any field scope, not blocked by
// an unconditional (no `where`) whole-entity or `Entity.*` deny? This proves
// only that SOME update access exists for a clean early denial ("no update
// right on this entity at all") — it is deliberately NOT a per-field or
// per-record decision. Which fields are actually writable, and record-level
// `where` denies, are gate (2)'s job (checkFieldWrites, called next with the
// real record for every touched field) — see LLD-20 §1-2. A field-scoped
// allow (`update [Entity.f]`) now satisfies this gate on its own; previously
// gate (1) queried field="" via can(), which only a whole-entity/`*` item
// can match, so a scoped-only role always failed here before ever reaching
// gate (2) (LLD-20 §0, the bug this closes).
//
// Conditional (`where`) denies are intentionally NOT evaluated here: they
// need the record, which an existence check does not have and must not
// need — record-level locks (e.g. `deny update X where status=Signed`)
// still hold because their item has Field="", which matches every field in
// gate (2)'s per-field can() call regardless of scope (LLD-20 §2/§3).
func (e *Engine) canUpdateEntity(role, entity string) decision {
	if strings.HasPrefix(entity, "core.") {
		if role == e.defApprover {
			return decision{true, ""}
		}
		return decision{false, "core reference data is managed by the " + e.defApprover + " role"}
	}
	pb, ok := e.model.Perms[role]
	if !ok {
		return decision{false, "no permissions block for role " + role}
	}

	// pass 1: an unconditional deny covering the whole entity or Entity.*
	// blocks every field, so it blocks existence outright. A field-scoped
	// deny (`deny update Entity.f`) or a conditional deny (`where ...`)
	// does not: some other field may still be writable, or the record may
	// not match the condition — both are gate (2)'s decision, not this one.
	for _, rule := range pb.Rules {
		if rule.Verb != "deny" {
			continue
		}
		for _, item := range rule.Items {
			if item.Where != "" {
				continue
			}
			if !verbGrantsUpdate(item.Verb) {
				continue
			}
			if !item.All && item.Entity != entity {
				continue
			}
			if item.All || item.Field == "" || item.Field == "*" {
				return decision{false, denyRuleText(item)}
			}
		}
	}

	// pass 2: any update (or full) rule targeting this entity, at any field
	// scope, proves access exists. Field is deliberately not checked here.
	for _, rule := range pb.Rules {
		if !verbGrantsUpdate(rule.Verb) {
			continue
		}
		for _, item := range rule.Items {
			if item.All || item.Entity == entity {
				return decision{true, ""}
			}
		}
	}
	return decision{false, "default deny: no allow rule for update " + entity}
}

func verbGrantsUpdate(v string) bool {
	return v == "update" || v == "full"
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
