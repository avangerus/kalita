package dsl

import "strings"

// Parsers for workflow / automation / ui blocks (full grammar v0, week 4).

// --- link ---------------------------------------------------------------------
//
//	link Task -> Task as blocks / blocked_by
//
// Single-line: a named bidirectional relation. The runtime stores the forward
// edge and exposes the inverse automatically.

func (p *parser) link(ln *Line) {
	t := ln.Toks
	// link From -> To as forward / inverse
	if len(t) < 8 || t[2].Text != "->" || t[4].Text != "as" || t[6].Text != "/" ||
		t[1].Kind != TIdent || t[3].Kind != TIdent || t[5].Kind != TIdent || t[7].Kind != TIdent {
		p.errs.add(EBadLink, ln.File, ln.Num,
			"link must be `link From -> To as forward / inverse`",
			"write e.g. `link Task -> Task as blocks / blocked_by`")
		return
	}
	p.ast.Links = append(p.ast.Links, &LinkDecl{
		From: t[1].Text, To: t[3].Text, Forward: t[5].Text, Inverse: t[7].Text,
		File: ln.File, Line: ln.Num,
	})
}

// --- workflow -----------------------------------------------------------------
//
//	workflow Debtor on status:
//	    OnTime  -> Overdue: auto when overdue_days > 0
//	    Overdue -> Claim:   send_claim assignee=agent(Collector)
//	    Claim   -> Legal:   escalate requires approval(FinDirector)
//	    any     -> Settled: auto when debt = 0

func (p *parser) workflow(ln *Line) {
	p.pos++
	t := ln.Toks
	if len(t) < 5 || t[1].Kind != TIdent || t[2].Text != "on" || t[3].Kind != TIdent || t[4].Text != ":" {
		p.errs.add(EExpectedColon, ln.File, ln.Num,
			"workflow declaration must be `workflow Entity on field:`",
			"write e.g. `workflow Debtor on status:`")
		p.skipChildren(ln.Indent)
		return
	}
	wf := &WorkflowDecl{Entity: t[1].Text, Field: t[3].Text, File: ln.File, Line: ln.Num}
	for _, body := range p.children(ln.Indent) {
		if tr := p.transition(&body); tr != nil {
			wf.Transitions = append(wf.Transitions, *tr)
		}
	}
	if len(wf.Transitions) == 0 {
		p.errs.add(EEmptyBlock, ln.File, ln.Num, "workflow has no transitions",
			"add at least one transition, e.g. `Draft -> Done: finish`")
	}
	p.ast.Workflows = append(p.ast.Workflows, wf)
}

