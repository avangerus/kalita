package engine

import (
	"github.com/avangerus/kalita/internal/dsl"
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
	WorkflowField string       `json:"workflow_field,omitempty"`
	Actions       []MetaAction `json:"actions,omitempty"`
	UI            MetaUI       `json:"ui"`
}

type Meta struct {
	Pack       string       `json:"pack"`
	DefVersion uint64       `json:"def_version"`
	ActorID    string       `json:"actor_id"`
	Role       string       `json:"role"`
	Entities   []MetaEntity `json:"entities"`
}

// MetaFor builds the per-actor system description.
func (e *Engine) MetaFor(actorID, role string) *Meta {
	e.mu.RLock()
	defer e.mu.RUnlock()

	m := &Meta{DefVersion: e.defVersion, ActorID: actorID, Role: role}
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
			CanCreate: e.can(role, "create", name, "", nil, actorID).allowed,
			CanRead:   e.can(role, "read", name, "", nil, actorID).allowed,
			CanUpdate: e.can(role, "update", name, "", nil, actorID).allowed,
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
				Readable: e.can(role, "read", name, f.Name, nil, actorID).allowed ||
					e.hasAnyRule(role, "read", name),
				Writable: f.Computed == "" && f.Type.Scalar != "serial" &&
					e.can(role, "update", name, f.Name, nil, actorID).allowed,
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
		m.Entities = append(m.Entities, me)
	}
	return m
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
