package mcp

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Tool definitions and dispatch. Closed list (MCP-CONTRACT-v0); propose_change
// and get_proposal land with the change pipeline (week 8).

func schema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var str = map[string]any{"type": "string"}
var num = map[string]any{"type": "number"}
var obj = map[string]any{"type": "object"}

var basisSchema = map[string]any{
	"type":        "object",
	"description": "provenance: what this mutation is based on",
	"properties":  map[string]any{"type": map[string]any{"type": "string", "enum": []string{"task", "rule", "adr", "human", "approval"}}, "id": str},
	"required":    []string{"type", "id"},
}

var toolDefs = []map[string]any{
	{"name": "describe_system", "description": "Packs, entities, workflows, roles and your permissions. Call this first.", "inputSchema": schema(map[string]any{})},
	{"name": "describe_entity", "description": "Full schema of one entity: fields, workflow, your access.", "inputSchema": schema(map[string]any{"entity": str}, "entity")},
	{"name": "get_grammar", "description": "The kalita DSL grammar with a canonical example, for generating packs.", "inputSchema": schema(map[string]any{})},
	{"name": "query", "description": "List records within your permissions. 'where' is the full condition language (and/or/not/(), =,!=,>,<,>=,<=, in [..], ref-paths like project.owner, $me/$self/$now); 'sort' is a list of fields (prefix - for descending); 'search' is full-text over text/string fields.", "inputSchema": schema(map[string]any{"entity": str, "filter": obj, "where": str, "sort": map[string]any{"type": "array", "items": str}, "search": str, "limit": num, "offset": num}, "entity")},
	{"name": "get_record", "description": "One record by id (fields you may not read are absent).", "inputSchema": schema(map[string]any{"entity": str, "id": str}, "entity", "id")},
	{"name": "create_record", "description": "Create a record. Requires basis.", "inputSchema": schema(map[string]any{"entity": str, "values": obj, "basis": basisSchema, "idempotency_key": str}, "entity", "values", "basis")},
	{"name": "update_record", "description": "Partially update a record. Requires basis. The workflow state field cannot be written — use act.", "inputSchema": schema(map[string]any{"entity": str, "id": str, "values": obj, "basis": basisSchema, "idempotency_key": str}, "entity", "id", "values", "basis")},
	{"name": "act", "description": "Execute a workflow transition by action name. May return pending_approval — then a human decides, you cannot rush it.", "inputSchema": schema(map[string]any{"entity": str, "id": str, "action": str, "basis": basisSchema, "idempotency_key": str}, "entity", "id", "action", "basis")},
	{"name": "list_my_tasks", "description": "Tasks assigned to your role (open by default).", "inputSchema": schema(map[string]any{"status": str})},
	{"name": "wait_for_task", "description": "Long-poll: blocks until an open task exists for your role or timeout_sec (default 25, max 55) passes. Use this instead of polling list_my_tasks in a loop.", "inputSchema": schema(map[string]any{"timeout_sec": num})},
	{"name": "take_task", "description": "Take an open task: an exclusive lease with TTL. Losing the lease loses the task.", "inputSchema": schema(map[string]any{"task_id": str}, "task_id")},
	{"name": "report_progress", "description": "Attach a progress note. The journal cross-checks it against your actual events on the record.", "inputSchema": schema(map[string]any{"task_id": str, "note": str}, "task_id", "note")},
	{"name": "complete_task", "description": "Finish a taken task with a result.", "inputSchema": schema(map[string]any{"task_id": str, "result": str}, "task_id", "result")},
	{"name": "fail_task", "description": "Honestly fail a task with a reason. Cheaper than silently hanging until the lease expires.", "inputSchema": schema(map[string]any{"task_id": str, "reason": str}, "task_id", "reason")},
	{"name": "comment", "description": "Post a comment on a record — the conversation thread (talk to a human in a task, reply to a customer). internal=true is a staff-only note the external customer cannot see.", "inputSchema": schema(map[string]any{"entity": str, "id": str, "body": str, "internal": map[string]any{"type": "boolean"}, "basis": basisSchema}, "entity", "id", "body", "basis")},
	{"name": "read_comments", "description": "Read the comment thread on a record (only what you may see — customers do not see internal notes).", "inputSchema": schema(map[string]any{"entity": str, "id": str}, "entity", "id")},
	{"name": "read_journal", "description": "Event history of a record you can read.", "inputSchema": schema(map[string]any{"entity": str, "id": str, "limit": num}, "entity", "id")},
	{"name": "compose_pack", "description": "Author a pack from STRUCTURED JSON instead of raw DSL — you do not need the grammar. Pass entities with typed fields, workflows, roles and permission rules; the node renders and validates the DSL. Field types: string,text,int,float,money,bool,date,datetime,file,email,url,phone,duration,percent,color,decimal,json,serial,enum(+values),ref(+ref),array_ref(+ref),tags,multiselect(+values),array_file.", "inputSchema": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pack":    str,
			"version": str,
			"entities": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
				"name":      str,
				"singleton": map[string]any{"type": "boolean"},
				"fields": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
					"name": str, "type": str, "values": map[string]any{"type": "array", "items": str},
					"ref": str, "required": map[string]any{"type": "boolean"}, "unique": map[string]any{"type": "boolean"},
					"default": str, "computed": str, "format": str, "on_delete": str,
				}, "required": []string{"name", "type"}}},
			}, "required": []string{"name", "fields"}}},
			"workflows": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
				"entity": str, "field": str,
				"transitions": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
					"from": str, "to": str, "action": str, "auto": map[string]any{"type": "boolean"},
					"when": str, "assignee_agent": str, "requires_approval": str,
				}}},
			}}},
			"roles": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
				"name": str, "agent": map[string]any{"type": "boolean"}}}},
			"permissions": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
				"role": str, "rules": map[string]any{"type": "array", "items": str}}}},
			"links": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
				"from": str, "to": str, "forward": str, "inverse": str}}},
		},
		"required": []string{"pack", "entities"},
	}},
	{"name": "field_types", "description": "The closed list of field types — discover types without the prose grammar.", "inputSchema": schema(map[string]any{})},
	{"name": "validate_dsl", "description": "Dry-run compile of .dsl sources. Returns structured errors with fix hints; loop until ok.", "inputSchema": schema(map[string]any{"files": obj}, "files")},
	{"name": "propose_change", "description": "Propose new/changed pack sources. Validated, then parked for a human signature; base_def_version must match the live system (describe_system).", "inputSchema": schema(map[string]any{"files": obj, "base_def_version": num, "description": str, "basis": basisSchema}, "files", "base_def_version", "description", "basis")},
	{"name": "get_proposal", "description": "Status of a proposal: pending, applied or rejected with reason.", "inputSchema": schema(map[string]any{"proposal_id": str}, "proposal_id")},
	{"name": "list_dashboards", "description": "Names and titles of the dashboards declared in the loaded packs.", "inputSchema": schema(map[string]any{})},
	{"name": "dashboard", "description": "Compute one dashboard by name: each tile is an aggregate (count/sum/avg/min/max) over a whole table, optionally grouped. Totals respect your row-level permissions.", "inputSchema": schema(map[string]any{"name": str}, "name")},
}