func (p *parser) transition(ln *Line) *TransitionDecl {
	t := ln.Toks
	if len(t) < 5 || t[0].Kind != TIdent || t[1].Text != "->" || t[2].Kind != TIdent || t[3].Text != ":" {
		p.errs.add(EBadTransition, ln.File, ln.Num,
			"transition must be `From -> To: action ...`",
			"write e.g. `Overdue -> Claim: send_claim assignee=agent(Collector)`")
		return nil
	}
	tr := &TransitionDecl{From: t[0].Text, To: t[2].Text, Line: ln.Num}
	rest := t[4:]
	if len(rest) == 0 {
		p.errs.add(EBadTransition, ln.File, ln.Num, "transition lacks an action name or `auto`",
			"name the action (`escalate`) or mark the transition `auto when <condition>`")
		return nil
	}
	if rest[0].Text == "auto" {
		tr.Auto = true
		tr.Action = "auto"
	} else {
		tr.Action = rest[0].Text
	}
	i := 1
	for i < len(rest) {
		switch rest[i].Text {
		case "when":
			expr, n := exprUntilStop(rest[i+1:], map[string]bool{"assignee": true, "requires": true, "label": true, "emits": true})
			tr.When = expr
			i += 1 + n
		case "label":
			if i+2 >= len(rest) || rest[i+1].Text != "=" || rest[i+2].Kind != TStr {
				p.errs.add(EBadTransition, ln.File, ln.Num, `label must be label="Button text"`, `write e.g. label="Take in progress"`)
				return tr
			}
			tr.Label = rest[i+2].Text
			i += 3
		case "assignee":
			if i+2 >= len(rest) || rest[i+1].Text != "=" {
				p.errs.add(EBadTransition, ln.File, ln.Num, "assignee requires =", "write `assignee=agent(Role)` or `assignee=Role`")
				return tr
			}
			if rest[i+2].Text == "agent" {
				if i+5 >= len(rest) || rest[i+3].Text != "(" || rest[i+4].Kind != TIdent || rest[i+5].Text != ")" {
					p.errs.add(EBadTransition, ln.File, ln.Num, "agent assignee must be agent(Role)", "write `assignee=agent(Collector)`")
					return tr
				}
				tr.AssigneeAgent = true
				tr.AssigneeRole = rest[i+4].Text
				i += 6
			} else {
				tr.AssigneeRole = rest[i+2].Text
				i += 3
			}
		case "requires":
			if i+4 >= len(rest) || rest[i+1].Text != "approval" || rest[i+2].Text != "(" || rest[i+3].Kind != TIdent {
				p.errs.add(EBadTransition, ln.File, ln.Num, "requires must be `requires approval(Role)`",
					"write e.g. `requires approval(FinDirector)`")
				return tr
			}
			tr.ApprovalRole = rest[i+3].Text
			i += 5
		case "emits":
			name, args, n, ok := p.parseEventCall(rest[i+1:], ln)
			if !ok {
				return tr
			}
			tr.Emits = append(tr.Emits, EmitClause{Event: name, Args: args, Line: ln.Num})
			i += 1 + n
		default:
			p.errs.add(EBadTransition, ln.File, ln.Num, "unexpected "+rest[i].Text+" in transition",
				`allowed clauses: auto, when <expr>, assignee=..., requires approval(Role), label="..."`)
			return tr
		}
	}
	return tr
}

// parseEventCall parses `Name(field=expr, ...)` or `pack.Name(...)` starting at
// toks[0], returning the (possibly qualified) name, the field=expr pairs, and
// the number of tokens consumed. Used by `emits` and the `emit` action.
func (p *parser) parseEventCall(toks []Tok, ln *Line) (string, []Assign, int, bool) {
	if len(toks) == 0 || toks[0].Kind != TIdent {
		p.errs.add(EBadAutomation, ln.File, ln.Num, "expected an event name",
			"write e.g. `emits OrderPlaced(order = $self, qty = qty)`")
		return "", nil, 0, false
	}
	name := toks[0].Text
	i := 1
	if i+1 < len(toks) && toks[i].Text == "." && toks[i+1].Kind == TIdent {
		name += "." + toks[i+1].Text
		i += 2
	}
	if i >= len(toks) || toks[i].Text != "(" {
		p.errs.add(EBadAutomation, ln.File, ln.Num, "event "+name+" must be followed by (args)",
			"write e.g. `"+name+"(field = expr, ...)` — use () for no payload")
		return "", nil, 0, false
	}
	// find the matching close paren
	depth, close := 0, -1
	for j := i; j < len(toks); j++ {
		switch toks[j].Text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				close = j
			}
		}
		if close >= 0 {
			break
		}
	}
	if close < 0 {
		p.errs.add(EBadAutomation, ln.File, ln.Num, "unclosed ( in event "+name, "close the payload with )")
		return "", nil, 0, false
	}
	args := parseAssigns(toks[i+1:close], ln, p.errs)
	return name, args, close + 1, true
}

