package engine

import (
	"sort"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Meta describes the system AS SEEN BY one actor: which entities and fields
// are visible, which buttons exist. The UI is a pure projection of this —
// it contains no permission logic of its own (week-7 mechanics rule #2).

type MetaField struct {
	Name     string   `json:"name"`
	Label    string   `json:"label,omitempty"`
	Type     string   `json:"type"`
	Values   []string `json:"values,omitempty"`
	Ref      string   `json:"ref,omitempty"`
	Required bool     `json:"required"`
	Computed bool     `json:"computed"`
	Readable bool     `json:"readable"`
	Writable bool     `json:"writable"`
}

type MetaAction struct {
	Action           string `json:"action"`
	Label            string `json:"label,omitempty"`
	From             string `json:"from"`
	To               string `json:"to"`
	RequiresApproval bool   `json:"requires_approval"`
	CanAct           bool   `json:"can_act"`
}

type MetaView struct {
	Name  string `json:"name"`
	Where string `json:"where"`
}

type MetaUI struct {
	ListColumns []string `json:"list_columns,omitempty"`
	Filters     []string `json:"filters,omitempty"`
	BoardBy     string   `json:"board_by,omitempty"`
}

type MetaEntity struct {
	Name          string       `json:"name"`
	Label         string       `json:"label,omitempty"`
	Singleton     bool         `json:"singleton,omitempty"`
	Fields        []MetaField  `json:"fields"`
	CanCreate     bool         `json:"can_create"`
	CanRead       bool         `json:"can_read"`
	CanUpdate     bool         `json:"can_update"`
	CanRevert     bool         `json:"can_revert"` // full on the entity → may undo/rewind (supervisory)
	WorkflowField string       `json:"workflow_field,omitempty"`
	Actions       []MetaAction `json:"actions,omitempty"`
	Links         []MetaLink   `json:"links,omitempty"`
	Children      []MetaChild  `json:"children,omitempty"`
	UI            MetaUI       `json:"ui"`
}

type MetaRole struct {
	Name    string `json:"name"`
	IsAgent bool   `json:"is_agent"`
}

// MetaLink is a named relation an entity can form from its side: the name to
// use and the entity on the other end (so the UI can offer a picker).
type MetaLink struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

// MetaChild is a master-detail relation: another entity that points at this one
// through a ref field, so the record view can show its detail rows (order lines,
// payments, findings…).
type MetaChild struct {
	Entity string `json:"entity"`
	Field  string `json:"field"`
	Label  string `json:"label,omitempty"`
}

type Meta struct {
	Pack       string       `json:"pack"`
	DefVersion uint64       `json:"def_version"`
	ActorID    string       `json:"actor_id"`
	Role       string       `json:"role"`
	Entities   []MetaEntity `json:"entities"`
	Roles      []MetaRole   `json:"roles"` // for inviting a user into a role (select, don't type)
}

// MetaFor builds the per-actor system description.
func (e *Engine) MetaFor(actorID, role string) *Meta {
	e.mu.RLock()
	defer e.mu.RUnlock()

	m := &Meta{DefVersion: e.defVersion, ActorID: actorID, Role: role}
	// meta checks are schema-level (record == nil), so row-level where-clauses do
	// not evaluate; an actor without attrs is fine here.
	actor := eventstore.Actor{ID: actorID, Role: role}
	if e.model.Manifest != nil {
		m.Pack = e.model.Manifest.Name
	}

	uiByEntity := map[string]*dsl.UIDecl{}
	for _, u := range e.model.UIs {
		uiByEntity[u.Entity] = u
	}

	for _, name := range e.model.Order {
		decl := e.model.Entities[name]
		me := MetaEntity{
			Name:      name,
			Label:     decl.Label,
			Singleton: decl.Singleton,
			CanCreate: e.can(actor, "create", name, "", nil).allowed,
			CanRead:   e.can(actor, "read", name, "", nil).allowed,
			CanUpdate: e.can(actor, "update", name, "", nil).allowed,
			CanRevert: e.hasFull(role, name),
		}
		// an entity readable only row-level (where) still must appear: probe
		// with a permissive check — if any read rule exists at all
		if !me.CanRead && e.hasAnyRule(role, "read", name) {
			me.CanRead = true
		}
		if !me.CanRead {
			continue // invisible entities do not exist for this actor
		}
		for _, f := range decl.Fields {
			mf := MetaField{
				Name: f.Name, Label: f.Label, Required: f.Required, Computed: f.Computed != "",
				Readable: e.can(actor, "read", name, f.Name, nil).allowed ||
					e.hasAnyRule(role, "read", name),
				Writable: f.Computed == "" && f.Type.Scalar != "serial" &&
					e.can(actor, "update", name, f.Name, nil).allowed,
			}
			switch f.Type.Kind {
			case dsl.TyScalar:
				mf.Type = f.Type.Scalar
			case dsl.TyEnum:
				mf.Type, mf.Values = "enum", f.Type.EnumValues
			case dsl.TyRef:
				mf.Type, mf.Ref = "ref", f.Type.RefTarget
			case dsl.TyArrayRef:
				mf.Type, mf.Ref = "array_ref", f.Type.RefTarget
			case dsl.TyTags:
				mf.Type = "tags"
			case dsl.TyMultiEnum:
				mf.Type, mf.Values = "multiselect", f.Type.EnumValues
			case dsl.TyArrayFile:
				mf.Type = "array_file"
			}
			// the workflow field is never directly writable
			if wf, ok := e.model.Workflows[name]; ok && wf.Field == f.Name {
				mf.Writable = false
			}
			// serial numbers are kernel-assigned, never user-editable
			if f.Type.Kind == dsl.TyScalar && f.Type.Scalar == "serial" {
				mf.Writable = false
			}
			me.Fields = append(me.Fields, mf)
		}
		if wf, ok := e.model.Workflows[name]; ok {
			me.WorkflowField = wf.Field
			for _, tr := range wf.Transitions {
				if tr.Auto {
					continue
				}
				me.Actions = append(me.Actions, MetaAction{
					Action: tr.Action, Label: tr.Label, From: tr.From, To: tr.To,
					RequiresApproval: tr.ApprovalRole != "",
					CanAct:           e.canNamed(role, "act", tr.Action),
				})
			}
		}
		if u, ok := uiByEntity[name]; ok {
			seen := map[string]bool{}
			for _, ref := range u.FieldRefs {
				if !seen[ref.Field] {
					me.UI.ListColumns = append(me.UI.ListColumns, ref.Field)
					seen[ref.Field] = true
				}
			}
			me.UI.BoardBy = u.BoardBy
		}
		// named relations this entity can form, each with the entity on the other
		// end (so the UI offers a relation picker + a target lookup)
		for _, l := range e.model.Links {
			if l.From == name {
				me.Links = append(me.Links, MetaLink{Name: l.Forward, Target: l.To})
			}
			if l.To == name && l.Inverse != l.Forward {
				me.Links = append(me.Links, MetaLink{Name: l.Inverse, Target: l.From})
			}
		}
		// master-detail: other entities that point at this one via a ref field —
		// the record view shows their rows (order lines, payments, findings).
		for _, other := range e.model.Order {
			od := e.model.Entities[other]
			for _, f := range od.Fields {
				if f.Type.Kind == dsl.TyRef && f.Type.RefTarget == name &&
					(e.can(actor, "read", other, "", nil).allowed || e.hasAnyRule(role, "read", other)) {
					me.Children = append(me.Children, MetaChild{Entity: other, Field: f.Name, Label: od.Label})
				}
			}
		}
		m.Entities = append(m.Entities, me)
	}
	for _, name := range sortedRoleNames(e.model.Roles) {
		m.Roles = append(m.Roles, MetaRole{Name: name, IsAgent: e.model.Roles[name].IsAgent})
	}
	return m
}

func sortedRoleNames(roles map[string]*dsl.RoleDecl) []string {
	out := make([]string, 0, len(roles))
	for n := range roles {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// hasAnyRule reports whether the role has any allow rule for verb on entity
// (incl. row-conditional ones — the UI shows the entity, rows filter later).
func (e *Engine) hasAnyRule(role, verb, entity string) bool {
	pb, ok := e.model.Perms[role]
	if !ok {
		return false
	}
	for _, rule := range pb.Rules {
		v := rule.Verb
		if v != verb && v != "full" {
			continue
		}
		for _, item := range rule.Items {
			if item.All || item.Entity == entity {
				return true
			}
		}
	}
	return false
}
