package dsl

import "sort"

// Built-in core entities — system reference data every node provides, merged
// into every model so packs can ref them (ref[core.Calendar]) and the runtime
// can CRUD/serve them. core.User stays virtual (identity-backed); the entities
// here are ordinary data with a fixed, kernel-owned schema.

// coreModelEntities returns the built-in entity declarations injected into the
// model. Add reference master-data here (calendars now; currencies, countries…
// later) — that is how core.* gets richer without touching user packs.
func coreModelEntities() []*EntityDecl {
	str := TypeRef{Kind: TyScalar, Scalar: "string"}
	intt := TypeRef{Kind: TyScalar, Scalar: "int"}
	return []*EntityDecl{
		{
			Name:  "core.Calendar",
			Label: "Calendar",
			Fields: []*FieldDecl{
				{Name: "code", Type: str, Required: true, Unique: true, Label: "Code"},
				{Name: "name", Type: str, Required: true, Label: "Name"},
				{Name: "workdays", Type: TypeRef{Kind: TyMultiEnum,
					EnumValues: []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}}, Label: "Working days"},
				{Name: "work_start", Type: intt, Default: "540", Label: "Work start (min from midnight)"},
				{Name: "work_end", Type: intt, Default: "1080", Label: "Work end (min from midnight)"},
				{Name: "holidays", Type: TypeRef{Kind: TyTags}, Label: "Holidays (YYYY-MM-DD)"},
				{Name: "extra_workdays", Type: TypeRef{Kind: TyTags}, Label: "Transferred working days (YYYY-MM-DD)"},
			},
		},
	}
}

// dictionaryEntity builds the standard master-data dictionary schema for an
// mdg[Name] field: a code, a human name, a self-parent for hierarchy, and an
// active flag (soft-retire). Injected as core.<Name> so it reuses the core.*
// permission policy (read by all, written by the node owner), the ref picker and
// the management screen — an MDG module out of the box.
func dictionaryEntity(name string) *EntityDecl {
	str := TypeRef{Kind: TyScalar, Scalar: "string"}
	full := corePrefix + name
	return &EntityDecl{
		Name: full, Label: name,
		Fields: []*FieldDecl{
			{Name: "code", Type: str, Required: true, Unique: true, Label: "Code"},
			{Name: "name", Type: str, Required: true, Label: "Name"},
			{Name: "parent", Type: TypeRef{Kind: TyRef, RefTarget: full}, Label: "Parent"},
			{Name: "active", Type: TypeRef{Kind: TyScalar, Scalar: "bool"}, Default: "true", Label: "Active"},
		},
	}
}

// coreEntityNames is the set of built-in entity names (for ref validation).
func coreEntityNames() map[string]bool {
	m := map[string]bool{"core.User": true}
	for _, e := range coreModelEntities() {
		m[e.Name] = true
	}
	return m
}

// mixinRegistry is the closed set of field mixins available via `include
// core.<Name>` inside an entity body (LLD-22 §1/§3). This is composition, not
// inheritance: the compiler inlines these fields into EntityDecl.Fields on
// the semantic phase, before any other check runs (LLD-22 §2) — permissions,
// computed, unique and workflow all see them as native fields.
//
// v1 is exactly these four seed mixins, fixed by measured repeat count
// across packs (LLD-22 §3: Assignable 14+8, Named 26, Contact 13+3, Period
// 4+4). The list grows only by a second measured consumer (DSL-CORE-LIB §5)
// — do not add to it speculatively.
func mixinRegistry() map[string][]*FieldDecl {
	str := TypeRef{Kind: TyScalar, Scalar: "string"}
	date := TypeRef{Kind: TyScalar, Scalar: "date"}
	userRef := TypeRef{Kind: TyRef, RefTarget: "core.User"}
	return map[string][]*FieldDecl{
		"core.Assignable": {
			{Name: "owner", Type: userRef, Default: "$me", Label: "Owner"},
			{Name: "assignee", Type: userRef, Label: "Assignee"},
		},
		"core.Named": {
			{Name: "name", Type: str, Required: true, Label: "Name"},
		},
		"core.Contact": {
			{Name: "email", Type: str, Label: "Email"},
			{Name: "phone", Type: str, Label: "Phone"},
		},
		"core.Period": {
			{Name: "from", Type: date, Label: "From"},
			{Name: "to", Type: date, Label: "To"},
		},
	}
}

// mixinNames returns the sorted list of registered mixin names, used in the
// "unknown mixin" error's fix hint so an agent sees the available choices.
func mixinNames() []string {
	reg := mixinRegistry()
	names := make([]string, 0, len(reg))
	for name := range reg {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
