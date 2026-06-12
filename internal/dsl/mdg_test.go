package dsl

import "testing"

// mdg[Name] is a one-line master-data dictionary: the author writes a field and
// the compiler provisions the whole directory (entity, schema, hierarchy) as
// core.<Name>. No hand-built reference entity, no chance to get it O(n^2) wrong.
func TestMDGFieldProvisionsDictionary(t *testing.T) {
	src := `pack hr
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1
entity Employee:
    name: string required
    position: mdg[Position]
    department: mdg[Department]
`
	m, errs := Compile(map[string]string{"hr.dsl": src})
	if len(errs) > 0 {
		t.Fatalf("must compile: %v", errs[0])
	}
	// the Employee field is a ref to the provisioned dictionary
	emp := m.Entities["Employee"]
	var pos *FieldDecl
	for _, f := range emp.Fields {
		if f.Name == "position" {
			pos = f
		}
	}
	if pos == nil || pos.Type.Kind != TyRef || pos.Type.RefTarget != "core.Position" {
		t.Fatalf("position must be ref[core.Position], got %+v", pos)
	}
	// the dictionaries themselves exist with the standard schema
	for _, name := range []string{"core.Position", "core.Department"} {
		d := m.Entities[name]
		if d == nil {
			t.Fatalf("%s dictionary was not provisioned", name)
		}
		got := map[string]bool{}
		for _, f := range d.Fields {
			got[f.Name] = true
		}
		for _, want := range []string{"code", "name", "parent", "active"} {
			if !got[want] {
				t.Errorf("%s missing standard field %q", name, want)
			}
		}
	}
}
