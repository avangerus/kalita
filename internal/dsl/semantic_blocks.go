package dsl

import (
	"fmt"
	"sort"
	"strings"
)

// Semantic analysis of workflow / automation / ui blocks (week 4 gate).

func analyzeBlocks(ast *AST, m *Model, errs *Errors) {
	actions := map[string]bool{}          // global action names (did-you-mean fallback)
	entityActions := map[string][]string{} // entity → its transition action names (LLD-01 §4c)

	for _, wf := range ast.Workflows {
		if prev, dup := m.Workflows[wf.Entity]; dup {
			errs.add(EDupWorkflow, wf.File, wf.Line,
				fmt.Sprintf("entity %s already has a workflow at line %d (one per entity in v0)", wf.Entity, prev.Line),
				"merge the transitions into one workflow block")
			continue
		}
		e, ok := m.Entities[wf.Entity]
		if !ok {
			errs.add(EUnknownEntity, wf.File, wf.Line, "workflow for unknown entity "+wf.Entity,
				"declare the entity before its workflow")
			continue
		}
		field := findField(e, wf.Field)
		if field == nil || field.Type.Kind != TyEnum {
			errs.add(EWorkflowField, wf.File, wf.Line,
				fmt.Sprintf("workflow field %s.%s must be a declared enum field", wf.Entity, wf.Field),
				"declare the field as enum[...] in the entity; the workflow governs its values")
			continue
		}
		m.Workflows[wf.Entity] = wf

		states := map[string]bool{}
		for _, v := range field.Type.EnumValues {
			states[v] = true
		}
		seenActions := map[string]int{}
		for _, tr := range wf.Transitions {
			if tr.From != "any" && !states[tr.From] {
				errs.add(EUnknownState, wf.File, tr.Line, "unknown state "+tr.From,
					"states are the enum values of "+wf.Entity+"."+wf.Field+"; `any` matches all")
			}
			if !states[tr.To] {
				errs.add(EUnknownState, wf.File, tr.Line, "unknown state "+tr.To,
					"states are the enum values of "+wf.Entity+"."+wf.Field)
			}
			if !tr.Auto {
				if prev, dup := seenActions[tr.Action]; dup {
					errs.add(EDupAction, wf.File, tr.Line,
						fmt.Sprintf("action %s already declared at line %d", tr.Action, prev),
						"action names are unique within a workflow; transitions sharing an action are not allowed in v0")
				}
				seenActions[tr.Action] = tr.Line
				actions[tr.Action] = true
				entityActions[wf.Entity] = append(entityActions[wf.Entity], tr.Action)
			}
			if tr.AssigneeRole != "" {
				checkRole(m, errs, wf.File, tr.Line, tr.AssigneeRole, tr.AssigneeAgent)
			}
			if tr.ApprovalRole != "" {
				checkRole(m, errs, wf.File, tr.Line, tr.ApprovalRole, false)
			}
			for _, ec := range tr.Emits {
				checkEmit(m, errs, wf.File, ec.Line, ec.Event, ec.Args)
			}
		}
	}

	for _, rule := range ast.Automations {
		if rule.Trigger == "event" {
			checkEventTrigger(m, errs, rule)
		} else if rule.Entity != "" {
			e, ok := m.Entities[rule.Entity]
			if !ok {
				errs.add(EUnknownEntity, rule.File, rule.Line, "automation references unknown entity "+rule.Entity,
					"reference an entity declared in this pack")
				continue
			}
			if rule.Trigger == "stuck" {
				wf, ok := m.Workflows[rule.Entity]
				if !ok {
					errs.add(EBadAutomation, rule.File, rule.Line,
						"stuck trigger requires "+rule.Entity+" to have a workflow",
						"declare a workflow for the entity; stuck watches its states")
				} else if f := findField(e, wf.Field); f != nil && !contains(f.Type.EnumValues, rule.StuckState) {
					errs.add(EUnknownState, rule.File, rule.Line, "unknown state "+rule.StuckState,
						"states are the enum values of "+rule.Entity+"."+wf.Field)
				}
			}
		}
		// the record type whose fields the create/update expressions read: the
		// trigger record (for record triggers) — used only to validate that
		// update's self target is coherent; runtime binds the actual context.
		triggerEntity := rule.Entity
		for _, a := range rule.Actions {
			switch a.Kind {
			case "agent":
				checkRole(m, errs, rule.File, a.Line, a.Role, true)
			case "escalate":
				checkRole(m, errs, rule.File, a.Line, a.Role, false)
			case "emit":
				checkEmit(m, errs, rule.File, a.Line, a.Event, a.Assigns)
			case "create":
				checkCreateAction(m, errs, rule.File, a)
			case "update":
				checkUpdateAction(m, errs, rule.File, triggerEntity, a)
			}
		}
	}

	for _, ui := range ast.UIs {
		e, ok := m.Entities[ui.Entity]
		if !ok {
			errs.add(EUnknownEntity, ui.File, ui.Line, "ui block for unknown entity "+ui.Entity,
				"declare the entity before its ui block")
			continue
		}
		for _, ref := range ui.FieldRefs {
			if findField(e, ref.Field) == nil {
				errs.add(EUIUnknownField, ui.File, ref.Line,
					fmt.Sprintf("ui references unknown field %s.%s", ui.Entity, ref.Field),
					"list/filters/section may only reference declared fields")
			}
		}
		if ui.BoardBy != "" && findField(e, ui.BoardBy) == nil {
			errs.add(EUIUnknownField, ui.File, ui.Line,
				fmt.Sprintf("board by unknown field %s.%s", ui.Entity, ui.BoardBy),
				"board groups by a declared enum field")
		}
	}

	for _, d := range ast.Dashboards {
		for _, tile := range d.Tiles {
			e, ok := m.Entities[tile.Entity]
			if !ok {
				errs.add(EDashboardEntity, d.File, tile.Line,
					fmt.Sprintf("dashboard tile %q aggregates over unknown entity %s", tile.Label, tile.Entity),
					"reference an entity declared in this pack")
				continue
			}
			if tile.Field != "" && findField(e, tile.Field) == nil {
				errs.add(EDashboardField, d.File, tile.Line,
					fmt.Sprintf("dashboard tile %q aggregates unknown field %s.%s", tile.Label, tile.Entity, tile.Field),
					tile.Func+" needs a declared numeric field of "+tile.Entity)
			}
			if tile.GroupBy != "" && findField(e, tile.GroupBy) == nil {
				errs.add(EDashboardField, d.File, tile.Line,
					fmt.Sprintf("dashboard tile %q groups by unknown field %s.%s", tile.Label, tile.Entity, tile.GroupBy),
					"group by a declared field of "+tile.Entity)
			}
		}
	}

	// LLD-01 §4c: act/approve/deny action names must exist as a transition of an
	// entity the role has some grant on — a name matching no reachable
	// transition is a silent "protection that isn't", so it fails compilation
	// with a did-you-mean hint (nearest transition by Levenshtein distance).
	allActions := make([]string, 0, len(actions))
	for a := range actions {
		allActions = append(allActions, a)
	}
	sort.Strings(allActions)

	// LLD-01 §4.2/§5 (PR-5): same did-you-mean discipline for `emit [Name]`
	// grants — a name that is not one of this pack's own declared events is a
	// silent "protection that isn't" exactly like an unknown act/approve name.
	eventNames := make([]string, 0, len(m.Events))
	for name := range m.Events {
		eventNames = append(eventNames, name)
	}
	sort.Strings(eventNames)

	for _, role := range sortedKeys(m.Perms) {
		pb := m.Perms[role]
		scoped := scopedActions(m, pb, entityActions)
		for _, rule := range pb.Rules {
			for _, item := range rule.Items {
				switch item.Verb {
				case "act", "approve":
					for _, name := range item.Names {
						if scoped[name] {
							continue
						}
						hint := "action names come from workflow transitions of entities this role can access"
						if near := nearest(name, keys(scoped)); near != "" {
							hint = "did you mean " + near + "? " + hint
						} else if near := nearest(name, allActions); near != "" {
							hint = "did you mean " + near + "? (note: " + near + " belongs to an entity " + role + " has no grant on)"
						}
						errs.add(EUnknownAction, "", item.Line,
							fmt.Sprintf("permissions of %s reference unknown action %s", role, name),
							hint)
					}
				case "emit":
					for _, name := range item.Names {
						if _, ok := m.Events[name]; ok {
							continue
						}
						hint := "event names come from `event` declarations in this pack"
						if near := nearest(name, eventNames); near != "" {
							hint = "did you mean " + near + "? " + hint
						}
						errs.add(EUnknownEmitGrant, "", item.Line,
							fmt.Sprintf("permissions of %s reference unknown event %s", role, name),
							hint)
					}
				}
			}
		}
	}
}