// parseAssigns splits `a = expr, b = expr` (at bracket depth 0) into Assign
// pairs. Expr keeps its raw joined text (evaluated at runtime).
func parseAssigns(toks []Tok, ln *Line, errs *Errors) []Assign {
	var out []Assign
	for _, group := range splitTopLevel(toks) {
		if len(group) < 3 || group[0].Kind != TIdent || group[1].Text != "=" {
			errs.add(EBadAutomation, ln.File, ln.Num, "payload item must be `field = expr`",
				"write e.g. `total = amount * qty`")
			continue
		}
		var parts []string
		for _, t := range group[2:] {
			if t.Kind == TStr {
				parts = append(parts, `"`+t.Text+`"`)
			} else {
				parts = append(parts, t.Text)
			}
		}
		out = append(out, Assign{Field: group[0].Text, Expr: strings.Join(parts, " "), Line: ln.Num})
	}
	return out
}

// exprUntilStop joins tokens into raw text until one of the stop keywords.
func exprUntilStop(toks []Tok, stop map[string]bool) (string, int) {
	var parts []string
	i := 0
	for i < len(toks) {
		t := toks[i]
		if t.Kind == TIdent && stop[t.Text] {
			break
		}
		if t.Kind == TStr {
			parts = append(parts, `"`+t.Text+`"`)
		} else {
			parts = append(parts, t.Text)
		}
		i++
	}
	return strings.Join(parts, " "), i
}

// --- automation -----------------------------------------------------------------
//
//	automation:
//	    on schedule daily at 09:00 for Debtor when <expr>:
//	        agent Collector: draft_reminder(tone = soft)
//	        notify email(manager)
//	    on update Task when review_cycles > 2:
//	        escalate_to Engineer
//	    on stuck Debtor in Claim for 10d:
//	        escalate_to FinDirector

func (p *parser) automation(ln *Line) {
	p.pos++
	for p.cur() != nil && p.cur().Indent > ln.Indent {
		trigLn := p.cur()
		if trigLn.Toks[0].Text != "on" {
			p.errs.add(EBadAutomation, trigLn.File, trigLn.Num, "automation rule must start with `on`",
				"write `on schedule ...:`, `on update Entity ...:` or `on stuck Entity in State for 2d:`")
			p.pos++
			p.skipChildren(trigLn.Indent)
			continue
		}
		rule := p.automationTrigger(trigLn)
		p.pos++
		for _, actLn := range p.children(trigLn.Indent) {
			if a := p.automationAction(&actLn); a != nil {
				rule.Actions = append(rule.Actions, *a)
			}
		}
		if rule != nil {
			if len(rule.Actions) == 0 {
				p.errs.add(EEmptyBlock, trigLn.File, trigLn.Num, "automation rule has no actions",
					"add an action line, e.g. `escalate_to Owner`")
			}
			p.ast.Automations = append(p.ast.Automations, rule)
		}
	}
}

