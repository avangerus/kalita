package dsl

import (
	"fmt"
	"strings"
)

// Recursive-descent parser over logical lines. On a malformed line it reports
// and skips, so one mistake does not hide the rest of the file from an agent.

type parser struct {
	lines []Line
	pos   int
	errs  *Errors
	ast   *AST
}

func parse(lines []Line, errs *Errors, ast *AST) {
	p := &parser{lines: lines, errs: errs, ast: ast}
	p.run()
}

func (p *parser) cur() *Line {
	if p.pos >= len(p.lines) {
		return nil
	}
	return &p.lines[p.pos]
}

func (p *parser) run() {
	for p.cur() != nil {
		ln := p.cur()
		if ln.Indent != 0 {
			p.errs.add(EBadIndent, ln.File, ln.Num,
				"unexpected indentation at top level",
				"top-level declarations (entity, roles, permissions, ...) start at column 0")
			p.pos++
			continue
		}
		head := ln.Toks[0].Text
		switch head {
		case "pack", "version", "requires", "depends":
			p.manifestLine(ln)
			p.pos++
		case "entity":
			p.entity(ln)
		case "constraints":
			p.constraints(ln)
		case "roles":
			p.roles(ln)
		case "permissions":
			p.permissions(ln)
		case "workflow":
			p.workflow(ln)
		case "automation":
			p.automation(ln)
		case "ui":
			p.ui(ln)
		default:
			p.errs.add(EUnknownBlock, ln.File, ln.Num,
				"unknown top-level block "+head,
				"allowed: pack, version, requires, depends, entity, constraints, workflow, roles, permissions, automation, ui")
			p.pos++
			p.skipChildren(0)
		}
	}
}

// children returns subsequent lines with indent greater than parentIndent,
// advancing the cursor past them.
func (p *parser) children(parentIndent int) []Line {
	var out []Line
	for p.cur() != nil && p.cur().Indent > parentIndent {
		out = append(out, *p.cur())
		p.pos++
	}
	return out
}

func (p *parser) skipChildren(parentIndent int) { p.children(parentIndent) }

// --- manifest ---------------------------------------------------------------

func (p *parser) manifestLine(ln *Line) {
	if p.ast.Manifest == nil {
		p.ast.Manifest = &Manifest{}
	}
	m := p.ast.Manifest
	rest := strings.TrimSpace(strings.TrimPrefix(ln.Raw, ln.Toks[0].Text))
	if rest == "" {
		p.errs.add(EBadManifest, ln.File, ln.Num, ln.Toks[0].Text+" requires a value",
			fmt.Sprintf("write e.g. `%s collections`", ln.Toks[0].Text))
		return
	}
	switch ln.Toks[0].Text {
	case "pack":
		m.Name = rest
	case "version":
		m.Version = rest
	case "requires":
		m.Requires = rest
	case "depends":
		m.Depends = append(m.Depends, rest)
	}
}

// --- entity ------------------------------------------------------------------

func (p *parser) entity(ln *Line) {
	p.pos++
	if len(ln.Toks) < 3 || ln.Toks[1].Kind != TIdent || ln.Toks[2].Text != ":" {
		p.errs.add(EExpectedColon, ln.File, ln.Num,
			"entity declaration must be `entity Name:`",
			"write `entity Debtor:` with a PascalCase name and a trailing colon")
		p.skipChildren(ln.Indent)
		return
	}
	e := &EntityDecl{Name: ln.Toks[1].Text, File: ln.File, Line: ln.Num}
	body := p.children(ln.Indent)
	if len(body) == 0 {
		p.errs.add(EEmptyBlock, ln.File, ln.Num, "entity "+e.Name+" has no fields",
			"add at least one field, e.g. `name: string required`")
	}
	for i := range body {
		if f := p.fieldLine(&body[i]); f != nil {
			e.Fields = append(e.Fields, f)
		}
	}
	p.ast.Entities = append(p.ast.Entities, e)
}

func (p *parser) fieldLine(ln *Line) *FieldDecl {
	t := ln.Toks
	if len(t) < 2 || t[0].Kind != TIdent || t[1].Text != ":" {
		p.errs.add(EExpectedColon, ln.File, ln.Num,
			"field must be `name: type [modifiers]`",
			"write e.g. `debt: money` or `status: enum[New, Done] default=New`")
		return nil
	}
	f := &FieldDecl{Name: t[0].Text, Line: ln.Num}
	rest := t[2:]
	n, ok := parseType(rest, &f.Type, ln, p.errs)
	if !ok {
		return nil
	}
	p.modifiers(rest[n:], f, ln)
	return f
}