// scopedActions is the set of transition action names a role may name in
// act/approve/deny lists: the transitions of every entity the role has any
// (non-deny) grant on. `all`/`*` grants widen it to every entity's transitions.
func scopedActions(m *Model, pb *PermBlock, entityActions map[string][]string) map[string]bool {
	out := map[string]bool{}
	all := false
	granted := map[string]bool{}
	for _, rule := range pb.Rules {
		if rule.Verb == "deny" {
			continue
		}
		for _, item := range rule.Items {
			if item.All {
				all = true
			} else if item.Entity != "" {
				granted[item.Entity] = true
			}
		}
	}
	for ent, acts := range entityActions {
		if all || granted[ent] {
			for _, a := range acts {
				out[a] = true
			}
		}
	}
	return out
}

// checkEmit resolves an emitted event and validates its payload against the
// event schema (LLD-01 §4). Used by both `emits` clauses and `emit` actions.
func checkEmit(m *Model, errs *Errors, file string, line int, ref string, args []Assign) {
	decl, _, own := m.EventByRef(ref)
	if !own {
		errs.add(EEmitForeignPack, file, line,
			"cannot emit "+ref+": a pack may only emit events it declares",
			"emit an event declared in this pack; subscribe to other packs' events with `on event "+ref+"`")
		return
	}
	if decl == nil {
		errs.add(EUnknownEvent, file, line, "emit references unknown event "+ref,
			"declare `event "+shortName(ref)+":` with its payload fields, or fix the name")
		return
	}
	checkPayload(m, errs, file, line, decl, args)
}

