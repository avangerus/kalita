package dsl

import "fmt"

// Semantic analysis of workflow / automation / ui blocks (week 4 gate).

func analyzeBlocks(ast *AST, m *Model, errs *Errors) {
	actions := map[string]bool{} // global action names, for act/approve perms

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
			}
			if tr.AssigneeRole != "" {
				checkRole(m, errs, wf.File, tr.Line, tr.AssigneeRole, tr.AssigneeAgent)
			}
			if tr.ApprovalRole != "" {
				checkRole(m, errs, wf.File, tr.Line, tr.ApprovalRole, false)
			}
		}
	}

	for _, rule := range ast.Automations {
		if rule.Entity != "" {
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
		for _, a := range rule.Actions {
			switch a.Kind {
			case "agent":
				checkRole(m, errs, rule.File, a.Line, a.Role, true)
			case "escalate":
				checkRole(m, errs, rule.File, a.Line, a.Role, false)
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

	// act/approve permission names must exist as workflow actions
	for role, pb := range m.Perms {
		for _, rule := range pb.Rules {
			for _, item := range rule.Items {
				v := item.Verb
				if v != "act" && v != "approve" {
					continue
				}
				for _, name := range item.Names {
					if !actions[name] {
						errs.add(EUnknownAction, "", item.Line,
							fmt.Sprintf("permissions of %s reference unknown action %s", role, name),
							"action names come from workflow transitions")
					}
				}
			}
		}
	}
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