// parseType consumes a type from toks, returns tokens consumed.
func parseType(toks []Tok, ty *TypeRef, ln *Line, errs *Errors) (int, bool) {
	if len(toks) == 0 || toks[0].Kind != TIdent {
		errs.add(EBadTypeSyntax, ln.File, ln.Num, "missing field type",
			"allowed types: string text int float money bool date datetime file enum[..] ref[..] array[ref[..]]")
		return 0, false
	}
	base := toks[0].Text
	switch {
	case scalarTypes[base]:
		ty.Kind, ty.Scalar = TyScalar, base
		return 1, true
	case base == "enum":
		vals, n, ok := bracketIdents(toks[1:], ln, errs, "enum")
		if !ok {
			return 0, false
		}
		ty.Kind, ty.EnumValues = TyEnum, vals
		return 1 + n, true
	case base == "ref":
		target, n, ok := bracketDotted(toks[1:], ln, errs, "ref")
		if !ok {
			return 0, false
		}
		ty.Kind, ty.RefTarget = TyRef, target
		return 1 + n, true
	case base == "array":
		// array[ref[X]]
		if len(toks) < 6 || toks[1].Text != "[" || toks[2].Text != "ref" {
			errs.add(EBadTypeSyntax, ln.File, ln.Num, "array supports only array[ref[Entity]] in v0",
				"write e.g. `members: array[ref[core.User]]`")
			return 0, false
		}
		target, n, ok := bracketDotted(toks[3:], ln, errs, "ref")
		if !ok {
			return 0, false
		}
		idx := 3 + n
		if idx >= len(toks) || toks[idx].Text != "]" {
			errs.add(EBadTypeSyntax, ln.File, ln.Num, "unclosed array[...]", "close with ]")
			return 0, false
		}
		ty.Kind, ty.RefTarget = TyArrayRef, target
		return idx + 1, true
	default:
		errs.add(EUnknownType, ln.File, ln.Num, "unknown type "+base,
			"allowed types: string text int float money bool date datetime file enum[..] ref[..] array[ref[..]]")
		return 0, false
	}
}

// bracketIdents parses [A, B, C] returning the idents and tokens consumed.
func bracketIdents(toks []Tok, ln *Line, errs *Errors, what string) ([]string, int, bool) {
	if len(toks) == 0 || toks[0].Text != "[" {
		errs.add(EBadTypeSyntax, ln.File, ln.Num, what+" requires [...]",
			fmt.Sprintf("write `%s[A, B]`", what))
		return nil, 0, false
	}
	var vals []string
	i := 1
	for i < len(toks) {
		switch {
		case toks[i].Text == "]":
			if len(vals) == 0 {
				errs.add(EBadTypeSyntax, ln.File, ln.Num, what+" list is empty", "list at least one value")
				return nil, 0, false
			}
			return vals, i + 1, true
		case toks[i].Text == ",":
			i++
		case toks[i].Kind == TIdent:
			vals = append(vals, toks[i].Text)
			i++
		default:
			errs.add(EBadTypeSyntax, ln.File, ln.Num, "unexpected "+toks[i].Text+" in "+what+" list",
				"separate identifiers with commas: "+what+"[A, B]")
			return nil, 0, false
		}
	}
	errs.add(EBadTypeSyntax, ln.File, ln.Num, "unclosed "+what+"[...]", "close with ]")
	return nil, 0, false
}

// bracketDotted parses [Name] or [pkg.Name] returning the dotted name.
func bracketDotted(toks []Tok, ln *Line, errs *Errors, what string) (string, int, bool) {
	if len(toks) < 3 || toks[0].Text != "[" || toks[1].Kind != TIdent {
		errs.add(EBadTypeSyntax, ln.File, ln.Num, what+" requires [Entity]",
			fmt.Sprintf("write `%s[Contract]` or `%s[core.User]`", what, what))
		return "", 0, false
	}
	name := toks[1].Text
	i := 2
	for i+1 < len(toks) && toks[i].Text == "." && toks[i+1].Kind == TIdent {
		name += "." + toks[i+1].Text
		i += 2
	}
	if i >= len(toks) || toks[i].Text != "]" {
		errs.add(EBadTypeSyntax, ln.File, ln.Num, "unclosed "+what+"[...]", "close with ]")
		return "", 0, false
	}
	return name, i + 1, true
}

