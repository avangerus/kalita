package dsl

import "testing"

// TECHDEBT "multi-line скобки в permissions" / same defect class as
// knowvault BUG-11 (a rule that silently means the opposite of what it
// reads): children() (parser.go) collects subsequent physical lines by
// indentation and hands each one to a per-line parser as an independent
// statement. A bracketed list ([...]) whose closing ] lands on the NEXT
// physical line used to split silently into two rules — the continuation
// re-parses as a fresh grant, which can flip a `deny [...]` into a GRANT of
// the very verbs it meant to forbid, while `kalita check` stayed green.
//
// This is the real shape found authoring packs/datapulse/security.dsl: an
// agent role's deny list wraps mid-bracket, and the continuation
// (`update MasterRecord, create StewardshipTask]`) starts with a valid verb
// keyword, so it silently re-parses as its own grant rule instead of erroring.

// TestWrappedDenyListRejected: the exact hazard shape — a `deny [...]` whose
// `]` is on the next physical line — must fail to compile with
// E061_UNCLOSED_LIST, and must NOT silently produce a phantom grant rule.
func TestWrappedDenyListRejected(t *testing.T) {
	src := `pack a
version 0.1.0
entity Order:
    status: enum[Draft, Sent] default=Draft
entity MasterRecord:
    note: string
entity StewardshipTask:
    title: string

roles:
    Escalator agent

permissions:
    Escalator:
        read Order
        deny [update Order.status,
            update MasterRecord, create StewardshipTask]
`
	model, errs := Compile(map[string]string{"pack.dsl": src})
	e := errWithCode(errs, EUnclosedList)
	if e == nil {
		t.Fatalf("wrapped `deny [...]` must be E061_UNCLOSED_LIST, got: %v", errs)
	}
	if e.FixHint == "" {
		t.Fatal("E061_UNCLOSED_LIST must carry a fix hint")
	}
	// The continuation line must NEVER surface as its own grant: that is
	// exactly the silent deny-flip this error exists to prevent.
	if model != nil {
		if blk := model.Perms["Escalator"]; blk != nil {
			for _, r := range blk.Rules {
				for _, it := range r.Items {
					if it.Verb == "update" && it.Entity == "MasterRecord" {
						t.Fatalf("continuation fragment must not become a live grant, got rule: %+v", r)
					}
					if it.Verb == "create" && it.Entity == "StewardshipTask" {
						t.Fatalf("continuation fragment must not become a live grant, got rule: %+v", r)
					}
				}
			}
		}
	}
}

// TestSingleLineDenyListCompilesClean: the single-line equivalent of the
// wrapped list above compiles with zero errors — the fix rejects only input
// that was already silently broken, it does not touch valid single-line
// lists (I-1, additive-only).
func TestSingleLineDenyListCompilesClean(t *testing.T) {
	src := `pack a
version 0.1.0
entity Order:
    status: enum[Draft, Sent] default=Draft
entity MasterRecord:
    note: string
entity StewardshipTask:
    title: string

roles:
    Escalator agent

permissions:
    Escalator:
        read Order
        deny [update Order.status, update MasterRecord, create StewardshipTask]
`
	errs := compileOne(src)
	if len(errs) != 0 {
		t.Fatalf("single-line deny [...] must compile cleanly, got: %v", errs)
	}
}

// TestWrappedGrantListRejected: the hazard is not deny-specific — any
// bracketed grant list wrapped across physical lines must be rejected the
// same way (e.g. `act [...]`).
func TestWrappedGrantListRejected(t *testing.T) {
	src := `pack a
version 0.1.0
entity Order:
    status: enum[Draft, Sent] default=Draft

roles:
    Bot agent

permissions:
    Bot:
        act [a,
            b]
        deny [delete *, create *, update *, act *, emit *, read *]
`
	errs := compileOne(src)
	e := errWithCode(errs, EUnclosedList)
	if e == nil {
		t.Fatalf("wrapped `act [...]` must be E061_UNCLOSED_LIST, got: %v", errs)
	}
}

// TestUnclosedListAppliesBeyondPermissions: the same wrap hazard is rejected
// in other children()-collected blocks too (widest safe scope) — here an
// entity's enum[...] field type wrapped across lines.
func TestUnclosedListAppliesBeyondPermissions(t *testing.T) {
	src := `entity A:
    s: enum[On,
        Off]
`
	errs := compileOne(src)
	e := errWithCode(errs, EUnclosedList)
	if e == nil {
		t.Fatalf("wrapped enum[...] field must be E061_UNCLOSED_LIST, got: %v", errs)
	}
}

