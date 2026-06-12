package dsl

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
			},
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
