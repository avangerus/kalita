package dsl

import (
	"fmt"
	"strings"
)

// Semantic model: the compiler's output, consumed by the runtime engines.

type Model struct {
	Manifest     *Manifest
	Product      *ProductDecl // product{} manifest section (LLD-05 §3), nil if absent
	Entities     map[string]*EntityDecl
	Order        []string // entity declaration order
	Events       map[string]*EventDecl // by short name (own pack)
	Roles        map[string]*RoleDecl
	Perms        map[string]*PermBlock    // by role
	Workflows    map[string]*WorkflowDecl // by entity (one workflow per entity in v0)
	Automations  []*AutomationRule
	UIs          []*UIDecl
	Links        []*LinkDecl
	Dashboards   []*DashboardDecl
	Integrations map[string]*IntegrationDecl // by connector name
}

// PackName is this model's pack namespace (Manifest.Name), used to resolve
// short vs qualified event names (LLD-01 §4.2).
func (m *Model) PackName() string {
	if m.Manifest == nil {
		return ""
	}
	return m.Manifest.Name
}

// EventByRef resolves an event reference — short (`X`, own pack) or qualified
// (`pack.X`). Returns the decl, its canonical `<pack>.<Event>` name, and
// whether the reference names THIS pack (own). A qualified reference to a pack
// other than this one resolves to (nil, name, false): the emitter pack is not
// in this build.
func (m *Model) EventByRef(ref string) (decl *EventDecl, canonical string, own bool) {
	pack, short := m.PackName(), ref
	if dot := strings.IndexByte(ref, '.'); dot >= 0 {
		pack, short = ref[:dot], ref[dot+1:]
	}
	own = pack == m.PackName()
	canonical = pack + "." + short
	if own {
		decl = m.Events[short]
	}
	return decl, canonical, own
}

// HasCapability reports whether "<connector>.<fn>" is a declared integration
// read-function — used to validate picker= and call permissions.
func (m *Model) HasCapability(name string) bool {
	dot := strings.IndexByte(name, '.')
	if dot < 0 {
		return false
	}
	decl, ok := m.Integrations[name[:dot]]
	if !ok {
		return false
	}
	for _, f := range decl.Funcs {
		if f.Name == name[dot+1:] {
			return true
		}
	}
	return false
}

// LinkByName resolves a link name (forward or inverse) for an entity, returning
// the decl and whether the name is the forward direction.
func (m *Model) LinkByName(entity, name string) (*LinkDecl, bool, bool) {
	for _, l := range m.Links {
		if l.From == entity && l.Forward == name {
			return l, true, true
		}
		if l.To == entity && l.Inverse == name {
			return l, false, true
		}
		// self-links (From==To): inverse also applies on the same entity
		if l.From == entity && l.Inverse == name {
			return l, false, true
		}
	}
	return nil, false, false
}

// corePrefix marks references into the built-in core pack (core.User etc).
const corePrefix = "core."

// coreEntities is the closed list of built-in entity names (for ref validation).
var coreEntities = coreEntityNames()