func (s *Server) dispatch(r *http.Request, actor eventstore.Actor, name string, args json.RawMessage) (any, any) {
	ctx := r.Context()
	switch name {
	case "describe_system":
		return s.describeSystem(actor), nil

	case "describe_entity":
		var a struct{ Entity string `json:"entity"` }
		_ = json.Unmarshal(args, &a)
		return s.describeEntity(actor, a.Entity)

	case "get_grammar":
		return map[string]any{"grammar": grammarText, "example": grammarExample}, nil

	case "query":
		var a struct {
			Entity string         `json:"entity"`
			Filter map[string]any `json:"filter"`
			Where  string         `json:"where"`
			Sort   []string       `json:"sort"`
			Search string         `json:"search"`
			Limit  int            `json:"limit"`
			Offset int            `json:"offset"`
		}
		_ = json.Unmarshal(args, &a)
		rows, err := s.eng.Query(ctx, actor, a.Entity, engine.QueryOpts{
			Filter: a.Filter, Where: a.Where, Sort: a.Sort, Search: a.Search, Limit: a.Limit, Offset: a.Offset})
		return map[string]any{"records": rows, "def_version": s.eng.DefVersion()}, toolErr(err)

	case "get_record":
		var a struct{ Entity, ID string }
		_ = json.Unmarshal(args, &a)
		rec, err := s.eng.Get(ctx, actor, a.Entity, a.ID)
		return rec, toolErr(err)

	case "create_record":
		var a struct {
			Entity         string            `json:"entity"`
			Values         map[string]any    `json:"values"`
			Basis          *eventstore.Basis `json:"basis"`
			IdempotencyKey string            `json:"idempotency_key"`
		}
		_ = json.Unmarshal(args, &a)
		rec, err := s.eng.Create(ctx, actor, a.Entity, a.Values, a.Basis, a.IdempotencyKey)
		return rec, toolErr(err)

	case "update_record":
		var a struct {
			Entity         string            `json:"entity"`
			ID             string            `json:"id"`
			Values         map[string]any    `json:"values"`
			Basis          *eventstore.Basis `json:"basis"`
			IdempotencyKey string            `json:"idempotency_key"`
		}
		_ = json.Unmarshal(args, &a)
		rec, err := s.eng.Update(ctx, actor, a.Entity, a.ID, a.Values, a.Basis, a.IdempotencyKey)
		return rec, toolErr(err)

	case "act":
		var a struct {
			Entity         string            `json:"entity"`
			ID             string            `json:"id"`
			Action         string            `json:"action"`
			Basis          *eventstore.Basis `json:"basis"`
			IdempotencyKey string            `json:"idempotency_key"`
		}
		_ = json.Unmarshal(args, &a)
		res, err := s.eng.Act(ctx, actor, a.Entity, a.ID, a.Action, a.Basis, a.IdempotencyKey)
		return res, toolErr(err)

	case "wait_for_task":
		var a struct {
			TimeoutSec int `json:"timeout_sec"`
		}
		_ = json.Unmarshal(args, &a)
		if a.TimeoutSec <= 0 || a.TimeoutSec > 55 {
			a.TimeoutSec = 25
		}
		tasks := s.eng.WaitForTask(ctx, actor.Role, time.Duration(a.TimeoutSec)*time.Second)
		return map[string]any{"tasks": tasks}, nil

	case "list_my_tasks":
		var a struct{ Status string }
		_ = json.Unmarshal(args, &a)
		status := engine.TaskStatus(a.Status)
		if a.Status == "" {
			status = engine.TaskOpen
		}
		return map[string]any{"tasks": s.eng.Tasks(actor.Role, status)}, nil

	case "take_task":
		var a struct{ TaskID string `json:"task_id"` }
		_ = json.Unmarshal(args, &a)
		t, err := s.eng.TakeTask(ctx, actor, a.TaskID)
		return t, toolErr(err)

	case "report_progress":
		var a struct {
			TaskID string `json:"task_id"`
			Note   string `json:"note"`
		}
		_ = json.Unmarshal(args, &a)
		return map[string]any{"ok": true}, toolErr(s.eng.ReportProgress(ctx, actor, a.TaskID, a.Note))

	case "complete_task":
		var a struct {
			TaskID string `json:"task_id"`
			Result string `json:"result"`
		}
		_ = json.Unmarshal(args, &a)
		return map[string]any{"ok": true}, toolErr(s.eng.CompleteTask(ctx, actor, a.TaskID, a.Result))

	case "fail_task":
		var a struct {
			TaskID string `json:"task_id"`
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(args, &a)
		return map[string]any{"ok": true}, toolErr(s.eng.FailTask(ctx, actor, a.TaskID, a.Reason))

	case "comment":
		var a struct {
			Entity   string            `json:"entity"`
			ID       string            `json:"id"`
			Body     string            `json:"body"`
			Internal bool              `json:"internal"`
			Basis    *eventstore.Basis `json:"basis"`
		}
		_ = json.Unmarshal(args, &a)
		c, err := s.eng.Comment(ctx, actor, a.Entity, a.ID, a.Body, a.Internal, a.Basis)
		return c, toolErr(err)

	case "read_comments":
		var a struct {
			Entity string `json:"entity"`
			ID     string `json:"id"`
		}
		_ = json.Unmarshal(args, &a)
		comments, err := s.eng.CommentsOf(actor, a.Entity, a.ID)
		return map[string]any{"comments": comments}, toolErr(err)

	case "read_journal":
		var a struct {
			Entity string `json:"entity"`
			ID     string `json:"id"`
			Limit  int    `json:"limit"`
		}
		_ = json.Unmarshal(args, &a)
		events, err := s.eng.Journal(ctx, actor, a.Entity, a.ID, a.Limit)
		if err != nil {
			return nil, toolErr(err)
		}
		out := make([]map[string]any, 0, len(events))
		for _, ev := range events {
			out = append(out, map[string]any{
				"seq": ev.Seq, "ts": ev.TS.Format(time.RFC3339), "kind": ev.Kind,
				"actor": ev.Actor, "payload": json.RawMessage(ev.Payload), "basis": ev.Basis,
			})
		}
		return map[string]any{"events": out}, nil

	case "compose_pack":
		// structured authoring: the agent passes a JSON pack spec (entities
		// with typed fields, workflows, roles, permissions) and the node
		// renders DSL from it — the grammar lives server-side, the agent never
		// carries grammar prose in context.
		var spec dsl.PackSpec
		if err := json.Unmarshal(args, &spec); err != nil {
			return nil, map[string]any{"code": "VALIDATION_ERROR", "message": "bad pack spec: " + err.Error(),
				"fix_hint": "pass {pack, entities:[{name, fields:[{name, type, ...}]}], workflows, roles, permissions}"}
		}
		rendered := dsl.RenderPack(&spec)
		_, errs := dsl.Compile(map[string]string{spec.Pack + ".dsl": rendered})
		out := map[string]any{"dsl": rendered, "ok": len(errs) == 0}
		if len(errs) > 0 {
			out["errors"] = errs
		}
		return out, nil

	case "field_types":
		return map[string]any{"types": dsl.FieldTypes()}, nil

	case "validate_dsl":
		var a struct{ Files map[string]string `json:"files"` }
		_ = json.Unmarshal(args, &a)
		if len(a.Files) == 0 {
			return nil, map[string]any{"code": "VALIDATION_ERROR", "message": "files is empty",
				"fix_hint": `pass {"files": {"my_pack.dsl": "<dsl source>"}}`}
		}
		_, errs := dsl.Compile(a.Files)
		if len(errs) == 0 {
			return map[string]any{"ok": true}, nil
		}
		return map[string]any{"ok": false, "errors": errs}, nil

	case "propose_change":
		var a struct {
			Files          map[string]string `json:"files"`
			BaseDefVersion uint64            `json:"base_def_version"`
			Description    string            `json:"description"`
			Basis          *eventstore.Basis `json:"basis"`
		}
		_ = json.Unmarshal(args, &a)
		p, dslErrs, err := s.eng.ProposeChange(ctx, actor, a.Files, a.BaseDefVersion, a.Description, a.Basis)
		if err != nil {
			return nil, toolErr(err)
		}
		if len(dslErrs) > 0 {
			return map[string]any{"ok": false, "errors": dslErrs}, nil
		}
		return map[string]any{"ok": true, "proposal_id": p.ID, "status": p.Status,
			"migration_plan": p.Plan,
			"note":           "parked for a human signature; poll get_proposal — you cannot rush it"}, nil

	case "get_proposal":
		var a struct {
			ProposalID string `json:"proposal_id"`
		}
		_ = json.Unmarshal(args, &a)
		p, err := s.eng.GetProposal(a.ProposalID)
		if err != nil {
			return nil, toolErr(err)
		}
		return map[string]any{"proposal_id": p.ID, "status": p.Status, "reason": p.Reason,
			"def_version": s.eng.DefVersion()}, nil

	case "list_dashboards":
		return map[string]any{"dashboards": s.eng.Dashboards()}, nil

	case "dashboard":
		var a struct{ Name string `json:"name"` }
		_ = json.Unmarshal(args, &a)
		res, e := s.eng.Dashboard(ctx, actor, a.Name)
		if e != nil {
			return nil, e
		}
		return res, nil

	default:
		return nil, map[string]any{"code": "VALIDATION_ERROR", "message": "unknown tool " + name,
			"fix_hint": "call tools/list for the available tools"}
	}
}

// toolErr converts an engine error to tool-output form (nil stays nil).
func toolErr(err error) any {
	if err == nil {
		return nil
	}
	var e *engine.Err
	if errors.As(err, &e) {
		return e
	}
	return map[string]any{"code": "INTERNAL", "message": err.Error()}
}

func (s *Server) describeSystem(actor eventstore.Actor) map[string]any {
	m := s.eng.Model()
	entities := []map[string]any{}
	for _, name := range m.Order {
		e := m.Entities[name]
		fields := []string{}
		for _, f := range e.Fields {
			fields = append(fields, f.Name)
		}
		ent := map[string]any{"name": name, "fields": fields}
		if wf, ok := m.Workflows[name]; ok {
			ent["workflow_field"] = wf.Field
		}
		entities = append(entities, ent)
	}
	pack := ""
	if m.Manifest != nil {
		pack = m.Manifest.Name
	}
	return map[string]any{
		"pack": pack, "def_version": s.eng.DefVersion(),
		"entities": entities, "your_role": actor.Role, "your_id": actor.ID,
	}
}

func (s *Server) describeEntity(actor eventstore.Actor, entity string) (any, any) {
	m := s.eng.Model()
	e, ok := m.Entities[entity]
	if !ok {
		return nil, map[string]any{"code": "NOT_FOUND", "message": "unknown entity " + entity,
			"fix_hint": "call describe_system for the list"}
	}
	fields := []map[string]any{}
	for _, f := range e.Fields {
		fd := map[string]any{"name": f.Name, "required": f.Required, "computed": f.Computed != ""}
		switch f.Type.Kind {
		case dsl.TyScalar:
			fd["type"] = f.Type.Scalar
		case dsl.TyEnum:
			fd["type"], fd["values"] = "enum", f.Type.EnumValues
		case dsl.TyRef:
			fd["type"], fd["ref"] = "ref", f.Type.RefTarget
		case dsl.TyArrayRef:
			fd["type"], fd["ref"] = "array[ref]", f.Type.RefTarget
		case dsl.TyTags:
			fd["type"] = "array[string]"
		case dsl.TyMultiEnum:
			fd["type"], fd["values"] = "array[enum]", f.Type.EnumValues
		case dsl.TyArrayFile:
			fd["type"] = "array[file]"
		}
		fields = append(fields, fd)
	}
	out := map[string]any{"name": entity, "fields": fields}
	if wf, ok := m.Workflows[entity]; ok {
		trs := []map[string]any{}
		for _, tr := range wf.Transitions {
			trs = append(trs, map[string]any{"from": tr.From, "to": tr.To, "action": tr.Action,
				"auto": tr.Auto, "when": tr.When, "requires_approval": tr.ApprovalRole != ""})
		}
		out["workflow"] = map[string]any{"field": wf.Field, "transitions": trs}
	}
	return out, nil
}
