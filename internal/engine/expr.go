package engine

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

var reDotSpace = regexp.MustCompile(`\s*\.\s*`)

// The unified condition language — one checkable evaluator shared by row-level
// permissions (`where`), query filters, workflow guards and aggregate
// conditions. Closed grammar, NOT arbitrary code, so the guarantees hold:
//
//	expr   := or
//	or     := and ('or' and)*
//	and    := unary ('and' unary)*
//	unary  := 'not' unary | primary
//	primary:= '(' expr ')' | comparison
//	cmp    := operand (('=' | '!=' | '>' | '<' | '>=' | '<=') operand
//	                    | 'in' '[' literal (',' literal)* ']')?
//	operand:= path | literal
//	path   := ident ('.' ident)*        # ref-path: project.owner
//	literal:= number | "string" | bareword | $me | $self | $now | true | false
//
// Fail-closed: any parse/eval error yields false (permission code must never
// fail open).

type evalCtx struct {
	values  map[string]any
	actorID string
	selfID  string
	now     time.Time
	// resolve follows a dotted path (ref hops) from the current record;
	// wired by the engine. nil falls back to flat field lookup.
	resolve func(path string) (any, bool)
}

// evalWhere reports whether expr matches in ctx. Empty expr matches.
func evalWhere(expr string, c evalCtx) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true
	}
	// the DSL tokenizer joins ref paths with spaces ("project . owner");
	// collapse them back so a path is one lexeme
	expr = reDotSpace.ReplaceAllString(expr, ".")
	toks, ok := lexExpr(expr)
	if !ok {
		return false
	}
	p := &exprParser{toks: toks}
	node := p.parseOr()
	if node == nil || p.pos != len(p.toks) {
		return false // trailing garbage fails closed
	}
	v, ok := node.eval(c)
	return ok && v
}

// --- lexer --------------------------------------------------------------------

type exTok struct {
	kind string // word | num | str | punct
	text string
}

func lexExpr(s string) ([]exTok, bool) {
	var toks []exTok
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t':
			i++
		case c == '"':
			j := i + 1
			for j < len(s) && s[j] != '"' {
				j++
			}
			if j >= len(s) {
				return nil, false
			}
			toks = append(toks, exTok{"str", s[i+1 : j]})
			i = j + 1
		case c == '(' || c == ')' || c == '[' || c == ']' || c == ',':
			toks = append(toks, exTok{"punct", string(c)})
			i++
		case c == '=' :
			toks = append(toks, exTok{"punct", "="})
			i++
		case c == '!' && i+1 < len(s) && s[i+1] == '=':
			toks = append(toks, exTok{"punct", "!="})
			i += 2
		case (c == '>' || c == '<') && i+1 < len(s) && s[i+1] == '=':
			toks = append(toks, exTok{"punct", s[i : i+2]})
			i += 2
		case c == '>' || c == '<':
			toks = append(toks, exTok{"punct", string(c)})
			i++
		case c >= '0' && c <= '9' || (c == '-' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9'):
			j := i + 1
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.') {
				j++
			}
			toks = append(toks, exTok{"num", s[i:j]})
			i = j
		case isWordByte(c):
			j := i
			for j < len(s) && (isWordByte(s[j]) || s[j] == '.') {
				j++
			}
			toks = append(toks, exTok{"word", s[i:j]})
			i = j
		default:
			return nil, false
		}
	}
	return toks, true
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '$'
}

// --- parser -------------------------------------------------------------------

type exprNode interface {
	eval(c evalCtx) (bool, bool) // (result, ok)
}

type exprParser struct {
	toks []exTok
	pos  int
}

func (p *exprParser) peek() (exTok, bool) {
	if p.pos < len(p.toks) {
		return p.toks[p.pos], true
	}
	return exTok{}, false
}
func (p *exprParser) isWord(w string) bool {
	t, ok := p.peek()
	return ok && t.kind == "word" && t.text == w
}
func (p *exprParser) isPunct(s string) bool {
	t, ok := p.peek()
	return ok && t.kind == "punct" && t.text == s
}

type boolOp struct {
	op          string
	left, right exprNode
}

func (n *boolOp) eval(c evalCtx) (bool, bool) {
	l, ok := n.left.eval(c)
	if !ok {
		return false, false
	}
	if n.op == "and" && !l {
		return false, true
	}
	if n.op == "or" && l {
		return true, true
	}
	r, ok := n.right.eval(c)
	if !ok {
		return false, false
	}
	return r, true
}

type notNode struct{ inner exprNode }

func (n *notNode) eval(c evalCtx) (bool, bool) {
	v, ok := n.inner.eval(c)
	return !v, ok
}

func (p *exprParser) parseOr() exprNode {
	left := p.parseAnd()
	for left != nil && p.isWord("or") {
		p.pos++
		right := p.parseAnd()
		if right == nil {
			return nil
		}
		left = &boolOp{"or", left, right}
	}
	return left
}

func (p *exprParser) parseAnd() exprNode {
	left := p.parseUnary()
	for left != nil && p.isWord("and") {
		p.pos++
		right := p.parseUnary()
		if right == nil {
			return nil
		}
		left = &boolOp{"and", left, right}
	}
	return left
}

func (p *exprParser) parseUnary() exprNode {
	if p.isWord("not") {
		p.pos++
		inner := p.parseUnary()
		if inner == nil {
			return nil
		}
		return &notNode{inner}
	}
	if p.isPunct("(") {
		p.pos++
		inner := p.parseOr()
		if inner == nil || !p.isPunct(")") {
			return nil
		}
		p.pos++
		return inner
	}
	return p.parseComparison()
}