func (p *parser) automationTrigger(ln *Line) *AutomationRule {
	rule := &AutomationRule{File: ln.File, Line: ln.Num}
	t := ln.Toks
	// strip trailing colon
	if t[len(t)-1].Text == ":" {
		t = t[:len(t)-1]
	} else {
		p.errs.add(EExpectedColon, ln.File, ln.Num, "trigger line must end with :", "add a trailing colon")
	}
	rest := t[1:]
	if len(rest) == 0 {
		p.errs.add(EBadAutomation, ln.File, ln.Num, "empty trigger", "write e.g. `on update Task:`")
		return rule
	}
	switch rest[0].Text {
	case "schedule":
		rule.Trigger = "schedule"
		i := 1
		var sched []string
		for i < len(rest) && rest[i].Text != "for" && rest[i].Text != "when" {
			sched = append(sched, rest[i].Text)
			i++
		}
		rule.Schedule = strings.Join(sched, " ")
		if i < len(rest) && rest[i].Text == "for" {
			if i+1 < len(rest) && rest[i+1].Kind == TIdent {
				rule.Entity = rest[i+1].Text
				i += 2
			} else {
				p.errs.add(EBadAutomation, ln.File, ln.Num, "`for` requires an entity name", "write `for Debtor`")
				return rule
			}
		}
		if i < len(rest) && rest[i].Text == "when" {
			rule.When, _ = exprUntilStop(rest[i+1:], nil)
			if rule.Entity == "" {
				p.errs.add(EBadAutomation, ln.File, ln.Num,
					"schedule rule with a `when` condition must bind an entity",
					"add `for <Entity>` before `when` so the condition has a record to evaluate against")
			}
		}
	case "create", "update", "delete":
		rule.Trigger = rest[0].Text
		if len(rest) < 2 || rest[1].Kind != TIdent {
			p.errs.add(EBadAutomation, ln.File, ln.Num, "on "+rest[0].Text+" requires an entity",
				"write `on "+rest[0].Text+" Task:`")
			return rule
		}
		rule.Entity = rest[1].Text
		if len(rest) > 2 && rest[2].Text == "when" {
			rule.When, _ = exprUntilStop(rest[3:], nil)
		}
	case "stuck":
		rule.Trigger = "stuck"
		// on stuck Entity in State for 10d
		if len(rest) < 6 || rest[1].Kind != TIdent || rest[2].Text != "in" || rest[3].Kind != TIdent || rest[4].Text != "for" {
			p.errs.add(EBadAutomation, ln.File, ln.Num, "stuck trigger must be `on stuck Entity in State for <duration>`",
				"write e.g. `on stuck Debtor in Claim for 10d:`")
			return rule
		}
		rule.Entity, rule.StuckState, rule.StuckFor = rest[1].Text, rest[3].Text, rest[5].Text
	case "event":
		// on event [pack.]Name [when <expr>]
		rule.Trigger = "event"
		if len(rest) < 2 || rest[1].Kind != TIdent {
			p.errs.add(EBadAutomation, ln.File, ln.Num, "on event requires an event name",
				"write e.g. `on event OrderPlaced:` or `on event crm.OpportunityWon:`")
			return rule
		}
		name := rest[1].Text
		i := 2
		if i+1 < len(rest) && rest[i].Text == "." && rest[i+1].Kind == TIdent {
			name += "." + rest[i+1].Text
			i += 2
		}
		rule.Event = name
		if i < len(rest) && rest[i].Text == "when" {
			rule.When, _ = exprUntilStop(rest[i+1:], nil)
		}
	default:
		p.errs.add(EBadAutomation, ln.File, ln.Num, "unknown trigger "+rest[0].Text,
			"triggers: schedule, create, update, delete, stuck, event")
	}
	return rule
}

