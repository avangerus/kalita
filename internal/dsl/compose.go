package dsl

import (
	"fmt"
	"sort"
	"strings"
)

// Structured authoring: agents describe a pack as JSON intent and the node
// renders DSL from it, so the grammar lives server-side and an agent never
// carries grammar text in context — the tool's JSON schema IS the contract.
// The rendered DSL is the same text a human would write, then compiled and
// validated normally (one source of truth: the grammar/compiler).

// PackSpec is the structured form of a pack.
type PackSpec struct {
	Pack     string         `json:"pack"`
	Version  string         `json:"version"`
	Entities []EntitySpec   `json:"entities"`
	Workflows []WorkflowSpec `json:"workflows"`
	Roles    []RoleSpec     `json:"roles"`
	Perms    []PermSpec     `json:"permissions"`
	Links    []LinkSpec     `json:"links"`
}

type EntitySpec struct {
	Name      string      `json:"name"`
	Singleton bool        `json:"singleton,omitempty"`
	Fields    []FieldSpec `json:"fields"`
}

type FieldSpec struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`              // string, int, enum, ref, serial, ...
	Values   []string `json:"values,omitempty"`  // enum/multiselect options
	Ref      string   `json:"ref,omitempty"`     // ref/array_ref target entity
	Required bool     `json:"required,omitempty"`
	Unique   bool     `json:"unique,omitempty"`
	Default  string   `json:"default,omitempty"`
	Computed string   `json:"computed,omitempty"`
	Format   string   `json:"format,omitempty"`   // serial format
	OnDelete string   `json:"on_delete,omitempty"`
}

type WorkflowSpec struct {
	Entity      string           `json:"entity"`
	Field       string           `json:"field"`
	Transitions []TransitionSpec `json:"transitions"`
}

type TransitionSpec struct {
	From          string `json:"from"`
	To            string `json:"to"`
	Action        string `json:"action,omitempty"`
	Auto          bool   `json:"auto,omitempty"`
	When          string `json:"when,omitempty"`
	AssigneeAgent string `json:"assignee_agent,omitempty"`
	Approval      string `json:"requires_approval,omitempty"`
}

type RoleSpec struct {
	Name  string `json:"name"`
	Agent bool   `json:"agent,omitempty"`
}

// PermSpec is one role's permissions as raw rule lines (kept simple: agents
// write rules like "read Issue where reporter = $me", "deny [delete *]").
type PermSpec struct {
	Role  string   `json:"role"`
	Rules []string `json:"rules"`
}

type LinkSpec struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Forward string `json:"forward"`
	Inverse string `json:"inverse"`
}

// RenderPack turns a PackSpec into DSL source text.
func RenderPack(s *PackSpec) string {
	var b strings.Builder
	ver := s.Version
	if ver == "" {
		ver = "0.1.0"
	}
	fmt.Fprintf(&b, "pack %s\nversion %s\nrequires kalita >= 0.1\ndepends core >= 0.1\n", s.Pack, ver)

	for _, e := range s.Entities {
		b.WriteString("\n")
		if e.Singleton {
			fmt.Fprintf(&b, "entity %s singleton:\n", e.Name)
		} else {
			fmt.Fprintf(&b, "entity %s:\n", e.Name)
		}
		for _, f := range e.Fields {
			b.WriteString("    " + renderField(f) + "\n")
		}
	}

	for _, l := range s.Links {
		fmt.Fprintf(&b, "\nlink %s -> %s as %s / %s\n", l.From, l.To, l.Forward, l.Inverse)
	}

	for _, w := range s.Workflows {
		fmt.Fprintf(&b, "\nworkflow %s on %s:\n", w.Entity, w.Field)
		for _, t := range w.Transitions {
			b.WriteString("    " + renderTransition(t) + "\n")
		}
	}

	if len(s.Roles) > 0 {
		b.WriteString("\nroles:\n")
		for _, r := range s.Roles {
			if r.Agent {
				fmt.Fprintf(&b, "    %s agent\n", r.Name)
			} else {
				fmt.Fprintf(&b, "    %s\n", r.Name)
			}
		}
	}

	if len(s.Perms) > 0 {
		b.WriteString("\npermissions:\n")
		for _, p := range s.Perms {
			fmt.Fprintf(&b, "    %s:\n", p.Role)
			for _, rule := range p.Rules {
				fmt.Fprintf(&b, "        %s\n", rule)
			}
		}
	}

	return b.String()
}

func renderField(f FieldSpec) string {
	var t string
	switch f.Type {
	case "enum":
		t = "enum[" + strings.Join(f.Values, ", ") + "]"
	case "multiselect":
		t = "array[enum[" + strings.Join(f.Values, ", ") + "]]"
	case "tags":
		t = "array[string]"
	case "array_file":
		t = "array[file]"
	case "ref":
		t = "ref[" + f.Ref + "]"
	case "array_ref":
		t = "array[ref[" + f.Ref + "]]"
	default:
		t = f.Type
	}
	parts := []string{f.Name + ": " + t}
	if f.Required {
		parts = append(parts, "required")
	}
	if f.Unique {
		parts = append(parts, "unique")
	}
	if f.Default != "" {
		parts = append(parts, "default="+f.Default)
	}
	if f.Computed != "" {
		parts = append(parts, "computed = "+f.Computed)
	}
	if f.Format != "" {
		parts = append(parts, `format="`+f.Format+`"`)
	}
	if f.OnDelete != "" {
		parts = append(parts, "on_delete="+f.OnDelete)
	}
	return strings.Join(parts, " ")
}

func renderTransition(t TransitionSpec) string {
	action := t.Action
	if t.Auto {
		action = "auto"
	}
	s := fmt.Sprintf("%s -> %s: %s", t.From, t.To, action)
	if t.When != "" {
		s += " when " + t.When
	}
	if t.AssigneeAgent != "" {
		s += " assignee=agent(" + t.AssigneeAgent + ")"
	}
	if t.Approval != "" {
		s += " requires approval(" + t.Approval + ")"
	}
	return s
}

// FieldTypes is the closed list of field types, for the structured tool's
// enum and for agents to discover without the prose grammar.
func FieldTypes() []string {
	out := make([]string, 0, len(scalarTypes)+5)
	for t := range scalarTypes {
		out = append(out, t)
	}
	out = append(out, "enum", "ref", "array_ref", "tags", "multiselect", "array_file")
	sort.Strings(out)
	return out
}