var onDeleteValues = map[string]bool{"restrict": true, "set_null": true, "cascade": true}

func (p *parser) modifiers(toks []Tok, f *FieldDecl, ln *Line) {
	i := 0
	for i < len(toks) {
		t := toks[i]
		switch t.Text {
		case "required":
			f.Required = true
			i++
		case "unique":
			f.Unique = true
			i++
		case "default", "computed", "on_delete":
			if i+2 >= len(toks)+1 || i+1 >= len(toks) || toks[i+1].Text != "=" {
				p.errs.add(EBadModifier, ln.File, ln.Num, t.Text+" requires =value",
					fmt.Sprintf("write `%s=...`", t.Text))
				return
			}
			val, n := exprUntilKeyword(toks[i+2:])
			switch t.Text {
			case "default":
				f.Default = val
			case "computed":
				f.Computed = val
			case "on_delete":
				if !onDeleteValues[val] {
					p.errs.add(EBadModifier, ln.File, ln.Num, "on_delete must be restrict, set_null or cascade",
						"write e.g. `on_delete=restrict`")
				}
				f.OnDelete = val
			}
			i += 2 + n
		default:
			p.errs.add(EBadModifier, ln.File, ln.Num, "unknown modifier "+t.Text,
				"allowed modifiers: required, unique, default=, computed=, on_delete=")
			return
		}
	}
}

var modKeywords = map[string]bool{"required": true, "unique": true, "default": true, "computed": true, "on_delete": true}

