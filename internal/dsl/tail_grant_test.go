package dsl

import (
	"strings"
	"testing"
)

// LLD-08 §2 (PR-1): the `tail` permission verb — grammar, closed-list kind
// validation with a did-you-mean hint (same discipline as §4c for act/approve
// and PR-5 for emit), and that a valid grant compiles cleanly.

// TestTailGrantCompiles: a role granted `tail [event.emitted]` compiles
// without error — the baseline the bridge role in packs/site relies on.
func TestTailGrantCompiles(t *testing.T) {
	src := `pack a
version 0.1.0
entity Order:
    status: enum[Draft, Sent] default=Draft

roles:
    Bridge agent

permissions:
    Bridge:
        tail [event.emitted]
        deny [delete *, create *, update *, act *, emit *, read *]
`
	errs := compileOne(src)
	if len(errs) != 0 {
		t.Fatalf("tail [event.emitted] must compile cleanly, got: %v", errs)
	}
}

// TestTailGrantMultipleKinds: a role may tail more than one closed-taxonomy
// kind at once — each comma-separated name is validated independently.
func TestTailGrantMultipleKinds(t *testing.T) {
	src := `pack a
version 0.1.0
entity Order:
    status: enum[Draft, Sent] default=Draft

roles:
    Egress agent

permissions:
    Egress:
        tail [event.emitted, checkpoint.sealed, op.begin]
        deny [delete *, create *, update *, act *, emit *, read *]
`
	errs := compileOne(src)
	if len(errs) != 0 {
		t.Fatalf("tail [event.emitted, checkpoint.sealed, op.begin] must compile cleanly, got: %v", errs)
	}
}

// TestTailGrantUnknownKindRejected: a kind name outside the closed
// eventstore.Kind taxonomy is a compile error carrying a fix hint — the
// permission is a silent "protection that isn't" otherwise, exactly like an
// unknown act/emit name.
func TestTailGrantUnknownKindRejected(t *testing.T) {
	src := `pack a
version 0.1.0
entity Order:
    status: enum[Draft, Sent] default=Draft

roles:
    Bridge agent

permissions:
    Bridge:
        tail [event.exploded]
        deny [delete *, create *, update *, act *, emit *, read *]
`
	errs := compileOne(src)
	e := errWithCode(errs, EUnknownTailKind)
	if e == nil {
		t.Fatalf("tail [event.exploded] must be E049_UNKNOWN_TAIL_KIND, got: %v", errs)
	}
	if e.FixHint == "" {
		t.Fatal("unknown tail kind error must carry a fix hint")
	}
}

// TestTailGrantDidYouMean: a near-miss kind name gets a did-you-mean hint
// naming the real kind.
func TestTailGrantDidYouMean(t *testing.T) {
	src := `pack a
version 0.1.0
entity Order:
    status: enum[Draft, Sent] default=Draft

roles:
    Bridge agent

permissions:
    Bridge:
        tail [event.emited]
        deny [delete *, create *, update *, act *, emit *, read *]
`
	errs := compileOne(src)
	e := errWithCode(errs, EUnknownTailKind)
	if e == nil {
		t.Fatalf("tail [event.emited] (typo) must be E049_UNKNOWN_TAIL_KIND, got: %v", errs)
	}
	if want := "event.emitted"; !strings.Contains(e.FixHint, want) {
		t.Fatalf("fix hint should suggest the nearest kind %q, got: %q", want, e.FixHint)
	}
}

// TestTailGrantRecordKindCompiles: record.created/updated ARE valid kind
// NAMES at compile time (the closed list is the full eventstore taxonomy,
// not the v1-servable subset) — read_events' §3 runtime cap on what it
// actually serves is a separate, engine-level restriction. This documents the
// boundary so a future reader does not "fix" the compiler to also enforce it.
func TestTailGrantRecordKindCompiles(t *testing.T) {
	src := `pack a
version 0.1.0
entity Order:
    status: enum[Draft, Sent] default=Draft

roles:
    Auditor agent

permissions:
    Auditor:
        tail [record.created]
        deny [delete *, create *, update *, act *, emit *, read *]
`
	errs := compileOne(src)
	if len(errs) != 0 {
		t.Fatalf("tail [record.created] is a syntactically valid kind name and must compile, got: %v", errs)
	}
}
