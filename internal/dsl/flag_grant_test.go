package dsl

import (
	"strings"
	"testing"
)

// TECHDEBT "Безопасность LLM-периметра" (b): the `flag` permission verb —
// grammar, closed-list validation against the deliberately narrow
// flaggableKinds set (NOT the full eventstore.AllKinds `tail` accepts), with
// a did-you-mean hint, mirroring tail_grant_test.go's discipline for `tail`.

// TestFlagGrantCompiles: a role granted `flag [ingest.flagged]` compiles
// without error — the baseline packs/knowvault's Indexer role relies on.
func TestFlagGrantCompiles(t *testing.T) {
	src := `pack a
version 0.1.0
entity Source:
    status: enum[New, Indexed] default=New

roles:
    Indexer agent

permissions:
    Indexer:
        update [Source]
        flag [ingest.flagged]
        deny [delete *, create *, act *, emit *, tail *, read *]
`
	errs := compileOne(src)
	if len(errs) != 0 {
		t.Fatalf("flag [ingest.flagged] must compile cleanly, got: %v", errs)
	}
}

// TestFlagGrantUnknownKindRejected: a kind outside the closed flaggable set
// (even a perfectly real eventstore.Kind, like record.created, that `tail`
// would happily accept) is a compile error carrying a fix hint — `flag`'s
// target list is its OWN closed set, narrower than the full taxonomy.
func TestFlagGrantUnknownKindRejected(t *testing.T) {
	src := `pack a
version 0.1.0
entity Source:
    status: enum[New, Indexed] default=New

roles:
    Indexer agent

permissions:
    Indexer:
        update [Source]
        flag [record.created]
        deny [delete *, create *, act *, emit *, tail *, read *]
`
	errs := compileOne(src)
	e := errWithCode(errs, EUnknownFlagKind)
	if e == nil {
		t.Fatalf("flag [record.created] must be E058_UNKNOWN_FLAG_KIND, got: %v", errs)
	}
	if e.FixHint == "" {
		t.Fatal("unknown flag kind error must carry a fix hint")
	}
}

// TestFlagGrantDidYouMean: a near-miss kind name gets a did-you-mean hint
// naming the real kind.
func TestFlagGrantDidYouMean(t *testing.T) {
	src := `pack a
version 0.1.0
entity Source:
    status: enum[New, Indexed] default=New

roles:
    Indexer agent

permissions:
    Indexer:
        update [Source]
        flag [ingest.flaged]
        deny [delete *, create *, act *, emit *, tail *, read *]
`
	errs := compileOne(src)
	e := errWithCode(errs, EUnknownFlagKind)
	if e == nil {
		t.Fatalf("flag [ingest.flaged] (typo) must be E058_UNKNOWN_FLAG_KIND, got: %v", errs)
	}
	if want := "ingest.flagged"; !strings.Contains(e.FixHint, want) {
		t.Fatalf("fix hint should suggest the nearest kind %q, got: %q", want, e.FixHint)
	}
}

// TestFlagGrantBadVerbHintMentionsFlag: the unknown-permission-verb error's
// fix hint should list `flag` among the allowed verbs now that it exists.
func TestFlagGrantBadVerbHintMentionsFlag(t *testing.T) {
	src := `pack a
version 0.1.0
entity Source:
    status: enum[New, Indexed] default=New

roles:
    Indexer agent

permissions:
    Indexer:
        flagg [ingest.flagged]
        deny [delete *, create *, update *, act *, emit *, tail *, read *]
`
	errs := compileOne(src)
	e := errWithCode(errs, EBadVerb)
	if e == nil {
		t.Fatalf("unknown verb flagg must be E018_UNKNOWN_PERMISSION_VERB, got: %v", errs)
	}
	if !strings.Contains(e.FixHint, "flag") {
		t.Fatalf("fix hint should list flag among allowed verbs, got: %q", e.FixHint)
	}
}
