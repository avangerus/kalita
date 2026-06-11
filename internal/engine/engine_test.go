package engine

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

var ctx = context.Background()

var (
	admin  = eventstore.Actor{Type: eventstore.ActorHuman, ID: "alice", Role: "Admin"}
	editor = eventstore.Actor{Type: eventstore.ActorHuman, ID: "eve", Role: "Editor"}
	bot    = eventstore.Actor{Type: eventstore.ActorAgent, ID: "bot-1", Role: "Bot"}
	basis  = &eventstore.Basis{Type: "human", ID: "alice"}
)

const testPack = `
entity Doc:
    title: string required
    secret: string
    code: string unique
    owner: ref[core.User] default=$me
    parent: ref[Doc]
    amount: money
    status: enum[Draft, Final] default=Draft
    size: int computed = length(title)

constraints:
    unique(title, status)

roles:
    Admin
    Editor
    Bot agent

permissions:
    Admin:
        full [Doc]
    Editor:
        read [Doc]
        update [Doc]
        deny [update Doc.amount, read Doc.secret]
    Bot:
        read Doc where owner = $me
        create [Doc]
        deny [update Doc.*, delete *]
`

func newEngine(t *testing.T) (*Engine, eventstore.Store) {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"test.kal": testPack})
	if len(errs) > 0 {
		t.Fatalf("test pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	e, err := New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	return e, store
}

func wantCode(t *testing.T, err error, code string) *Err {
	t.Helper()
	var e *Err
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
	return e
}

// --- permission matrix -------------------------------------------------------

func TestDefaultDeny(t *testing.T) {
	e, _ := newEngine(t)
	doc, err := e.Create(ctx, admin, "Doc", map[string]any{"title": "a"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Update(ctx, bot, "Doc", doc.ID, map[string]any{"title": "b"}, basis, "")
	wantCode(t, err, CodePermissionDenied)
}

func TestDenyBeatsAllow(t *testing.T) {
	e, _ := newEngine(t)
	doc, _ := e.Create(ctx, admin, "Doc", map[string]any{"title": "a", "amount": 10.0}, basis, "")

	if _, err := e.Update(ctx, editor, "Doc", doc.ID, map[string]any{"title": "b"}, basis, ""); err != nil {
		t.Fatalf("Editor may update title: %v", err)
	}
	_, err := e.Update(ctx, editor, "Doc", doc.ID, map[string]any{"amount": 99.0}, basis, "")
	pe := wantCode(t, err, CodePermissionDenied)
	if pe.Rule == "" {
		t.Fatal("PERMISSION_DENIED must name the deciding rule")
	}
}

func TestFieldMasking(t *testing.T) {
	e, _ := newEngine(t)
	doc, _ := e.Create(ctx, admin, "Doc", map[string]any{"title": "a", "secret": "s3cr3t"}, basis, "")

	got, err := e.Get(ctx, editor, "Doc", doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, leaked := got.Values["secret"]; leaked {
		t.Fatal("Editor must not see Doc.secret")
	}
	full, _ := e.Get(ctx, admin, "Doc", doc.ID)
	if full.Values["secret"] != "s3cr3t" {
		t.Fatal("Admin must see Doc.secret")
	}
}

func TestRowLevelRead(t *testing.T) {
	e, _ := newEngine(t)
	mine, _ := e.Create(ctx, bot, "Doc", map[string]any{"title": "mine"}, basis, "")
	other, _ := e.Create(ctx, admin, "Doc", map[string]any{"title": "alien", "owner": "alice"}, basis, "")

	if _, err := e.Get(ctx, bot, "Doc", mine.ID); err != nil {
		t.Fatalf("bot must read its own doc: %v", err)
	}
	// invisible records report NOT_FOUND, not PERMISSION_DENIED
	_, err := e.Get(ctx, bot, "Doc", other.ID)
	wantCode(t, err, CodeNotFound)

	rows, err := e.Query(ctx, bot, "Doc", QueryOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != mine.ID {
		t.Fatalf("bot query must see exactly its own doc, got %d rows", len(rows))
	}
}

func TestDefaultMe(t *testing.T) {
	e, _ := newEngine(t)
	doc, err := e.Create(ctx, bot, "Doc", map[string]any{"title": "x"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Values["owner"] != "bot-1" {
		t.Fatalf("default=$me must resolve to the actor id, got %v", doc.Values["owner"])
	}
	if doc.Values["status"] != "Draft" {
		t.Fatalf("enum default must apply, got %v", doc.Values["status"])
	}
}

// --- validation ----------------------------------------------------------------

func TestValidation(t *testing.T) {
	e, _ := newEngine(t)
	cases := []struct {
		name   string
		values map[string]any
		code   string
	}{
		{"unknown field", map[string]any{"title": "a", "ghost": 1}, CodeValidation},
		{"missing required", map[string]any{"amount": 1.0}, CodeValidation},
		{"wrong type", map[string]any{"title": 42.0}, CodeValidation},
		{"bad enum", map[string]any{"title": "a", "status": "Banana"}, CodeValidation},
		{"computed is read-only", map[string]any{"title": "a", "size": 3.0}, CodeValidation},
		{"missing ref", map[string]any{"title": "a", "parent": "ghost-id"}, CodeValidation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.Create(ctx, admin, "Doc", tc.values, basis, "")
			ve := wantCode(t, err, tc.code)
			if ve.FixHint == "" {
				t.Fatal("validation errors must carry fix_hint")
			}
		})
	}
}

func TestUniqueConstraints(t *testing.T) {
	e, _ := newEngine(t)
	if _, err := e.Create(ctx, admin, "Doc", map[string]any{"title": "a", "code": "X1"}, basis, ""); err != nil {
		t.Fatal(err)
	}
	// field-level unique
	_, err := e.Create(ctx, admin, "Doc", map[string]any{"title": "b", "code": "X1"}, basis, "")
	wantCode(t, err, CodeConflict)
	// entity-level unique(title, status)
	_, err = e.Create(ctx, admin, "Doc", map[string]any{"title": "a"}, basis, "")
	wantCode(t, err, CodeConflict)
}

// --- journal discipline ----------------------------------------------------------

func TestBasisRequired(t *testing.T) {
	e, _ := newEngine(t)
	_, err := e.Create(ctx, admin, "Doc", map[string]any{"title": "a"}, nil, "")
	wantCode(t, err, CodeBasisRequired)
}

func TestEverythingIsAnEvent(t *testing.T) {
	e, store := newEngine(t)
	doc, _ := e.Create(ctx, admin, "Doc", map[string]any{"title": "a"}, basis, "")
	_, _ = e.Update(ctx, admin, "Doc", doc.ID, map[string]any{"title": "b"}, basis, "")

	events, _ := store.All(ctx)
	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d", len(events))
	}
	if events[0].Kind != eventstore.RecordCreated || events[1].Kind != eventstore.RecordUpdated {
		t.Fatalf("wrong kinds: %s, %s", events[0].Kind, events[1].Kind)
	}
	if events[1].Basis == nil {
		t.Fatal("events must carry basis")
	}
}

func TestReplayRebuildsIdenticalState(t *testing.T) {
	e, store := newEngine(t)
	doc, _ := e.Create(ctx, admin, "Doc", map[string]any{"title": "a", "amount": 5.0}, basis, "")
	_, _ = e.Update(ctx, admin, "Doc", doc.ID, map[string]any{"amount": 7.0}, basis, "")

	e2, err := New(ctx, e.model, store)
	if err != nil {
		t.Fatal(err)
	}
	got, err := e2.Get(ctx, admin, "Doc", doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Values["amount"] != 7.0 || got.Values["title"] != "a" {
		t.Fatalf("replayed state differs: %v", got.Values)
	}
}

func TestIdempotentCreate(t *testing.T) {
	e, store := newEngine(t)
	a, _ := e.Create(ctx, admin, "Doc", map[string]any{"title": "a"}, basis, "k1")
	b, _ := e.Create(ctx, admin, "Doc", map[string]any{"title": "a"}, basis, "k1")
	if a.ID != b.ID {
		t.Fatal("same idempotency key must return the same record")
	}
	events, _ := store.All(ctx)
	if len(events) != 1 {
		t.Fatalf("want 1 event, got %d", len(events))
	}
}

func TestSingleton(t *testing.T) {
	src := `
entity Settings singleton:
    model: string required default="bge-m3"
    chunk: int default=512

roles:
    Owner

permissions:
    Owner:
        full [Settings]
`
	model, errs := dsl.Compile(map[string]string{"t.kal": src})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	first, err := e.Create(ctx, owner, "Settings", map[string]any{}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Values["model"] != "bge-m3" {
		t.Fatalf("defaults must apply: %v", first.Values)
	}
	// second instance must conflict
	_, err = e.Create(ctx, owner, "Settings", map[string]any{}, basis, "")
	ce := wantCode(t, err, CodeConflict)
	if ce.FixHint == "" {
		t.Fatal("singleton conflict must hint at updating")
	}
	// updates work normally — and are journaled (who switched the model)
	if _, err := e.Update(ctx, owner, "Settings", first.ID, map[string]any{"model": "e5-large"}, basis, ""); err != nil {
		t.Fatal(err)
	}
}

// --- additive migration prototype (risk #1, exercised early) ---------------------

const testPackV2 = testPack + `
entity Note:
    body: text required

permissions:
    Admin:
        full [Note]
`

func TestAdditiveMigration(t *testing.T) {
	e, store := newEngine(t)
	doc, _ := e.Create(ctx, admin, "Doc", map[string]any{"title": "a"}, basis, "")

	model2, errs := dsl.Compile(map[string]string{"test.kal": testPackV2})
	if len(errs) > 0 {
		t.Fatalf("v2 must compile: %v", errs[0])
	}
	if err := e.ApplyAdditive(ctx, admin, model2, basis); err != nil {
		t.Fatalf("additive change must apply: %v", err)
	}
	if e.DefVersion() != 2 {
		t.Fatalf("def_version must bump to 2, got %d", e.DefVersion())
	}
	// old data lives on
	if _, err := e.Get(ctx, admin, "Doc", doc.ID); err != nil {
		t.Fatal(err)
	}
	// new entity usable
	if _, err := e.Create(ctx, admin, "Note", map[string]any{"body": "hello"}, basis, ""); err != nil {
		t.Fatal(err)
	}
	// replay catches up def_version
	e2, err := New(ctx, model2, store)
	if err != nil {
		t.Fatal(err)
	}
	if e2.DefVersion() != 2 {
		t.Fatalf("replayed def_version must be 2, got %d", e2.DefVersion())
	}
}

func TestDestructiveMigrationRejected(t *testing.T) {
	e, _ := newEngine(t)
	// v2 drops the Doc entity entirely
	model2, errs := dsl.Compile(map[string]string{"test.kal": "entity Other:\n    x: int\n"})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	err := e.ApplyAdditive(ctx, admin, model2, basis)
	wantCode(t, err, CodeValidation)
}

// --- acceptance pack end-to-end ---------------------------------------------------

func TestCollectionsEndToEnd(t *testing.T) {
	files := map[string]string{}
	for _, name := range []string{"pack.kal", "collections.kal"} {
		raw, err := os.ReadFile("../../examples/collections/" + name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(raw)
	}
	model, errs := dsl.Compile(files)
	if len(errs) > 0 {
		t.Fatalf("collections must compile: %v", errs[0])
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}

	accountant := eventstore.Actor{Type: eventstore.ActorHuman, ID: "anna", Role: "Accountant"}
	collector := eventstore.Actor{Type: eventstore.ActorAgent, ID: "collector-1", Role: "Collector"}

	open, err := e.Create(ctx, accountant, "Contract",
		map[string]any{"company": "Vector LLC", "due_date": "2026-05-01", "amount": 100000.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	classified, err := e.Create(ctx, accountant, "Contract",
		map[string]any{"company": "Secret Corp", "due_date": "2026-05-01", "classified": true}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	debtor, err := e.Create(ctx, accountant, "Debtor",
		map[string]any{"company": "Vector LLC", "contract": open.ID, "debt": 100000.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}

	// collector sees the open contract, the classified one does not exist for it
	if _, err := e.Get(ctx, collector, "Contract", open.ID); err != nil {
		t.Fatalf("collector must read open contracts: %v", err)
	}
	if _, err := e.Get(ctx, collector, "Contract", classified.ID); err == nil {
		t.Fatal("classified contract must be invisible to the collector")
	}
	// collector may not touch the debt
	_, err = e.Update(ctx, collector, "Debtor", debtor.ID, map[string]any{"debt": 0.0}, basis, "")
	wantCode(t, err, CodePermissionDenied)
	// and may not create anything
	_, err = e.Create(ctx, collector, "Debtor", map[string]any{"company": "X", "contract": open.ID}, basis, "")
	wantCode(t, err, CodePermissionDenied)
}