func (p *parser) automationAction(ln *Line) *AutomationAction {
	t := ln.Toks
	a := &AutomationAction{Raw: ln.Raw, Line: ln.Num}
	switch t[0].Text {
	case "agent":
		// agent Role : task ( args )
		if len(t) < 4 || t[1].Kind != TIdent || t[2].Text != ":" || t[3].Kind != TIdent {
			p.errs.add(EBadAutomation, ln.File, ln.Num, "agent action must be `agent Role: task(args)`",
				"write e.g. `agent Collector: draft_reminder(tone = soft)`")
			return nil
		}
		a.Kind, a.Role, a.Task = "agent", t[1].Text, t[3].Text
		if len(t) > 4 {
			a.Args, _ = exprUntilStop(t[4:], nil)
		}
	case "notify":
		a.Kind = "notify"
		a.Args, _ = exprUntilStop(t[1:], nil)
	case "webhook":
		// webhook out "url"
		if len(t) < 3 || t[1].Text != "out" || t[2].Kind != TStr {
			p.errs.add(EBadAutomation, ln.File, ln.Num, "webhook must be `webhook out \"https://...\"`",
				"only declared outgoing webhooks exist; write the URL as a string")
			return nil
		}
		a.Kind, a.Args = "webhook", t[2].Text
	case "escalate_to":
		if len(t) < 2 || t[1].Kind != TIdent {
			p.errs.add(EBadAutomation, ln.File, ln.Num, "escalate_to requires a role", "write `escalate_to Owner`")
			return nil
		}
		a.Kind, a.Role = "escalate", t[1].Text
	case "emit":
		// emit [pack.]Name(field = expr, ...)  — LLD-01 §4
		name, args, _, ok := p.parseEventCall(t[1:], ln)
		if !ok {
			return nil
		}
		a.Kind, a.Event, a.Assigns = "emit", name, args
	case "create":
		// create Entity(field = expr, ...)  — LLD-01 §4b
		name, args, _, ok := p.parseEventCall(t[1:], ln)
		if !ok {
			return nil
		}
		a.Kind, a.Entity, a.Assigns = "create", name, args
	case "update":
		// update [ref_path] set field = expr, ...  — LLD-01 §4b
		a.Kind = "update"
		setIdx := -1
		for j := 1; j < len(t); j++ {
			if t[j].Text == "set" {
				setIdx = j
				break
			}
		}
		if setIdx == -1 {
			p.errs.add(EBadAutomation, ln.File, ln.Num, "update action must be `update [ref] set field = expr, ...`",
				"write e.g. `update product set in_stock = in_stock - qty`")
			return nil
		}
		// tokens between `update` and `set` are the optional single ref field
		if setIdx == 2 && t[1].Kind == TIdent {
			a.RefPath = t[1].Text
		} else if setIdx != 1 {
			p.errs.add(EBadAutomation, ln.File, ln.Num, "update target must be a single ref field before `set`",
				"write `update set ...` for the trigger record, or `update product set ...` for one ref hop")
			return nil
		}
		a.Assigns = parseAssigns(t[setIdx+1:], ln, p.errs)
		if len(a.Assigns) == 0 {
			p.errs.add(EBadAutomation, ln.File, ln.Num, "update needs at least one `field = expr`",
				"write e.g. `update set probability = 100`")
			return nil
		}
	default:
		p.errs.add(EBadAutomation, ln.File, ln.Num, "unknown action "+t[0].Text,
			"actions: agent, notify, webhook out, escalate_to, emit, create, update")
		return nil
	}
	return a
}

// --- ui ---------------------------------------------------------------------------
//
// The ui block is parsed permissively: structure is free-form within known
// heads, the compiler validates only field references (list/filters/columns/
// section lines) and the board field.

var uiFieldHeads = map[string]bool{"list": true, "filters": true, "section": true, "columns": true}

func (p *parser) ui(ln *Line) {
	p.pos++
	t := ln.Toks
	if len(t) < 3 || t[1].Kind != TIdent || t[2].Text != ":" {
		p.errs.add(EExpectedColon, ln.File, ln.Num, "ui block must be `ui Entity:`", "write e.g. `ui Debtor:`")
		p.skipChildren(ln.Indent)
		return
	}
	decl := &UIDecl{Entity: t[1].Text, File: ln.File, Line: ln.Num}
	for _, body := range p.children(ln.Indent) {
		head := body.Toks[0].Text
		switch {
		case uiFieldHeads[head] || (head == "section" && len(body.Toks) > 1):
			for _, tok := range bracketContents(body.Toks) {
				decl.FieldRefs = append(decl.FieldRefs, PermItem{Entity: decl.Entity, Field: tok, Line: body.Num})
			}
		case head == "board":
			// board: by status
			for j := range body.Toks {
				if body.Toks[j].Text == "by" && j+1 < len(body.Toks) {
					decl.BoardBy = body.Toks[j+1].Text
				}
			}
		}
		// sort=, view, form, actions: free-form in v0
	}
	p.ast.UIs = append(p.ast.UIs, decl)
}