// exprUntilKeyword joins tokens into a raw expression until the next modifier
// keyword. Expressions stay raw text in week 2; the expression grammar is
// analyzed with workflow guards in week 4.
func exprUntilKeyword(toks []Tok) (string, int) {
	var parts []string
	i := 0
	depth := 0
	for i < len(toks) {
		t := toks[i]
		if depth == 0 && t.Kind == TIdent && modKeywords[t.Text] {
			break
		}
		if t.Text == "(" || t.Text == "[" {
			depth++
		}
		if t.Text == ")" || t.Text == "]" {
			depth--
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

// --- constraints --------------------------------------------------------------

func (p *parser) constraints(ln *Line) {
	p.pos++
	body := p.children(ln.Indent)
	if len(p.ast.Entities) == 0 {
		p.errs.add(EOrphanBlock, ln.File, ln.Num, "constraints block without a preceding entity",
			"place `constraints:` immediately after the entity it constrains")
		return
	}
	target := p.ast.Entities[len(p.ast.Entities)-1]
	for i := range body {
		b := &body[i]
		t := b.Toks
		if len(t) < 4 || t[0].Text != "unique" || t[1].Text != "(" {
			p.errs.add(EBadModifier, b.File, b.Num, "only unique(...) constraints are supported in v0",
				"write e.g. `unique(company, contract)`")
			continue
		}
		var fields []string
		for _, tok := range t[2:] {
			if tok.Kind == TIdent {
				fields = append(fields, tok.Text)
			}
		}
		target.Constraints = append(target.Constraints, ConstraintDecl{Line: b.Num, Kind: "unique", Fields: fields})
	}
}

// --- roles --------------------------------------------------------------------

func (p *parser) roles(ln *Line) {
	p.pos++
	body := p.children(ln.Indent)
	if len(body) == 0 {
		p.errs.add(EEmptyBlock, ln.File, ln.Num, "roles block is empty", "declare at least one role")
	}
	for i := range body {
		b := &body[i]
		r := &RoleDecl{Name: b.Toks[0].Text, Line: b.Num}
		if len(b.Toks) > 1 && b.Toks[1].Text == "agent" {
			r.IsAgent = true
		}
		p.ast.Roles = append(p.ast.Roles, r)
	}
}

// --- permissions ----------------------------------------------------------------

var permVerbs = map[string]bool{
	"read": true, "create": true, "update": true, "delete": true,
	"act": true, "approve": true, "full": true, "deny": true,
}

func (p *parser) permissions(ln *Line) {
	p.pos++
	for p.cur() != nil && p.cur().Indent > ln.Indent {
		roleLn := p.cur()
		if len(roleLn.Toks) < 2 || roleLn.Toks[len(roleLn.Toks)-1].Text != ":" {
			p.errs.add(EExpectedColon, roleLn.File, roleLn.Num,
				"permission block must be `RoleName:`",
				"write the role name with a trailing colon, then its rules indented")
			p.pos++
			p.skipChildren(roleLn.Indent)
			continue
		}
		block := &PermBlock{Role: roleLn.Toks[0].Text, Line: roleLn.Num}
		p.pos++
		for _, ruleLn := range p.children(roleLn.Indent) {
			if rule := p.permRule(&ruleLn); rule != nil {
				block.Rules = append(block.Rules, *rule)
			}
		}
		p.ast.Permissions = append(p.ast.Permissions, block)
	}
}

func (p *parser) permRule(ln *Line) *PermRule {
	verb := ln.Toks[0].Text
	if !permVerbs[verb] {
		p.errs.add(EBadVerb, ln.File, ln.Num, "unknown permission verb "+verb,
			"allowed verbs: read, create, update, delete, act, approve, full, deny")
		return nil
	}
	rule := &PermRule{Verb: verb, Line: ln.Num}
	rest := ln.Toks[1:]
	if len(rest) > 0 && rest[0].Text == "[" {
		for _, group := range splitTopLevel(rest[1 : len(rest)-trailingBracket(rest)]) {
			if item := parsePermItem(group, verb, ln, p.errs); item != nil {
				rule.Items = append(rule.Items, *item)
			}
		}
	} else if item := parsePermItem(rest, verb, ln, p.errs); item != nil {
		rule.Items = append(rule.Items, *item)
	}
	return rule
}

func trailingBracket(toks []Tok) int {
	if len(toks) > 0 && toks[len(toks)-1].Text == "]" {
		return 1
	}
	return 0
}

// splitTopLevel splits a token list on commas at bracket depth 0.
func splitTopLevel(toks []Tok) [][]Tok {
	var out [][]Tok
	var cur []Tok
	depth := 0
	for _, t := range toks {
		switch t.Text {
		case "[", "(":
			depth++
		case "]", ")":
			depth--
		}
		if t.Text == "," && depth == 0 {
			if len(cur) > 0 {
				out = append(out, cur)
			}
			cur = nil
			continue
		}
		cur = append(cur, t)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// parsePermItem parses one permission target:
//
//	Debtor | Debtor.debt | Debtor.* | * | all | Entity where <expr>
//	(inside deny) update Debtor.debt | delete * | act [a, b] | read X where ...
func parsePermItem(toks []Tok, outerVerb string, ln *Line, errs *Errors) *PermItem {
	if len(toks) == 0 {
		return nil
	}
	item := &PermItem{Verb: outerVerb, Line: ln.Num}
	i := 0
	if outerVerb == "deny" && permVerbs[toks[0].Text] {
		item.Verb = toks[0].Text
		i = 1
	}
	if item.Verb == "act" || item.Verb == "approve" {
		for _, t := range toks[i:] {
			if t.Kind == TIdent {
				item.Names = append(item.Names, t.Text)
			}
		}
		return item
	}
	if i >= len(toks) {
		errs.add(EBadVerb, ln.File, ln.Num, "deny item lacks a target",
			"write e.g. `deny [update Debtor.debt]`")
		return nil
	}
	switch {
	case toks[i].Text == "*" || toks[i].Text == "all":
		item.All = true
		i++
	case toks[i].Kind == TIdent:
		item.Entity = toks[i].Text
		i++
		if i+1 < len(toks)+1 && i < len(toks) && toks[i].Text == "." {
			i++
			if i < len(toks) && (toks[i].Kind == TIdent || toks[i].Text == "*") {
				item.Field = toks[i].Text
				i++
			} else {
				errs.add(EBadVerb, ln.File, ln.Num, "expected field name after .",
					"write Entity.field or Entity.*")
				return nil
			}
		}
	default:
		errs.add(EBadVerb, ln.File, ln.Num, "unexpected "+toks[i].Text+" in permission target",
			"target must be an Entity, Entity.field, * or all")
		return nil
	}
	if i < len(toks) && toks[i].Text == "where" {
		expr, _ := exprUntilKeyword(toks[i+1:])
		item.Where = expr
	}
	return item
}

