package engine

import (
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// ABAC over related records (the Jira bone in the throat): a developer sees an
// issue if (they report it OR they own its project) AND it is not Closed.
func TestABACOverRelations(t *testing.T) {
	src := `
entity Project:
    name: string required
    owner: ref[core.User]

entity Issue:
    project: ref[Project] on_delete=restrict
    title: string required
    reporter: ref[core.User]
    status: enum[Open, Closed] default=Open

roles:
    Admin
    Dev

permissions:
    Admin:
        full [Project, Issue]
    Dev:
        read [Project]
        read Issue where (reporter = $me or project.owner = $me) and status != Closed
        create [Issue]
        deny [delete *, update Project.*]
`
	model, errs := dsl.Compile(map[string]string{"t.dsl": src})
	if len(errs) > 0 {
		t.Fatalf("compile: %v", errs[0])
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	admin := eventstore.Actor{Type: eventstore.ActorHuman, ID: "boss", Role: "Admin"}
	dev := eventstore.Actor{Type: eventstore.ActorHuman, ID: "alice", Role: "Dev"}

	// alice owns project P1; bob owns P2
	p1, _ := e.Create(ctx, admin, "Project", map[string]any{"name": "P1", "owner": "alice"}, basis, "")
	p2, _ := e.Create(ctx, admin, "Project", map[string]any{"name": "P2", "owner": "bob"}, basis, "")

	// i1: in alice's project, reported by bob — visible via project.owner
	i1, _ := e.Create(ctx, admin, "Issue", map[string]any{"project": p1.ID, "title": "a", "reporter": "bob"}, basis, "")
	// i2: in bob's project, reported by alice — visible via reporter
	i2, _ := e.Create(ctx, admin, "Issue", map[string]any{"project": p2.ID, "title": "b", "reporter": "alice"}, basis, "")
	// i3: bob's project, bob reports — invisible to alice
	i3, _ := e.Create(ctx, admin, "Issue", map[string]any{"project": p2.ID, "title": "c", "reporter": "bob"}, basis, "")

	can := func(id string) bool {
		_, err := e.Get(ctx, dev, "Issue", id)
		return err == nil
	}
	if !can(i1.ID) {
		t.Fatal("i1 must be visible via project.owner = $me (ref-path ABAC)")
	}
	if !can(i2.ID) {
		t.Fatal("i2 must be visible via reporter = $me (or branch)")
	}
	if can(i3.ID) {
		t.Fatal("i3 must be invisible (neither branch matches)")
	}

	// close i1 -> the `and status != Closed` clause hides it
	if _, err := e.Update(ctx, admin, "Issue", i1.ID, map[string]any{"status": "Closed"}, basis, ""); err != nil {
		t.Fatal(err)
	}
	if can(i1.ID) {
		t.Fatal("closed i1 must be hidden by the status clause")
	}

	// Query honors the same ABAC: alice sees exactly i2 now
	rows, _ := e.Query(ctx, dev, "Issue", QueryOpts{})
	if len(rows) != 1 || rows[0].ID != i2.ID {
		t.Fatalf("query must return only i2, got %d rows", len(rows))
	}
}