func analyze(ast *AST, errs *Errors) *Model {
	m := &Model{
		Manifest:     ast.Manifest,
		Product:      ast.Product,
		Entities:     map[string]*EntityDecl{},
		Events:       map[string]*EventDecl{},
		Roles:        map[string]*RoleDecl{},
		Perms:        map[string]*PermBlock{},
		Workflows:    map[string]*WorkflowDecl{},
		Automations:  ast.Automations,
		UIs:          ast.UIs,
		Links:        ast.Links,
		Dashboards:   ast.Dashboards,
		Integrations: map[string]*IntegrationDecl{},
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

	// merge built-in core data entities (core.Calendar…) so packs can ref them
	// and the runtime can CRUD/serve them. A user pack may not redeclare them.
	for _, ce := range coreModelEntities() {
		if _, exists := m.Entities[ce.Name]; !exists {
			m.Entities[ce.Name] = ce
			m.Order = append(m.Order, ce.Name)
		}
	}

	// auto-provision a master-data dictionary for every mdg[Name] field (a ref to
	// core.<Name> that isn't a kernel built-in): the author writes one field and
	// gets the whole directory — entity, picker, permissions, management screen.
	dicts := map[string]bool{}
	for _, e := range m.Entities {
		for _, f := range e.Fields {
			if f.Type.Kind == TyRef && strings.HasPrefix(f.Type.RefTarget, corePrefix) && !coreEntities[f.Type.RefTarget] {
				dicts[strings.TrimPrefix(f.Type.RefTarget, corePrefix)] = true
			}
		}
	}
	for short := range dicts {
		full := corePrefix + short
		if _, exists := m.Entities[full]; !exists {
			de := dictionaryEntity(short)
			m.Entities[full] = de
			m.Order = append(m.Order, full)
		}
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

	analyzeEvents(ast, m, errs)
	analyzeBlocks(ast, m, errs)
	analyzeLinks(ast, m, errs)
	analyzeIntegrations(ast, m, errs)
	return m
}

// analyzeEvents registers event declarations and enforces the payload-field
// type subset (LLD-01 §4.1): scalar + ref only, NOT computed/serial/file/
// array[file]. Ref targets are validated like entity ref fields.
func analyzeEvents(ast *AST, m *Model, errs *Errors) {
	for _, ev := range ast.Events {
		if _, dup := m.Events[ev.Name]; dup {
			errs.add(EDupEvent, ev.File, ev.Line, "event "+ev.Name+" is declared twice",
				"merge the declarations or rename one event")
			continue
		}
		seen := map[string]bool{}
		for _, f := range ev.Fields {
			if seen[f.Name] {
				errs.add(EDupField, ev.File, f.Line,
					fmt.Sprintf("event field %s.%s already declared", ev.Name, f.Name),
					"remove or rename the duplicate field")
				continue
			}
			seen[f.Name] = true
			checkEventFieldType(m, ev, f, errs)
		}
		m.Events[ev.Name] = ev
	}
}

// checkEventFieldType rejects the forbidden payload field types.
func checkEventFieldType(m *Model, ev *EventDecl, f *FieldDecl, errs *Errors) {
	bad := func(what string) {
		errs.add(EEventFieldType, ev.File, f.Line,
			fmt.Sprintf("event %s field %s may not be %s", ev.Name, f.Name, what),
			"event payload fields must be a scalar (string/int/money/date/...) or ref[...] — not computed, serial, file or array[file]")
	}
	if f.Computed != "" {
		bad("computed")
		return
	}
	switch f.Type.Kind {
	case TyArrayFile:
		bad("array[file]")
	case TyScalar:
		if f.Type.Scalar == "file" {
			bad("a file")
		} else if f.Type.Scalar == "serial" {
			bad("a serial")
		}
	case TyRef:
		checkRefTarget(m, &EntityDecl{Name: ev.Name, File: ev.File}, f, errs)
	}
}

// integrationResults is the closed set of result shapes a read-function may
// declare. The renderer maps each to a widget (tree = browser, list = picker).
var integrationResults = map[string]bool{"tree": true, "list": true}

func analyzeIntegrations(ast *AST, m *Model, errs *Errors) {
	for _, decl := range ast.Integrations {
		if _, dup := m.Integrations[decl.Name]; dup {
			errs.add(EBadIntegration, decl.File, decl.Line, "integration "+decl.Name+" is declared twice",
				"merge the declarations or rename one integration")
			continue
		}
		seen := map[string]int{}
		for _, fn := range decl.Funcs {
			if prev, dup := seen[fn.Name]; dup {
				errs.add(EBadIntegration, decl.File, fn.Line,
					fmt.Sprintf("read %s already declared at line %d", fn.Name, prev),
					"function names are unique within an integration")
				continue
			}
			seen[fn.Name] = fn.Line
			if !integrationResults[fn.Result] {
				errs.add(EBadIntegration, decl.File, fn.Line,
					"read "+fn.Name+" has unknown result type "+fn.Result,
					"a read function returns `tree` or `list`")
			}
		}
		m.Integrations[decl.Name] = decl
	}

	// picker= must reference a declared capability
	for _, name := range m.Order {
		for _, f := range m.Entities[name].Fields {
			if f.Picker != "" && !m.HasCapability(f.Picker) {
				errs.add(EUnknownCapability, m.Entities[name].File, f.Line,
					fmt.Sprintf("%s.%s picker references unknown capability %s", name, f.Name, f.Picker),
					"declare it as `read <fn>(...)` inside an `integration` block")
			}
		}
	}

	// call permissions must reference a declared capability
	for role, pb := range m.Perms {
		for _, rule := range pb.Rules {
			for _, item := range rule.Items {
				if item.Verb != "call" {
					continue
				}
				for _, cap := range item.Names {
					if !m.HasCapability(cap) {
						errs.add(EUnknownCapability, "", item.Line,
							fmt.Sprintf("permissions of %s reference unknown capability %s", role, cap),
							"capabilities are integration read-functions, e.g. `call [files.list_dirs]`")
					}
				}
			}
		}
	}
}

func analyzeLinks(ast *AST, m *Model, errs *Errors) {
	names := map[string]bool{} // per-entity link names must be unique
	mark := func(entity, name string, file string, line int) {
		key := entity + "." + name
		if names[key] {
			errs.add(EDupLinkName, file, line,
				"link name "+name+" already used on "+entity,
				"forward and inverse names must be unique per entity (and not clash with fields)")
		}
		names[key] = true
	}
	for _, l := range ast.Links {
		if _, ok := m.Entities[l.From]; !ok {
			errs.add(ELinkEntity, l.File, l.Line, "link from unknown entity "+l.From, "declare the entity first")
			continue
		}
		if _, ok := m.Entities[l.To]; !ok {
			errs.add(ELinkEntity, l.File, l.Line, "link to unknown entity "+l.To, "declare the entity first")
			continue
		}
		mark(l.From, l.Forward, l.File, l.Line)
		// symmetric link (same name both ways, e.g. relates_to): mark once
		if !(l.From == l.To && l.Forward == l.Inverse) {
			mark(l.To, l.Inverse, l.File, l.Line)
		}
	}
}

// checkRefTarget validates ref / array[ref] targets.
func checkRefTarget(m *Model, e *EntityDecl, f *FieldDecl, errs *Errors) {
	target := f.Type.RefTarget
	if strings.HasPrefix(target, corePrefix) {
		// a kernel built-in (core.User/core.Calendar) or an auto-provisioned mdg
		// dictionary (injected into the model) is a valid target.
		if !coreEntities[target] && m.Entities[target] == nil {
			errs.add(EUnknownRef, e.File, f.Line, "unknown core entity "+target,
				"available core entities: core.User, core.Calendar; or use mdg[Name] to auto-create a dictionary")
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