// checkPayload validates emit payload field=expr pairs against an event's
// schema: unknown fields, missing required fields, and literal type mismatches
// (LLD-01 §4: "несовпадение типа поля payload со схемой события — ошибка").
func checkPayload(m *Model, errs *Errors, file string, line int, decl *EventDecl, args []Assign) {
	byName := map[string]*FieldDecl{}
	for _, f := range decl.Fields {
		byName[f.Name] = f
	}
	given := map[string]bool{}
	for _, a := range args {
		f, ok := byName[a.Field]
		if !ok {
			errs.add(EEventPayload, file, a.Line,
				fmt.Sprintf("event %s has no payload field %s", decl.Name, a.Field),
				"payload fields: "+eventFieldNames(decl))
			continue
		}
		given[a.Field] = true
		if msg := literalTypeMismatch(f, a.Expr); msg != "" {
			errs.add(EEventPayload, file, a.Line,
				fmt.Sprintf("event %s field %s: %s", decl.Name, a.Field, msg),
				"pass a value matching the payload field's declared type")
		}
	}
	for _, f := range decl.Fields {
		if f.Required && !given[f.Name] {
			errs.add(EEventPayload, file, line,
				fmt.Sprintf("event %s payload is missing required field %s", decl.Name, f.Name),
				"add `"+f.Name+" = <expr>` to the emit")
		}
	}
}

// literalTypeMismatch reports a type error when the expr is a bare literal that
// cannot match the field type. Non-literal expressions (paths, arithmetic) are
// checked at runtime against the record/payload context.
func literalTypeMismatch(f *FieldDecl, expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return ""
	}
	isString := len(expr) >= 2 && expr[0] == '"' && expr[len(expr)-1] == '"'
	isNumber := true
	for _, r := range expr {
		if !(r >= '0' && r <= '9' || r == '.' || r == '-') {
			isNumber = false
			break
		}
	}
	if !isString && !isNumber {
		return "" // a path / expression — validated at runtime
	}
	if f.Type.Kind != TyScalar {
		if isString || isNumber {
			// ref/enum/array payloads take ids/values, not bare number/string literals
			if f.Type.Kind == TyRef {
				return "" // a ref id is a string; allow (existence checked at runtime)
			}
		}
		return ""
	}
	switch f.Type.Scalar {
	case "int", "float", "money", "decimal", "percent":
		if isString {
			return "expects a number, got a string literal"
		}
	case "bool":
		if isString || isNumber {
			return "expects true/false"
		}
	case "string", "text", "email", "url", "phone", "color", "duration", "date", "datetime":
		if isNumber {
			return "expects a string, got a number literal"
		}
	}
	return ""
}