// --- dashboard --------------------------------------------------------------
//
//	dashboard SalesFunnel "Sales funnel":
//	    tile "Open deals":   count Deal where stage != Won and stage != Lost
//	    tile "Pipeline":     sum amount Deal where stage != Lost
//	    tile "By stage":     count Deal group by stage
//	    tile "Avg won deal": avg amount Deal where stage = Won
//
// A dashboard is a set of tiles. Each tile is an aggregate over the WHOLE table
// of an entity (unlike a computed field, which rolls up rows tied to $self),
// optionally filtered by `where` and/or broken down by `group by <field>`.

var aggTileFuncs = map[string]bool{"count": true, "sum": true, "avg": true, "min": true, "max": true}

func (p *parser) dashboard(ln *Line) {
	p.pos++
	t := ln.Toks
	if len(t) < 3 || t[1].Kind != TIdent || t[len(t)-1].Text != ":" {
		p.errs.add(EExpectedColon, ln.File, ln.Num,
			"dashboard block must be `dashboard Name [\"Title\"]:`",
			"write e.g. `dashboard SalesFunnel \"Sales funnel\":`")
		p.skipChildren(ln.Indent)
		return
	}
	d := &DashboardDecl{Name: t[1].Text, File: ln.File, Line: ln.Num}
	if t[2].Kind == TStr {
		d.Title = t[2].Text
	}
	for _, body := range p.children(ln.Indent) {
		if tile := p.dashboardTile(&body); tile != nil {
			d.Tiles = append(d.Tiles, *tile)
		}
	}
	if len(d.Tiles) == 0 {
		p.errs.add(EEmptyBlock, ln.File, ln.Num, "dashboard has no tiles",
			"add at least one tile, e.g. `tile \"Total\": count Deal`")
	}
	p.ast.Dashboards = append(p.ast.Dashboards, d)
}

func (p *parser) dashboardTile(ln *Line) *DashboardTile {
	t := ln.Toks
	if len(t) < 4 || t[0].Text != "tile" || t[1].Kind != TStr || t[2].Text != ":" {
		p.errs.add(EBadDashboardTile, ln.File, ln.Num,
			"tile must be `tile \"Label\": <func> [field] Entity [group by field] [where expr]`",
			"write e.g. `tile \"Pipeline\": sum amount Deal where stage != Lost`")
		return nil
	}
	tile := &DashboardTile{Label: t[1].Text, Line: ln.Num}
	rest := t[3:]
	if len(rest) == 0 || rest[0].Kind != TIdent || !aggTileFuncs[rest[0].Text] {
		p.errs.add(EBadDashboardTile, ln.File, ln.Num,
			"tile must start with an aggregate: count, sum, avg, min, max",
			"write e.g. `count Deal` or `sum amount Deal`")
		return nil
	}
	tile.Func = rest[0].Text
	i := 1
	if tile.Func != "count" {
		if i >= len(rest) || rest[i].Kind != TIdent {
			p.errs.add(EBadDashboardTile, ln.File, ln.Num,
				tile.Func+" needs a field to aggregate", "write e.g. `"+tile.Func+" amount Deal`")
			return nil
		}
		tile.Field = rest[i].Text
		i++
	}
	if i >= len(rest) || rest[i].Kind != TIdent {
		p.errs.add(EBadDashboardTile, ln.File, ln.Num, "tile needs an entity to aggregate over",
			"name the entity after the function, e.g. `count Deal`")
		return nil
	}
	tile.Entity = rest[i].Text
	i++
	for i < len(rest) {
		switch rest[i].Text {
		case "group":
			if i+2 >= len(rest) || rest[i+1].Text != "by" || rest[i+2].Kind != TIdent {
				p.errs.add(EBadDashboardTile, ln.File, ln.Num, "group by needs a field",
					"write e.g. `group by stage`")
				return nil
			}
			tile.GroupBy = rest[i+2].Text
			i += 3
		case "where":
			expr, n := exprUntilStop(rest[i+1:], map[string]bool{"group": true})
			tile.Where = expr
			i += 1 + n
		default:
			p.errs.add(EBadDashboardTile, ln.File, ln.Num, "unexpected "+rest[i].Text+" in tile",
				"after the entity only `group by <field>` and `where <expr>` are allowed")
			return nil
		}
	}
	return tile
}

