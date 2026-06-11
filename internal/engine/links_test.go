package engine

import (
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Named bidirectional links: adding `blocks` from A makes A see `blocks` B and
// B see `blocked_by` A — one fact, two views, kept consistent and replayable.
func TestBidirectionalLinks(t *testing.T) {
	src := `
entity Task:
    title: string required

roles:
    Owner

permissions:
    Owner:
        full [Task]

link Task -> Task as blocks / blocked_by
`
	model, errs := dsl.Compile(map[string]string{"t.kal": src})
	if len(errs) > 0 {
		t.Fatalf("link pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	e, err := New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	a, _ := e.Create(ctx, owner, "Task", map[string]any{"title": "A"}, basis, "")
	b, _ := e.Create(ctx, owner, "Task", map[string]any{"title": "B"}, basis, "")

	// A blocks B
	if err := e.Link(ctx, owner, "Task", a.ID, "blocks", b.ID, basis); err != nil {
		t.Fatal(err)
	}
	// A sees blocks -> B
	la := e.LinksOf(owner, "Task", a.ID)
	if len(la) != 1 || la[0].Name != "blocks" || la[0].RecordID != b.ID {
		t.Fatalf("A must see blocks->B: %+v", la)
	}
	// B sees blocked_by -> A (the inverse, automatically)
	lb := e.LinksOf(owner, "Task", b.ID)
	if len(lb) != 1 || lb[0].Name != "blocked_by" || lb[0].RecordID != a.ID {
		t.Fatalf("B must see blocked_by->A: %+v", lb)
	}

	// linking via the inverse name from B is the same fact (idempotent)
	if err := e.Link(ctx, owner, "Task", b.ID, "blocked_by", a.ID, basis); err != nil {
		t.Fatal(err)
	}
	if got := e.LinksOf(owner, "Task", a.ID); len(got) != 1 {
		t.Fatalf("inverse link must be the same fact, got %d", len(got))
	}

	// unknown link name rejected
	if err := e.Link(ctx, owner, "Task", a.ID, "relates", b.ID, basis); err == nil {
		t.Fatal("unknown link name must be rejected")
	}

	// unlink removes both views
	if err := e.Unlink(ctx, owner, "Task", a.ID, "blocks", b.ID, basis); err != nil {
		t.Fatal(err)
	}
	if len(e.LinksOf(owner, "Task", a.ID)) != 0 || len(e.LinksOf(owner, "Task", b.ID)) != 0 {
		t.Fatal("unlink must clear both sides")
	}

	// replay rebuilds the link state (re-link first)
	_ = e.Link(ctx, owner, "Task", a.ID, "blocks", b.ID, basis)
	e2, err := New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	if got := e2.LinksOf(owner, "Task", b.ID); len(got) != 1 || got[0].Name != "blocked_by" {
		t.Fatalf("replay must restore links: %+v", got)
	}
}

func TestLinkSemanticErrors(t *testing.T) {
	cases := []struct {
		name string
		src  string
		code dsl.Code
	}{
		{"bad syntax", "entity Task:\n    title: string\n\nlink Task blocks Task", dsl.EBadLink},
		{"unknown entity", "entity Task:\n    title: string\n\nlink Task -> Ghost as a / b", dsl.ELinkEntity},
		{"duplicate name", "entity Task:\n    title: string\n\nlink Task -> Task as blocks / x\nlink Task -> Task as blocks / y", dsl.EDupLinkName},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, errs := dsl.Compile(map[string]string{"t.kal": tc.src})
			found := false
			for _, e := range errs {
				if e.Code == tc.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("want %s, got %v", tc.code, errs)
			}
		})
	}
}
