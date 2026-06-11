package dsl

import "testing"

// Structured authoring: a JSON pack spec renders to DSL that compiles — agents
// author without carrying grammar prose.
func TestComposePack(t *testing.T) {
	spec := &PackSpec{
		Pack: "mini",
		Entities: []EntitySpec{{
			Name: "Ticket",
			Fields: []FieldSpec{
				{Name: "number", Type: "serial", Format: "T-{seq:4}"},
				{Name: "title", Type: "string", Required: true},
				{Name: "priority", Type: "enum", Values: []string{"Low", "High"}, Default: "Low"},
				{Name: "assignee", Type: "ref", Ref: "core.User"},
				{Name: "status", Type: "enum", Values: []string{"Open", "Done"}, Default: "Open"},
			},
		}},
		Workflows: []WorkflowSpec{{
			Entity: "Ticket", Field: "status",
			Transitions: []TransitionSpec{
				{From: "Open", To: "Done", Action: "close", Approval: "Lead"},
			},
		}},
		Roles: []RoleSpec{{Name: "Lead"}, {Name: "Bot", Agent: true}},
		Perms: []PermSpec{
			{Role: "Lead", Rules: []string{"full [Ticket]", "approve [close]"}},
			{Role: "Bot", Rules: []string{"read [Ticket]", "deny [delete *]"}},
		},
	}
	src := RenderPack(spec)
	_, errs := Compile(map[string]string{"mini.kal": src})
	if len(errs) > 0 {
		t.Fatalf("composed pack must compile, got:\n%s\nerror: %v", src, errs[0])
	}
}