// checkEventTrigger validates an `on event [pack.]Name` subscription (LLD-01
// §4.1): the event must exist in this pack, or — for a qualified name — the
// emitter pack must be present in this build.
func checkEventTrigger(m *Model, errs *Errors, rule *AutomationRule) {
	decl, _, own := m.EventByRef(rule.Event)
	if !own {
		errs.add(EEventPackAbsent, rule.File, rule.Line,
			"on event "+rule.Event+": pack "+packOf(rule.Event)+" is not part of this build",
			"install/compose the emitting pack, or subscribe to an event declared here")
		return
	}
	if decl == nil {
		errs.add(EUnknownEvent, rule.File, rule.Line,
			"on event references unknown event "+rule.Event,
			"declare `event "+shortName(rule.Event)+":` or fix the name")
	}
}

// checkCreateAction validates a `create Entity(field=expr, ...)` action.
func checkCreateAction(m *Model, errs *Errors, file string, a AutomationAction) {
	e, ok := m.Entities[a.Entity]
	if !ok {
		errs.add(EUnknownEntity, file, a.Line, "create references unknown entity "+a.Entity,
			"reference an entity declared in this pack")
		return
	}
	for _, as := range a.Assigns {
		if findField(e, as.Field) == nil {
			errs.add(EBadActionExpr, file, as.Line,
				fmt.Sprintf("create %s: unknown field %s", a.Entity, as.Field),
				"assign only declared fields of "+a.Entity)
		}
	}
}

// checkUpdateAction validates an `update [ref] set field=expr, ...` action and
// enforces the HITL guard: a workflow-governed field can never be written
// directly (LLD-01 §4b — use an explicit transition instead).
func checkUpdateAction(m *Model, errs *Errors, file, triggerEntity string, a AutomationAction) {
	target := triggerEntity
	if a.RefPath != "" {
		// one hop: the ref field must be a ref of the trigger entity
		te, ok := m.Entities[triggerEntity]
		if !ok {
			return // trigger-entity error already reported (or event-trigger update)
		}
		rf := findField(te, a.RefPath)
		if rf == nil || rf.Type.Kind != TyRef {
			errs.add(EBadActionExpr, file, a.Line,
				fmt.Sprintf("update %s: %s is not a ref field of %s", a.RefPath, a.RefPath, triggerEntity),
				"update <ref> set ... follows one ref field of the trigger record")
			return
		}
		target = rf.Type.RefTarget
	}
	te, ok := m.Entities[target]
	if !ok {
		return
	}
	wf := m.Workflows[target]
	for _, as := range a.Assigns {
		if wf != nil && as.Field == wf.Field {
			errs.add(EUpdateWorkflow, file, as.Line,
				fmt.Sprintf("update may not write %s.%s: it is governed by the workflow (HITL bypass)", target, as.Field),
				"move the state with an explicit transition (act ...), not a direct field write")
			continue
		}
		if findField(te, as.Field) == nil {
			errs.add(EBadActionExpr, file, as.Line,
				fmt.Sprintf("update %s: unknown field %s", target, as.Field),
				"assign only declared fields of "+target)
		}
	}
}

// --- small helpers -----------------------------------------------------------

func shortName(ref string) string {
	if dot := strings.LastIndexByte(ref, '.'); dot >= 0 {
		return ref[dot+1:]
	}
	return ref
}

func packOf(ref string) string {
	if dot := strings.IndexByte(ref, '.'); dot >= 0 {
		return ref[:dot]
	}
	return ""
}

func eventFieldNames(ev *EventDecl) string {
	names := make([]string, len(ev.Fields))
	for i, f := range ev.Fields {
		names[i] = f.Name
	}
	return strings.Join(names, ", ")
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// nearest returns the closest candidate to name by Levenshtein distance, within
// a small threshold (so a wild typo does not suggest an unrelated action).
func nearest(name string, candidates []string) string {
	best, bestD := "", 1<<30
	for _, c := range candidates {
		d := levenshtein(name, c)
		if d < bestD {
			best, bestD = c, d
		}
	}
	max := len(name)
	if len(best) > max {
		max = len(best)
	}
	if best == "" || bestD > max/2+1 {
		return ""
	}
	return best
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

func checkRole(m *Model, errs *Errors, file string, line int, role string, mustBeAgent bool) {
	r, ok := m.Roles[role]
	if !ok {
		errs.add(EUnknownRole, file, line, "unknown role "+role, "declare the role in the roles: block")
		return
	}
	if mustBeAgent && !r.IsAgent {
		errs.add(ENotAgentRole, file, line, role+" is not an agent role",
			"agent(...) and `agent Role:` actions require a role declared with the `agent` marker")
	}
}

func findField(e *EntityDecl, name string) *FieldDecl {
	for _, f := range e.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}
