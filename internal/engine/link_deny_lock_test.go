package engine

import (
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Regression: a row-level lock `deny update X where status=Signed` must block
// Link/Unlink on the locked record — link/unlink are update-class mutations of
// the record's graph. The gate used to pass a nil record to can(), so the
// conditional deny never matched (whereMatchesForDeny fails closed to
// "no match" on a nil record) — a signed, update-locked record could still be
// linked and unlinked. Contradicted LLD-20's own invariant (permissions.go
// gate-2 comment). Against the pre-fix code this test FAILS at the first Link.
func TestLinkUnlinkRespectRowLevelDenyLock(t *testing.T) {
	src := `
entity Doc:
    title: string required
    status: enum[Draft, Signed] default=Draft

roles:
    Admin
    Locked

permissions:
    Admin:
        full [Doc]
    Locked:
        read [Doc]
        update [Doc]
        deny [update Doc where status = Signed]

link Doc -> Doc as relates / related_by
`
	model, errs := dsl.Compile(map[string]string{"t.dsl": src})
	if len(errs) > 0 {
		t.Fatalf("pack must compile: %v", errs[0])
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	admin := eventstore.Actor{Type: eventstore.ActorHuman, ID: "a", Role: "Admin"}
	locked := eventstore.Actor{Type: eventstore.ActorHuman, ID: "l", Role: "Locked"}

	signed, _ := e.Create(ctx, admin, "Doc", map[string]any{"title": "S", "status": "Signed"}, basis, "")
	draft, _ := e.Create(ctx, admin, "Doc", map[string]any{"title": "D", "status": "Draft"}, basis, "")

	// acting ON the signed (update-locked) record must be denied by deny-where
	if err := e.Link(ctx, locked, "Doc", signed.ID, "relates", draft.ID, basis); err == nil {
		t.Fatal("Link on a Signed (locked) record must be denied by `deny update where status=Signed`")
	}
	// control: acting on the Draft record is allowed (proves the fix does not over-deny)
	if err := e.Link(ctx, locked, "Doc", draft.ID, "relates", signed.ID, basis); err != nil {
		t.Fatalf("Link on a Draft record must be allowed: %v", err)
	}
	// Unlink is the same gate: lock the draft (allowed while still Draft), then
	// unlinking while acting on the now-Signed record must be denied.
	if _, err := e.Update(ctx, admin, "Doc", draft.ID, map[string]any{"status": "Signed"}, basis, ""); err != nil {
		t.Fatalf("admin locking the draft must succeed: %v", err)
	}
	if err := e.Unlink(ctx, locked, "Doc", draft.ID, "relates", signed.ID, basis); err == nil {
		t.Fatal("Unlink on a now-Signed (locked) record must be denied")
	}
}
