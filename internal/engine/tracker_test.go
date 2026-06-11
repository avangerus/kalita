package engine

import (
	"os"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// The completeness test: the Jira-like tracker pack runs end-to-end — rich
// fields, named links, a workflow with HITL — proving the primitives suffice.
func TestTrackerPackEndToEnd(t *testing.T) {
	files := map[string]string{}
	for _, n := range []string{"pack.kal", "tracker.kal"} {
		raw, err := os.ReadFile("../../packs/tracker/" + n)
		if err != nil {
			t.Fatal(err)
		}
		files[n] = string(raw)
	}
	model, errs := dsl.Compile(files)
	if len(errs) > 0 {
		t.Fatalf("tracker must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	e, err := New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	lead := eventstore.Actor{Type: eventstore.ActorHuman, ID: "mike", Role: "Lead"}

	proj, err := e.Create(ctx, lead, "Project", map[string]any{"name": "Platform", "key": "PLAT", "color": "#4da3ff"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := e.Create(ctx, lead, "Issue", map[string]any{
		"project": proj.ID, "title": "Auth", "type": "Story", "story_points": 8.0,
		"estimate": "2d4h", "progress": 30.0, "labels": []any{"security"},
		"components": []any{"Backend", "Infra"},
	}, basis, "")
	if err != nil {
		t.Fatalf("rich issue must create: %v", err)
	}
	b, _ := e.Create(ctx, lead, "Issue", map[string]any{"project": proj.ID, "title": "UI"}, basis, "")

	// link: A blocks B; B sees blocked_by A
	if err := e.Link(ctx, lead, "Issue", a.ID, "blocks", b.ID, basis); err != nil {
		t.Fatal(err)
	}
	if l := e.LinksOf(lead, "Issue", b.ID); len(l) != 1 || l[0].Name != "blocked_by" {
		t.Fatalf("B must see blocked_by: %+v", l)
	}

	// workflow with act permissions; approve is HITL
	for _, act := range []string{"plan", "start", "submit_for_review"} {
		if _, err := e.Act(ctx, lead, "Issue", a.ID, act, basis, ""); err != nil {
			t.Fatalf("%s: %v", act, err)
		}
	}
	res, err := e.Act(ctx, lead, "Issue", a.ID, "approve", basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "pending_approval" {
		t.Fatalf("approve must park for Lead signature (HITL), got %s", res.Status)
	}
	got, _ := e.Get(ctx, lead, "Issue", a.ID)
	if got.Values["status"] != "InReview" {
		t.Fatalf("must wait at InReview, got %v", got.Values["status"])
	}
}
