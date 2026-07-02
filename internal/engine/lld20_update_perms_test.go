package engine

import (
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// LLD-20 acceptance tests (docs/lld/LLD-20-field-scoped-update-perms.md §5).
//
// Gate (1) in engine.Update (records.go) used to be a whole-record check
// (`can(actor,"update",entity,"",rec)`), which only a whole-entity/`*` item
// can satisfy — a role holding ONLY field-scoped `update [X.f]` grants could
// never pass it, so a true allow-list ("write ONLY these fields") was
// unreachable: gate (1) always denied before gate (2) (checkFieldWrites, the
// real per-field allow+deny check) ever ran. LLD-20 replaces gate (1) with
// an existence check (canUpdateEntity, permissions.go): does the role hold
// ANY update rule on the entity, any field scope, not blocked by an
// unconditional whole-entity/`*` deny? Gate (2) is untouched and remains the
// sole source of per-field truth.

const lld20Pack = `
entity Doc:
    title: string required
    secret: string
    amount: money
    owner: ref[core.User] default=$me
    status: enum[Draft, Signed] default=Draft

roles:
    Admin
    Editor
    ScopedOnly
    NoUpdate
    LockedUnscoped
    LockedScoped

permissions:
    Admin:
        full [Doc]
    Editor:
        read [Doc]
        update [Doc]
        deny [update Doc.amount, read Doc.secret]
    # true allow-list: no unscoped 'update [Doc]' grant anywhere for this role
    ScopedOnly:
        read [Doc]
        update [Doc.title, Doc.owner]
    NoUpdate:
        read [Doc]
    LockedUnscoped:
        read [Doc]
        update [Doc]
        deny [update Doc where status = Signed]
    LockedScoped:
        read [Doc]
        update [Doc.title]
        deny [update Doc where status = Signed]
`

func newLLD20Engine(t *testing.T) *Engine {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"lld20.dsl": lld20Pack})
	if len(errs) > 0 {
		t.Fatalf("test pack must compile: %v", errs[0])
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// §5.1 — a role with ONLY `update [X.a, X.b]` (no unscoped grant) writes a
// and b, a write to an ungranted field c is denied, and reading is untouched.
func TestScopedUpdateWritesOnlyGrantedFields(t *testing.T) {
	e := newLLD20Engine(t)
	scoped := eventstore.Actor{Type: eventstore.ActorAgent, ID: "legal-1", Role: "ScopedOnly"}

	doc, err := e.Create(ctx, admin, "Doc", map[string]any{"title": "orig", "secret": "s", "owner": "alice"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}

	// granted fields: title, owner
	if _, err := e.Update(ctx, scoped, "Doc", doc.ID, map[string]any{"title": "new"}, basis, ""); err != nil {
		t.Fatalf("ScopedOnly must write granted field title: %v", err)
	}
	if _, err := e.Update(ctx, scoped, "Doc", doc.ID, map[string]any{"owner": "bob"}, basis, ""); err != nil {
		t.Fatalf("ScopedOnly must write granted field owner: %v", err)
	}

	// ungranted field: secret — no allow rule at all for it, default-deny
	_, err = e.Update(ctx, scoped, "Doc", doc.ID, map[string]any{"secret": "leak"}, basis, "")
	pe := wantCode(t, err, CodePermissionDenied)
	if pe.Rule == "" {
		t.Fatal("PERMISSION_DENIED for the ungranted field must name a rule")
	}

	// reading is not affected by the write-side fix
	got, err := e.Get(ctx, scoped, "Doc", doc.ID)
	if err != nil {
		t.Fatalf("ScopedOnly must still read Doc: %v", err)
	}
	if got.Values["title"] != "new" {
		t.Fatalf("title must reflect the allowed write, got %v", got.Values["title"])
	}
	if got.Values["secret"] != "s" {
		t.Fatalf("secret must be unchanged by the denied write, got %v", got.Values["secret"])
	}
}

// §5.2 — an unscoped `update [X]` role keeps writing everything except an
// explicit per-field deny; gate (1) still exists-checks true for it and gate
// (2) is untouched, so this is a pure regression guard.
func TestUnscopedUpdateUnchanged(t *testing.T) {
	e := newLLD20Engine(t)
	ed := eventstore.Actor{Type: eventstore.ActorHuman, ID: "eve", Role: "Editor"}

	doc, err := e.Create(ctx, admin, "Doc", map[string]any{"title": "a", "amount": 10.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Update(ctx, ed, "Doc", doc.ID, map[string]any{"title": "b"}, basis, ""); err != nil {
		t.Fatalf("Editor may update title: %v", err)
	}
	_, err = e.Update(ctx, ed, "Doc", doc.ID, map[string]any{"amount": 99.0}, basis, "")
	wantCode(t, err, CodePermissionDenied)
}

// §5.3 — a record-level `deny update X where status=Signed` locks the
// record for both a scoped-only role and an unscoped role: gate (2) matches
// the deny's Field="" against every touched field regardless of the actor's
// own grant scope, so gate (1)'s relaxation does not reopen this lock.
func TestRecordDenyStillLocks(t *testing.T) {
	e := newLLD20Engine(t)
	unscoped := eventstore.Actor{Type: eventstore.ActorAgent, ID: "u-1", Role: "LockedUnscoped"}
	scoped := eventstore.Actor{Type: eventstore.ActorAgent, ID: "s-1", Role: "LockedScoped"}

	draft, err := e.Create(ctx, admin, "Doc", map[string]any{"title": "d", "status": "Draft"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	signed, err := e.Create(ctx, admin, "Doc", map[string]any{"title": "s", "status": "Signed"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}

	// both roles may write title on the Draft record
	if _, err := e.Update(ctx, unscoped, "Doc", draft.ID, map[string]any{"title": "d2"}, basis, ""); err != nil {
		t.Fatalf("LockedUnscoped must write title on a Draft record: %v", err)
	}
	if _, err := e.Update(ctx, scoped, "Doc", draft.ID, map[string]any{"title": "d3"}, basis, ""); err != nil {
		t.Fatalf("LockedScoped must write its granted field title on a Draft record: %v", err)
	}

	// neither role may write title once the record is Signed
	_, err = e.Update(ctx, unscoped, "Doc", signed.ID, map[string]any{"title": "x"}, basis, "")
	wantCode(t, err, CodePermissionDenied)
	_, err = e.Update(ctx, scoped, "Doc", signed.ID, map[string]any{"title": "x"}, basis, "")
	wantCode(t, err, CodePermissionDenied)
}

// §5.5 — a role with NO update rules at all (not even a deny) fails cleanly
// at gate (1): no panic, no silent no-op, a named PERMISSION_DENIED.
func TestNoUpdateRightCleanError(t *testing.T) {
	e := newLLD20Engine(t)
	nobody := eventstore.Actor{Type: eventstore.ActorHuman, ID: "n-1", Role: "NoUpdate"}

	doc, err := e.Create(ctx, admin, "Doc", map[string]any{"title": "a"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Update(ctx, nobody, "Doc", doc.ID, map[string]any{"title": "b"}, basis, "")
	pe := wantCode(t, err, CodePermissionDenied)
	if pe.Rule == "" {
		t.Fatal("must name a rule, even the synthetic 'default deny' one")
	}

	// direct gate (1) check must be a clean, non-panicking false — not an
	// empty/zero decision that could be mistaken for allowed.
	d := e.canUpdateEntity("NoUpdate", "Doc")
	if d.allowed {
		t.Fatal("canUpdateEntity must deny a role with zero update rules")
	}
	if d.rule == "" {
		t.Fatal("denial must carry an explanatory rule string")
	}
}

// §5.4 / §3 — the key non-loosening proof. Exhaustively compares the OLD
// gate (1) (can(actor,"update",entity,"",record) — unchanged, still the
// production `can` function, just no longer wired into Update) against the
// NEW gate (1) (canUpdateEntity) for every (role, field, record) triple,
// each gate composed with the unchanged gate (2) per-field check to get the
// actual permitted-write outcome under old vs. new code.
//
// Required invariant: new ⊇ old everywhere (no previously allowed write
// becomes denied), and every cell where new != old is a write that a
// SCOPED-ONLY role's own grant explicitly names — nothing else flips.
func TestUpdateNotLoosened(t *testing.T) {
	e := newLLD20Engine(t)

	roles := []string{"Admin", "Editor", "ScopedOnly", "NoUpdate", "LockedUnscoped", "LockedScoped"}
	fields := []string{"title", "secret", "amount", "owner", "status"}
	records := map[string]map[string]any{
		"draft":  {"title": "d", "secret": "s", "amount": 1.0, "owner": "alice", "status": "Draft"},
		"signed": {"title": "d", "secret": "s", "amount": 1.0, "owner": "alice", "status": "Signed"},
	}

	// the ONLY new capability this fix introduces: a scoped-only role's own
	// named fields become writable (subject to gate 2, e.g. record locks).
	newCapability := map[string]map[string]bool{
		"ScopedOnly":   {"title": true, "owner": true},
		"LockedScoped": {"title": true},
	}

	oldGate1 := func(role, entity string, record map[string]any) bool {
		actor := eventstore.Actor{Role: role}
		return e.can(actor, "update", entity, "", record).allowed
	}
	gate2 := func(role, entity, field string, record map[string]any) bool {
		actor := eventstore.Actor{Role: role}
		return e.can(actor, "update", entity, field, record).allowed
	}

	diffs := 0
	for _, role := range roles {
		for recName, rec := range records {
			for _, field := range fields {
				oldPermit := oldGate1(role, "Doc", rec) && gate2(role, "Doc", field, rec)
				newPermit := e.canUpdateEntity(role, "Doc").allowed && gate2(role, "Doc", field, rec)

				if oldPermit && !newPermit {
					t.Fatalf("LOOSENED IN REVERSE (bug in the proof, not the fix): role=%s field=%s record=%s old=true new=false",
						role, field, recName)
				}
				if oldPermit == newPermit {
					continue
				}
				// oldPermit==false, newPermit==true: must be an explicitly
				// expected new capability, nothing else.
				diffs++
				if !newCapability[role][field] {
					t.Fatalf("UNEXPECTED LOOSENING: role=%s field=%s record=%s now permitted (old=false new=true) "+
						"but this field is not in that role's granted scope", role, field, recName)
				}
			}
		}
	}
	if diffs == 0 {
		t.Fatal("proof is vacuous: expected at least one (role,field,record) where the fix newly permits a scoped write")
	}
}