// operand is a path (word) or a literal.
type operand struct {
	kind string // path | lit
	text string
}

func (p *exprParser) parseOperand() (operand, bool) {
	t, ok := p.peek()
	if !ok {
		return operand{}, false
	}
	switch t.kind {
	case "word":
		p.pos++
		// $me/$self/$now/true/false/null are literals; everything else is a path
		if strings.HasPrefix(t.text, "$") || t.text == "true" || t.text == "false" || t.text == "null" {
			return operand{"lit", t.text}, true
		}
		return operand{"path", t.text}, true
	case "num", "str":
		p.pos++
		return operand{"lit", t.text}, true
	}
	return operand{}, false
}

type cmpNode struct {
	left  operand
	op    string
	right operand
	// in-list
	inList []operand
}

func (p *exprParser) parseComparison() exprNode {
	left, ok := p.parseOperand()
	if !ok {
		return nil
	}
	// `field in [a, b]`
	if p.isWord("in") {
		p.pos++
		if !p.isPunct("[") {
			return nil
		}
		p.pos++
		var list []operand
		for !p.isPunct("]") {
			o, ok := p.parseOperand()
			if !ok {
				return nil
			}
			list = append(list, o)
			if p.isPunct(",") {
				p.pos++
			}
		}
		p.pos++ // consume ]
		return &cmpNode{left: left, op: "in", inList: list}
	}
	t, ok := p.peek()
	if !ok || t.kind != "punct" || !cmpOps[t.text] {
		// a bare path with no operator: truthiness (bool field = true)
		return &cmpNode{left: left, op: "truthy"}
	}
	p.pos++
	right, ok := p.parseOperand()
	if !ok {
		return nil
	}
	return &cmpNode{left: left, op: t.text, right: right}
}

var cmpOps = map[string]bool{"=": true, "!=": true, ">": true, "<": true, ">=": true, "<=": true}

func (n *cmpNode) eval(c evalCtx) (bool, bool) {
	// presence check: `x = null` (x is empty) / `x != null` (x is filled).
	// only meaningful for equality; `null` on either side switches to presence.
	if (n.op == "=" || n.op == "!=") && (isNullLit(n.left) || isNullLit(n.right)) {
		subj := n.left
		if isNullLit(n.left) {
			subj = n.right
		}
		present := operandPresent(subj, c)
		if n.op == "!=" {
			return present, true
		}
		return !present, true
	}
	lv, lok := resolveOperand(n.left, c)
	switch n.op {
	case "truthy":
		b, _ := lv.(bool)
		return b, lok
	case "in":
		if !lok {
			return false, true
		}
		for _, item := range n.inList {
			rv, _ := resolveOperand(item, c)
			if valuesEqual(lv, rv) {
				return true, true
			}
		}
		return false, true
	}
	rv, rok := resolveOperand(n.right, c)
	if !lok || !rok {
		return false, true // missing value → condition false, but not an error
	}
	switch n.op {
	case "=":
		return valuesEqual(lv, rv), true
	case "!=":
		return !valuesEqual(lv, rv), true
	default:
		lf, ok1 := toFloat(lv)
		rf, ok2 := toFloat(rv)
		if !ok1 || !ok2 {
			return false, true
		}
		switch n.op {
		case ">":
			return lf > rf, true
		case "<":
			return lf < rf, true
		case ">=":
			return lf >= rf, true
		case "<=":
			return lf <= rf, true
		}
	}
	return false, false
}

func isNullLit(o operand) bool { return o.kind == "lit" && o.text == "null" }

// operandPresent reports whether the operand resolves to a real (non-null)
// value. An absent field or a field set to nil is "not present" — that is what
// `= null` tests. A bareword compared against null is read as a field name.
func operandPresent(o operand, c evalCtx) bool {
	if o.kind == "lit" {
		return o.text != "null"
	}
	if !strings.Contains(o.text, ".") {
		v, ok := c.values[o.text]
		return ok && v != nil
	}
	if c.resolve != nil {
		v, ok := c.resolve(o.text)
		return ok && v != nil
	}
	return false
}

// resolveOperand returns the value of a path or literal. A bareword that is
// not an existing field is a literal (enum value): `status = Overdue` compares
// the status field to the literal "Overdue".
func resolveOperand(o operand, c evalCtx) (any, bool) {
	if o.kind == "lit" {
		return litValue(o.text, c), true
	}
	if !strings.Contains(o.text, ".") {
		if v, ok := c.values[o.text]; ok {
			return v, true
		}
		return o.text, true // bareword literal (enum value)
	}
	if c.resolve != nil {
		if v, ok := c.resolve(o.text); ok {
			return v, true
		}
	}
	return nil, false
}

func litValue(lit string, c evalCtx) any {
	switch lit {
	case "$me":
		return c.actorID
	case "$self":
		return c.selfID
	case "$now":
		return c.now.Format(time.RFC3339)
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if n, err := strconv.ParseFloat(lit, 64); err == nil {
		return n
	}
	// strip surrounding quotes (raw default values like "bge-m3")
	if len(lit) >= 2 && lit[0] == '"' && lit[len(lit)-1] == '"' {
		return lit[1 : len(lit)-1]
	}
	return lit
}

// evalLiteral resolves a single literal token (used for field default values).
func evalLiteral(lit string, c evalCtx) any {
	return litValue(lit, c)
}

func valuesEqual(a, b any) bool {
	if af, ok := toFloat(a); ok {
		if bf, ok := toFloat(b); ok {
			return af == bf
		}
		return false
	}
	return a == b
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
