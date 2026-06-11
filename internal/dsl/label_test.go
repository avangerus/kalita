package dsl

import "testing"

// i18n labels: an entity label and field label= modifier parse and survive into
// the model, while name-only declarations keep an empty label (UI falls back).
func TestLabelsParse(t *testing.T) {
	src := `pack t
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1
entity Incident "Инцидент":
    title: string required label="Тема"
    priority: enum[P1, P2] default=P1 label="Приоритет"
    plain: string
`
	m, errs := Compile(map[string]string{"t.kal": src})
	if len(errs) > 0 {
		t.Fatalf("must compile: %v", errs[0])
	}
	e := m.Entities["Incident"]
	if e.Label != "Инцидент" {
		t.Errorf("entity label = %q, want Инцидент", e.Label)
	}
	want := map[string]string{"title": "Тема", "priority": "Приоритет", "plain": ""}
	for _, f := range e.Fields {
		if f.Label != want[f.Name] {
			t.Errorf("field %s label = %q, want %q", f.Name, f.Label, want[f.Name])
		}
	}
}