// TestUnclosedListBracketDepthHelper exercises bracketDepth directly for the
// three shapes that matter: balanced, net-open (the line that opened the
// list), net-close (the continuation fragment).
func TestUnclosedListBracketDepthHelper(t *testing.T) {
	cases := []struct {
		name string
		toks []Tok
		want int
	}{
		{"balanced", []Tok{{TIdent, "deny"}, {TPunct, "["}, {TIdent, "a"}, {TPunct, "]"}}, 0},
		{"net-open (opening fragment)", []Tok{{TIdent, "deny"}, {TPunct, "["}, {TIdent, "a"}, {TPunct, ","}}, 1},
		{"net-close (continuation fragment)", []Tok{{TIdent, "update"}, {TIdent, "a"}, {TPunct, "]"}}, -1},
	}
	for _, tc := range cases {
		if got := bracketDepth(tc.toks); got != tc.want {
			t.Errorf("%s: bracketDepth = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// --- E062_UNCLOSED_CALL: the OTHER half of the class (parens, not brackets) ---
//
// TECHDEBT "P2 multi-line скобки в permissions" residual (closed 2026-07-06):
// the same silent-split hazard E061 closed for `[...]` was still open for
// `(...)` — a wrapped `create Entity(...)`, `assignee=agent(...)`, `requires
// approval(...)`, `emits Event(...)` etc. whose closing `)` lands on the NEXT
// physical line. Worse than the bracket case in one respect: automation
// actions are keyed by a leading verb (create/update/agent/notify/...), so a
// continuation fragment that happens to start with `update set ...` silently
// becomes a SECOND, unintended action on the same trigger — not just a
// dropped rule but an extra one.

// TestWrappedCreateActionPhantomUpdateRejected is the real hazard shape from
// TECHDEBT: `create Task(title = "...",\n  update set status = Sent)`. The
// continuation starts with the `update` action keyword, so — absent this fix —
// it would silently re-parse as its own `update set ...` action on the
// trigger record (Order), alongside the intended `create Task`. Must fail to
// compile with E062_UNCLOSED_CALL, and the phantom `update` action must NEVER
// reach the AST.
func TestWrappedCreateActionPhantomUpdateRejected(t *testing.T) {
	src := `pack a
version 0.1.0
entity Order:
    status: enum[Draft, Sent] default=Draft
entity Task:
    title: string
    note: string

roles:
    Bot agent

permissions:
    Bot:
        read Order
        update Order
        create Task

automation:
    on create Order:
        create Task(title = "wrapped",
            update set status = Sent)
`
	model, errs := Compile(map[string]string{"pack.dsl": src})
	e := errWithCode(errs, EUnclosedCall)
	if e == nil {
		t.Fatalf("wrapped `create Task(...)` must be E062_UNCLOSED_CALL, got: %v", errs)
	}
	if e.FixHint == "" {
		t.Fatal("E062_UNCLOSED_CALL must carry a fix hint")
	}
	// The continuation fragment must NEVER surface as its own action: that is
	// exactly the phantom-action hazard this error exists to prevent.
	if model != nil {
		for _, rule := range model.Automations {
			for _, a := range rule.Actions {
				if a.Kind == "update" {
					t.Fatalf("continuation fragment must not become a live action, got: %+v", a)
				}
			}
		}
	}
}

// TestSingleLineCreateActionCompilesClean: the single-line equivalent of a
// wrapped `create Entity(...)` compiles with zero errors — the fix rejects
// only input that was already silently broken, it does not touch valid
// single-line calls (I-1, additive-only).
func TestSingleLineCreateActionCompilesClean(t *testing.T) {
	src := `pack a
version 0.1.0
entity Order:
    status: enum[Draft, Sent] default=Draft
entity Task:
    title: string
    note: string

roles:
    Bot agent

permissions:
    Bot:
        read Order
        create Task
        deny [delete *, update *]

automation:
    on create Order:
        create Task(title = "wrapped", note = "second line")
`
	errs := compileOne(src)
	if len(errs) != 0 {
		t.Fatalf("single-line create Entity(...) must compile cleanly, got: %v", errs)
	}
}

// TestUnclosedCallAppliesBeyondAutomation: the same wrap hazard is rejected in
// other children()-collected blocks too (widest safe scope, mirrors
// TestUnclosedListAppliesBeyondPermissions) — here a workflow transition's
// `requires approval(...)` wrapped across lines.
func TestUnclosedCallAppliesBeyondAutomation(t *testing.T) {
	src := `pack a
version 0.1.0
entity Contract:
    status: enum[Draft, Sent] default=Draft

roles:
    FinDirector

workflow Contract on status:
    Draft -> Sent: submit requires approval(
        FinDirector)
`
	errs := compileOne(src)
	e := errWithCode(errs, EUnclosedCall)
	if e == nil {
		t.Fatalf("wrapped `requires approval(...)` must be E062_UNCLOSED_CALL, got: %v", errs)
	}
}

// TestUnclosedCallParenDepthHelper exercises parenDepth directly for the
// three shapes that matter: balanced, net-open (the line that opened the
// call), net-close (the continuation fragment). Mirrors
// TestUnclosedListBracketDepthHelper for the bracket helper.
func TestUnclosedCallParenDepthHelper(t *testing.T) {
	cases := []struct {
		name string
		toks []Tok
		want int
	}{
		{"balanced", []Tok{{TIdent, "create"}, {TIdent, "Task"}, {TPunct, "("}, {TIdent, "a"}, {TPunct, ")"}}, 0},
		{"net-open (opening fragment)", []Tok{{TIdent, "create"}, {TIdent, "Task"}, {TPunct, "("}, {TIdent, "a"}, {TPunct, ","}}, 1},
		{"net-close (continuation fragment)", []Tok{{TIdent, "update"}, {TIdent, "set"}, {TIdent, "a"}, {TPunct, ")"}}, -1},
	}
	for _, tc := range cases {
		if got := parenDepth(tc.toks); got != tc.want {
			t.Errorf("%s: parenDepth = %d, want %d", tc.name, got, tc.want)
		}
	}
}
