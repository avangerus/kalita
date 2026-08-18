package engine

import (
	"fmt"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// TestDiffPlanReportsAuthorityDeltas (Security Pro MAJOR-1): the plan shown
// to the signer must NAME permission-rule changes and approval-gate/guard
// changes — never collapse them into a generic "no structural changes".
func TestDiffPlanReportsAuthorityDeltas(t *testing.T) {
	oldSrc := "pack p\nversion 0.1.0\n\nentity Doc:\n    amount: money\n    status: enum[Draft, Final] default=Draft\n\nroles:\n    Owner\n    Clerk\n\nworkflow Doc on status:\n    Draft -> Final: approve requires approval(Owner)\n    Final -> Draft: reject\n\npermissions:\n    Owner:\n        read [Doc]\n        approve [approve]\n        act [approve]\n    Clerk:\n        read [Doc]\n"
	nextSrc := "pack p\nversion 0.1.0\n\nentity Doc:\n    amount: money\n    status: enum[Draft, Final] default=Draft\n\nroles:\n    Owner\n    Clerk\n\nworkflow Doc on status:\n    Draft -> Final: approve requires approval(Owner) when amount > 100\n    Final -> Draft: reject\n\npermissions:\n    Owner:\n        read [Doc]\n        approve [approve]\n        act [approve, reject]\n    Clerk:\n        read [Doc]\n        update [Doc.amount]\n"
	oldM, errs := dsl.CompileMulti(map[string]string{"p.dsl": oldSrc})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	nextM, errs := dsl.CompileMulti(map[string]string{"p.dsl": nextSrc})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	plan := diffPlan(oldM, nextM)
	joined := ""
	for _, l := range plan {
		joined += l + "\n"
	}
	for _, want := range []string{
		"permissions for role Owner changed: 3 → 3 rules",
		"permissions for role Clerk changed: 1 → 2 rules",
		"guard changed: Doc.approve",
	} {
		found := false
		for _, l := range plan {
			if l == want {
				found = true
			}
		}
		if !found {
			t.Errorf("plan must contain %q, got:\n%s", want, joined)
		}
	}
}

// TestProposePendingCap (Security Pro MAJOR-2): the open-proposal backlog is
// bounded — the 21st parked proposal is refused, decided ones don't count.
func TestProposePendingCap(t *testing.T) {
	boot := map[string]string{
		"one.dsl": "pack one\nversion 0.1.0\n\nentity Alpha:\n    title: string\n",
	}
	model, errs := dsl.CompileMulti(boot)
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	e, err := New(ctx, model, store, WithSources(boot))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		merged := map[string]string{"one.dsl": boot["one.dsl"],
			"extra.dsl": fmt.Sprintf("pack extra\nversion 0.1.0\n\nentity Boom%d:\n    x: int\n", i)}
		if _, _, err := e.ProposeChange(ctx, admin, merged, e.DefVersion(),
			"add", basis, fmt.Sprintf("idem-cap-%d", i)); err != nil {
			t.Fatalf("proposal %d: %v", i, err)
		}
	}
	merged := map[string]string{"one.dsl": boot["one.dsl"],
		"extra.dsl": "pack extra\nversion 0.1.0\n\nentity BoomX:\n    x: int\n"}
	_, _, err = e.ProposeChange(ctx, admin, merged, e.DefVersion(),
		"add", basis, "idem-cap-21")
	if err == nil || !containsCode(err, CodeConflict) {
		t.Fatalf("21st pending proposal must be refused with CONFLICT, got %v", err)
	}
}

func containsCode(err error, code string) bool {
	var e *Err
	for {
		if err == nil {
			return false
		}
		if ee, ok := err.(*Err); ok {
			return ee.Code == code
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
		_ = e
	}
}

// TestProposeSizeCap (Security Pro MAJOR-2 remainder): a single proposal's
// source size is bounded, so an ungated propose cannot journal megabytes.
func TestProposeSizeCap(t *testing.T) {
	boot := map[string]string{
		"one.dsl": "pack one\nversion 0.1.0\n\nentity Alpha:\n    title: string\n",
	}
	model, errs := dsl.CompileMulti(boot)
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	e, err := New(ctx, model, store, WithSources(boot))
	if err != nil {
		t.Fatal(err)
	}
	huge := make([]byte, 5<<20) // 5 MiB
	for i := range huge {
		huge[i] = '#' // a giant comment — cheap to produce, huge to store
	}
	merged := map[string]string{"one.dsl": boot["one.dsl"],
		"bloat.dsl": string(huge)}
	_, _, err = e.ProposeChange(ctx, admin, merged, e.DefVersion(),
		"bloat", basis, "idem-size-1")
	if err == nil || !containsCode(err, CodeValidation) {
		t.Fatalf("oversized proposal must be refused with VALIDATION, got %v", err)
	}
}

// TestDefinitionContentHashStable: the signature content hash is stable under
// key reordering and changes when the content changes (Security Pro MAJOR-1).
func TestDefinitionContentHashStable(t *testing.T) {
	a := map[string]string{"one.dsl": "pack one\n", "two.dsl": "pack two\n"}
	b := map[string]string{"two.dsl": "pack two\n", "one.dsl": "pack one\n"}
	if DefinitionContentHash(a) != DefinitionContentHash(b) {
		t.Fatal("content hash must not depend on map iteration order")
	}
	c := map[string]string{"one.dsl": "pack one\n", "two.dsl": "pack two CHANGED\n"}
	if DefinitionContentHash(a) == DefinitionContentHash(c) {
		t.Fatal("content hash must change with content")
	}
}
