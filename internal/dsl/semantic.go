package dsl

import (
	"fmt"
	"strings"
)

// Semantic model: the compiler's output, consumed by the runtime engines.

type Model struct {
	Manifest *Manifest
	Entities map[string]*EntityDecl
	Order    []string // entity declaration order
	Roles    map[string]*RoleDecl
	Perms    map[string]*PermBlock // by role
	Raw      []*RawBlock           // workflow/automation/ui, analyzed week 4
}

// corePrefix marks references into the built-in core pack (core.User etc).
const corePrefix = "core."

// coreEntities is the closed list of built-ins available in v0.
var coreEntities = map[string]bool{"core.User": true}

func analyze(ast *AST, errs *Errors) *Model {
	m := &Model{
		Manifest: ast.Manifest,
		Entities: map[string]*EntityDecl{},
		Roles:    map[string]*RoleDecl{},
		Perms:    map[string]*PermBlock{},
		Raw:      ast.RawBlocks,
	}

	// entities, duplicate detection
	for _, e := range ast.Entities {
		if _, dup := m.Entities[e.Name]; dup {
			errs.add(EDupEntity, e.File, e.Line, "entity "+e.Name+" is declared twice",
				"merge the declarations or rename one entity")
			continue
		}
		m.Entities[e.Name] = e
		m.Order = append(m.Order, e.Name)
	}

	// fields: duplicates, ref targets, enum defaults, constraint fields
	for _, name := range m.Order {
		e := m.Entities[name]
		seen := map[string]int{}
		for _, f := range e.Fields {
			if prev, dup := seen[f.Name]; dup {
				errs.add(EDupField, e.File, f.Line,
					fmt.Sprintf("field %s.%s already declared at line %d", e.Name, f.Name, prev),
					"remove or rename the duplicate field")
				continue
			}
			seen[f.Name] = f.Line
			switch f.Type.Kind {
			case TyRef, TyArrayRef:
				checkRefTarget(m, e, f, errs)
			case TyEnum:
				if f.Default != "" && !contains(f.Type.EnumValues, f.Default) {
					errs.add(EBadEnumDefault, e.File, f.Line,
						fmt.Sprintf("default %s is not among enum values [%s]", f.Default, strings.Join(f.Type.EnumValues, ", ")),
						"use one of the declared enum values as default")
				}
			}
		}
		for _, c := range e.Constraints {
			for _, cf := range c.Fields {
				if _, ok := seen[cf]; !ok {
					errs.add(EConstraint, e.File, c.Line,
						fmt.Sprintf("constraint references unknown field %s.%s", e.Name, cf),
						"unique(...) may only list fields declared in the entity above")
				}
			}
		}
	}

	// roles
	for _, r := range ast.Roles {
		if _, dup := m.Roles[r.Name]; dup {
			errs.add(EDupRole, "", r.Line, "role "+r.Name+" is declared twice", "remove the duplicate role")
			continue
		}
		m.Roles[r.Name] = r
	}

	// permissions: blocks for the same role merge (a pack may extend a role's
	// rules across files)
	for _, pb := range ast.Permissions {
		if _, ok := m.Roles[pb.Role]; !ok {
			errs.add(EUnknownRole, "", pb.Line, "permissions for undeclared role "+pb.Role,
				"declare the role in the roles: block first")
			continue
		}
		for _, rule := range pb.Rules {
			for _, item := range rule.Items {
				checkPermTarget(m, pb.Role, item, errs)
			}
		}
		if existing, ok := m.Perms[pb.Role]; ok {
			existing.Rules = append(existing.Rules, pb.Rules...)
		} else {
			cp := *pb
			m.Perms[pb.Role] = &cp
		}
	}

	// The defining constraint of the grammar: an agent role without explicit
	// deny boundaries does not compile (DSL-SPEC-v0 §5) — checked over the
	// merged rule set.
	for name, pb := range m.Perms {
		role := m.Roles[name]
		if role == nil || !role.IsAgent {
			continue
		}
		hasDeny := false
		for _, rule := range pb.Rules {
			if rule.Verb == "deny" {
				hasDeny = true
			}
		}
		if !hasDeny {
			errs.add(EAgentNoDeny, "", pb.Line,
				"agent role "+name+" has no deny block",
				"agent roles must declare explicit boundaries, e.g. `deny [delete *, update "+firstEntity(m)+".*]`")
		}
	}

	// agent roles that have no permission block at all are equally unbounded
	for name, r := range m.Roles {
		if r.IsAgent {
			if _, ok := m.Perms[name]; !ok {
				errs.add(EAgentNoDeny, "", r.Line,
					"agent role "+name+" has no permissions block (and therefore no deny boundaries)",
					"add a permissions block for "+name+" with explicit deny rules")
			}
		}
	}

	return m
}

// checkRefTarget validates ref / array[ref] targets.
func checkRefTarget(m *Model, e *EntityDecl, f *FieldDecl, errs *Errors) {
	target := f.Type.RefTarget
	if strings.HasPrefix(target, corePrefix) {
		if !coreEntities[target] {
			errs.add(EUnknownRef, e.File, f.Line, "unknown core entity "+target,
				"available core entities: core.User")
		}
		return
	}
	if _, ok := m.Entities[target]; !ok {
		errs.add(EUnknownRef, e.File, f.Line,
			fmt.Sprintf("%s.%s references undeclared entity %s", e.Name, f.Name, target),
			"declare `entity "+target+":` in this pack or reference core.User")
	}
}

func checkPermTarget(m *Model, role string, item PermItem, errs *Errors) {
	if item.All || item.Entity == "" {
		return // `all`, `*`, act/approve name lists — checked against workflow in week 4
	}
	e, ok := m.Entities[item.Entity]
	if !ok {
		errs.add(EUnknownEntity, "", item.Line,
			fmt.Sprintf("permissions of %s reference unknown entity %s", role, item.Entity),
			"reference an entity declared in this pack")
		return
	}
	if item.Field != "" && item.Field != "*" {
		for _, f := range e.Fields {
			if f.Name == item.Field {
				return
			}
		}
		errs.add(EUnknownField, "", item.Line,
			fmt.Sprintf("permissions of %s reference unknown field %s.%s", role, item.Entity, item.Field),
			"reference a field declared in the entity, or use "+item.Entity+".*")
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func firstEntity(m *Model) string {
	if len(m.Order) > 0 {
		return m.Order[0]
	}
	return "Entity"
}
