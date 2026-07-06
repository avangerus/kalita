package dsl

import (
	"strings"
	"testing"
)

// LLD-22 §1/§2/§3 (PR-1): `include core.<Mixin>` field-mixin composition —
// registry, parser position rule and the loud-collision semantics. Model
// content (field order, engine create/read) and permission visibility are
// covered by include_engine_test.go (package dsl_test, needs internal/engine).

// TestIncludeCollisionLoud: a mixin field colliding with an own field, or two
// mixins bringing in the same name, is a compile error naming the field and
// the mixin(s) involved — never a silent merge (LLD-22 §2).
func TestIncludeCollisionLoud(t *testing.T) {
	t.Run("own field collides with mixin field", func(t *testing.T) {
		src := `pack a
version 0.1.0
entity Ticket:
    include core.Assignable
    owner: string
    title: string required
`
		errs := compileOne(src)
		e := errWithCode(errs, EMixinFieldCollision)
		if e == nil {
			t.Fatalf("own field owner + include core.Assignable must be E054_MIXIN_FIELD_COLLISION, got: %v", errs)
		}
		if e.FixHint == "" {
			t.Fatal("collision error must carry a fix hint")
		}
		if !strings.Contains(e.Message, "owner") || !strings.Contains(e.Message, "core.Assignable") {
			t.Fatalf("collision message must name the field and the mixin, got: %q", e.Message)
		}
	})

	t.Run("two mixins collide with each other", func(t *testing.T) {
		// core.Contact brings `email`+`phone`; declare a second, fictitious
		// overlap by including core.Contact twice — same name from two
		// includes must be rejected exactly like a cross-mixin collision.
		src := `pack a
version 0.1.0
entity Ticket:
    include core.Contact
    include core.Contact
    title: string required
`
		errs := compileOne(src)
		e := errWithCode(errs, EMixinFieldCollision)
		if e == nil {
			t.Fatalf("two includes bringing the same field name must be E054_MIXIN_FIELD_COLLISION, got: %v", errs)
		}
		if !strings.Contains(e.Message, "core.Contact") {
			t.Fatalf("collision message must name the colliding includes, got: %q", e.Message)
		}
	})
}

// TestIncludeUnknownMixin: `include core.Nosuch` is a compile error listing
// the available mixins (LLD-22 §2).
func TestIncludeUnknownMixin(t *testing.T) {
	src := `pack a
version 0.1.0
entity Ticket:
    include core.Nosuch
    title: string required
`
	errs := compileOne(src)
	e := errWithCode(errs, EUnknownMixin)
	if e == nil {
		t.Fatalf("include core.Nosuch must be E053_UNKNOWN_MIXIN, got: %v", errs)
	}
	for _, want := range []string{"core.Assignable", "core.Named", "core.Contact", "core.Period"} {
		if !strings.Contains(e.FixHint, want) {
			t.Errorf("fix hint must list available mixins, missing %s: %q", want, e.FixHint)
		}
	}
}

// TestIncludePositionEnforced: `include` after the first field is a compile
// error — the construct is only readable at the top of the entity body
// (LLD-22 §1).
func TestIncludePositionEnforced(t *testing.T) {
	src := `pack a
version 0.1.0
entity Ticket:
    title: string required
    include core.Named
`
	errs := compileOne(src)
	e := errWithCode(errs, EIncludePosition)
	if e == nil {
		t.Fatalf("include after the first field must be E055_INCLUDE_POSITION, got: %v", errs)
	}
	if e.FixHint == "" {
		t.Fatal("include-position error must carry a fix hint")
	}
}

// TestIncludeBadSyntax: a malformed include line (missing mixin name) is
// rejected with a fix hint rather than silently ignored or crashing the
// parser.
func TestIncludeBadSyntax(t *testing.T) {
	src := `pack a
version 0.1.0
entity Ticket:
    include
    title: string required
`
	errs := compileOne(src)
	e := errWithCode(errs, EBadInclude)
	if e == nil {
		t.Fatalf("bare `include` must be E056_BAD_INCLUDE, got: %v", errs)
	}
}
