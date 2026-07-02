package dsl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// compileErr compiles a single-file pack and returns the first error with the
// given code, or nil.
func errWithCode(errs []*Error, code Code) *Error {
	for _, e := range errs {
		if e.Code == code {
			return e
		}
	}
	return nil
}

func compileOne(src string) []*Error {
	_, errs := Compile(map[string]string{"pack.dsl": src})
	return errs
}

// Test 7 (LLD-01 §7): emitting an event owned by another pack is a compile
// error — a pack owns the facts it emits.
func TestEmitsForeignPackCompileError(t *testing.T) {
	src := `pack a
version 0.1.0
entity Order:
    status: enum[Draft, Sent] default=Draft
workflow Order on status:
    Draft -> Sent: send emits b.SomethingHappened(x = 1)
`
	errs := compileOne(src)
	if errWithCode(errs, EEmitForeignPack) == nil {
		t.Fatalf("emitting b.* from pack a must be E042_EMIT_FOREIGN_PACK, got: %v", errs)
	}
}

// Test 7 (LLD-01 §7): a payload literal whose type does not match the event
// schema is a compile error.
func TestEventPayloadTypeError(t *testing.T) {
	src := `pack a
version 0.1.0
event Priced:
    amount: money
entity Order:
    status: enum[Draft, Sent] default=Draft
workflow Order on status:
    Draft -> Sent: send emits Priced(amount = "not a number")
`
	errs := compileOne(src)
	e := errWithCode(errs, EEventPayload)
	if e == nil {
		t.Fatalf("string literal into a money payload field must be E045_EVENT_PAYLOAD, got: %v", errs)
	}
	if e.FixHint == "" {
		t.Fatal("payload type error must carry a fix hint")
	}
}

// Test 7 companions: unknown event, forbidden payload field type, subscribing
// to an absent pack, duplicate event.
func TestEventMiscCompileErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		code Code
	}{
		{"unknown event on emit", `pack a
version 0.1.0
entity Order:
    status: enum[Draft, Sent] default=Draft
workflow Order on status:
    Draft -> Sent: send emits Ghost(x = 1)
`, EUnknownEvent},
		{"forbidden payload type: file", `pack a
version 0.1.0
event Bad:
    doc: file
`, EEventFieldType},
		{"forbidden payload type: computed", `pack a
version 0.1.0
event Bad:
    total: money computed = 1 + 1
`, EEventFieldType},
		{"subscribe to absent pack", `pack a
version 0.1.0
entity Order:
    status: enum[Draft, Sent] default=Draft
automation:
    on event other.Thing:
        notify email(x)
`, EEventPackAbsent},
		{"duplicate event", `pack a
version 0.1.0
event Dup:
    x: int
event Dup:
    y: int
`, EDupEvent},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := compileOne(c.src)
			if errWithCode(errs, c.code) == nil {
				t.Fatalf("want %s, got: %v", c.code, errs)
			}
		})
	}
}

// Test 14 (LLD-01 §4b): `update ... set <workflow-field>` is a COMPILE error —
// a workflow-governed field can never be written directly (HITL bypass).
func TestUpdateWorkflowFieldCompileError(t *testing.T) {
	src := `pack a
version 0.1.0
entity Order:
    title: string
    status: enum[Draft, Sent] default=Draft
workflow Order on status:
    Draft -> Sent: send
automation:
    on create Order:
        update set status = Sent
`
	errs := compileOne(src)
	e := errWithCode(errs, EUpdateWorkflow)
	if e == nil {
		t.Fatalf("update set status (a workflow field) must be E046_UPDATE_WORKFLOW_FIELD, got: %v", errs)
	}
	if !strings.Contains(strings.ToLower(e.FixHint), "transition") {
		t.Fatalf("fix hint should point at using a transition, got: %q", e.FixHint)
	}
}

// Test 15 (LLD-01 §4c): an act/approve name that is not a transition of any
// entity the role can access is a compile error with a did-you-mean hint.
func TestUnknownActionNameRejected(t *testing.T) {
	src := `pack a
version 0.1.0
entity Order:
    status: enum[Draft, Sent] default=Draft
workflow Order on status:
    Draft -> Sent: send
roles:
    Op
permissions:
    Op:
        full [Order]
        act [snd]
`
	errs := compileOne(src)
	e := errWithCode(errs, EUnknownAction)
	if e == nil {
		t.Fatalf("act [snd] must be E028_UNKNOWN_ACTION, got: %v", errs)
	}
	if !strings.Contains(e.FixHint, "send") {
		t.Fatalf("fix hint should suggest the nearest action `send`, got: %q", e.FixHint)
	}
}

// Test 15 (LLD-01 §4c): scoping — an act name that IS a real transition but of
// an entity the role has no grant on is still rejected.
func TestActionNameScopedToGrantedEntities(t *testing.T) {
	src := `pack a
version 0.1.0
entity Order:
    status: enum[Draft, Sent] default=Draft
entity Ticket:
    state: enum[Open, Closed] default=Open
workflow Order on status:
    Draft -> Sent: send
workflow Ticket on state:
    Open -> Closed: close
roles:
    OrderOp
permissions:
    OrderOp:
        full [Order]
        act [close]
`
	errs := compileOne(src)
	if errWithCode(errs, EUnknownAction) == nil {
		t.Fatalf("act [close] (Ticket transition, no grant on Ticket) must be rejected, got: %v", errs)
	}
}

// Test 15 (LLD-01 §4c): every shipped pack and example must compile cleanly
// under the stricter validation.
func TestAllPacksCompile(t *testing.T) {
	for _, root := range []string{"../../packs", "../../examples"} {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("read %s: %v", root, err)
		}
		for _, ent := range entries {
			if !ent.IsDir() {
				continue
			}
			dir := filepath.Join(root, ent.Name())
			files := map[string]string{}
			dslEntries, _ := os.ReadDir(dir)
			for _, de := range dslEntries {
				if filepath.Ext(de.Name()) != ".dsl" {
					continue
				}
				raw, err := os.ReadFile(filepath.Join(dir, de.Name()))
				if err != nil {
					t.Fatal(err)
				}
				files[de.Name()] = string(raw)
			}
			if len(files) == 0 {
				continue
			}
			t.Run(ent.Name(), func(t *testing.T) {
				if _, errs := Compile(files); len(errs) > 0 {
					t.Fatalf("%s must compile, got %d errors; first: %v", dir, len(errs), errs[0])
				}
			})
		}
	}
}