// --- product --------------------------------------------------------------
//
// The product{} manifest (LLD-05 §3, PK-4) white-labels a pack shipped as a
// standalone product — name/logo/colors/home face/auth mode. Node --brand*
// flags default from these values (cmd/kalita); the frontend reads them via
// /api/meta to theme itself without a build step (ADR-004). `#` starts a
// comment in this DSL (lexer.go), so hex colors must be quoted strings.
//
//	product:
//	    name "KnowVault"
//	    logo "https://cdn.example.com/logo.svg"
//	    color accent "#4da3ff"
//	    home SearchFace
//	    auth password

var productAuthModes = map[string]bool{"password": true, "invite": true, "token": true}

func (p *parser) product(ln *Line) {
	p.pos++
	t := ln.Toks
	if len(t) < 2 || t[len(t)-1].Text != ":" {
		p.errs.add(EExpectedColon, ln.File, ln.Num,
			"product block must be `product:`", "write `product:` with fields indented below")
		p.skipChildren(ln.Indent)
		return
	}
	if p.ast.Product != nil {
		p.errs.add(EBadProduct, ln.File, ln.Num,
			"product block declared twice", "a pack has exactly one product block")
		p.skipChildren(ln.Indent)
		return
	}
	decl := &ProductDecl{Colors: map[string]string{}, File: ln.File, Line: ln.Num}
	for _, body := range p.children(ln.Indent) {
		bt := body.Toks
		switch bt[0].Text {
		case "name":
			if len(bt) >= 2 && bt[1].Kind == TStr {
				decl.Name = bt[1].Text
			} else {
				p.errs.add(EBadProduct, body.File, body.Num,
					"name requires a quoted value", `write e.g. name "KnowVault"`)
			}
		case "logo":
			if len(bt) >= 2 && bt[1].Kind == TStr {
				decl.Logo = bt[1].Text
			} else {
				p.errs.add(EBadProduct, body.File, body.Num,
					"logo requires a quoted value", `write e.g. logo "https://.../logo.svg"`)
			}
		case "color":
			if len(bt) >= 3 && bt[1].Kind == TIdent && bt[2].Kind == TStr {
				decl.Colors[bt[1].Text] = bt[2].Text
			} else {
				p.errs.add(EBadProduct, body.File, body.Num,
					"color requires a name and a quoted value", `write e.g. color accent "#4da3ff"`)
			}
		case "home":
			if len(bt) >= 2 && bt[1].Kind == TIdent {
				decl.Home = bt[1].Text
			} else {
				p.errs.add(EBadProduct, body.File, body.Num,
					"home requires a face name", "write e.g. `home SearchFace`")
			}
		case "auth":
			if len(bt) >= 2 && bt[1].Kind == TIdent && productAuthModes[bt[1].Text] {
				decl.Auth = bt[1].Text
			} else {
				p.errs.add(EBadProduct, body.File, body.Num,
					"auth must be one of password, invite, token", "write e.g. `auth password`")
			}
		default:
			p.errs.add(EBadProduct, body.File, body.Num,
				"unknown product field "+bt[0].Text,
				"allowed: name, logo, color <key> <value>, home <Face>, auth password|invite|token")
		}
	}
	p.ast.Product = decl
}

// bracketContents returns identifiers inside the first [...] of the line.
func bracketContents(toks []Tok) []string {
	var out []string
	depth := 0
	for _, t := range toks {
		switch t.Text {
		case "[":
			depth++
			continue
		case "]":
			return out
		}
		if depth > 0 && t.Kind == TIdent {
			out = append(out, t.Text)
		}
	}
	return out
}
