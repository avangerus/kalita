package dsl_test

// LLD-22 §6, tests 1 and 4: proving the inlined Model is not just structurally
// correct but actually usable by the runtime — create/read round-trip through
// internal/engine, and that permissions on an included field are enforced,
// not merely accepted by the compiler. This file is `package dsl_test` (an
// external test package, still physically inside internal/dsl per the LLD's
// file layout) because internal/engine imports internal/dsl — a same-package
// test here would be a compile cycle.

import (
	"context"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
)

// TestIncludeInlinesFields: an entity with `include` compiles; the resulting
// Model carries the mixin's fields ahead of the entity's own fields, in
// include order (LLD-22 §2); and the inlined fields are ordinary enough for
// the engine to create and read a record through them.
func TestIncludeInlinesFields(t *testing.T) {
	src := `pack a
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1
entity Ticket:
    include core.Assignable
    include core.Named
    title: string required
    status: enum[New, Done] default=New
roles:
    Admin
permissions:
    Admin:
        full [Ticket]
`
	model, errs := dsl.Compile(map[string]string{"a.dsl": src})
	if len(errs) > 0 {
		t.Fatalf("entity with include must compile clean, got: %v", errs)
	}

	ticket, ok := model.Entities["Ticket"]
	if !ok {
		t.Fatal("entity Ticket must exist")
	}
	wantOrder := []string{"owner", "assignee", "name", "title", "status"}
	if len(ticket.Fields) != len(wantOrder) {
		t.Fatalf("Ticket must have %d fields (2 from Assignable + 1 from Named + 2 own), got %d: %+v",
			len(wantOrder), len(ticket.Fields), ticket.Fields)
	}
	for i, name := range wantOrder {
		if ticket.Fields[i].Name != name {
			t.Fatalf("field order wrong at %d: want %s, got %s (full: %v)", i, name, ticket.Fields[i].Name, fieldNames(ticket.Fields))
		}
	}
	owner := ticket.Fields[0]
	if owner.Type.Kind != dsl.TyRef || owner.Type.RefTarget != "core.User" || owner.Default != "$me" {
		t.Fatalf("core.Assignable.owner parsed wrong: %+v", owner)
	}
	name := ticket.Fields[2]
	if name.Type.Kind != dsl.TyScalar || name.Type.Scalar != "string" || !name.Required {
		t.Fatalf("core.Named.name parsed wrong: %+v", name)
	}

	// create/read work: the mixin fields behave like hand-written ones.
	ctx := context.Background()
	eng, err := engine.New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	admin := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Admin"}
	basis := &eventstore.Basis{Type: "human", ID: "root"}
	rec, err := eng.Create(ctx, admin, "Ticket", map[string]any{
		"title": "Fix the thing", "name": "T-1", "owner": "root",
	}, basis, "")
	if err != nil {
		t.Fatalf("create with mixin fields: %v", err)
	}
	got, err := eng.Get(ctx, admin, "Ticket", rec.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Values["owner"] != "root" || got.Values["name"] != "T-1" || got.Values["title"] != "Fix the thing" {
		t.Fatalf("mixin + own field values did not round-trip: %+v", got.Values)
	}
}

// TestIncludePermsSeeMixinFields: `update [Ticket.owner]` — a permission
// targeting a field brought in by `include core.Assignable` — validates at
// compile time (the field is not "invisible" to permission checks) and is
// enforced at runtime exactly like a hand-written field.
func TestIncludePermsSeeMixinFields(t *testing.T) {
	src := `pack a
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1
entity Ticket:
    include core.Assignable
    title: string required
roles:
    Admin
    Clerk
permissions:
    Admin:
        full [Ticket]
    Clerk:
        read [Ticket]
        create [Ticket]
        update [Ticket.owner]
`
	model, errs := dsl.Compile(map[string]string{"a.dsl": src})
	if len(errs) > 0 {
		t.Fatalf("update [Ticket.owner] on an included field must validate clean, got: %v", errs)
	}

	ctx := context.Background()
	eng, err := engine.New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	admin := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Admin"}
	clerk := eventstore.Actor{Type: eventstore.ActorHuman, ID: "u1", Role: "Clerk"}
	basis := &eventstore.Basis{Type: "human", ID: "root"}

	rec, err := eng.Create(ctx, admin, "Ticket", map[string]any{"title": "T"}, basis, "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Clerk is granted update only on the mixin field owner — updating it works.
	if _, err := eng.Update(ctx, clerk, "Ticket", rec.ID, map[string]any{"owner": "u1"}, basis, ""); err != nil {
		t.Fatalf("Clerk must be able to update the granted mixin field owner: %v", err)
	}
	// Clerk was not granted update on title (own field) — must be denied,
	// proving the grant is scoped to the named field and not a blanket pass.
	if _, err := eng.Update(ctx, clerk, "Ticket", rec.ID, map[string]any{"title": "other"}, basis, ""); err == nil {
		t.Fatal("Clerk must NOT be able to update title (not granted) — permission scoping broken")
	}
}

func fieldNames(fs []*dsl.FieldDecl) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Name
	}
	return out
}
