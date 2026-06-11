package engine

import (
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Aggregates roll up over related records on read — Jira's story-point sum,
// counts, averages — without invalidation.
func TestAggregates(t *testing.T) {
	src := `
entity Epic:
    title: string required
    total_points: int computed = sum(Task.points where epic = $self)
    task_count: int computed = count(Task where epic = $self)
    avg_points: float computed = avg(Task.points where epic = $self)
    max_points: int computed = max(Task.points where epic = $self)

entity Task:
    title: string required
    epic: ref[Epic]
    points: int

roles:
    Owner

permissions:
    Owner:
        full [Epic, Task]
`
	model, errs := dsl.Compile(map[string]string{"t.kal": src})
	if len(errs) > 0 {
		t.Fatalf("compile: %v", errs[0])
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	epic, _ := e.Create(ctx, owner, "Epic", map[string]any{"title": "Auth"}, basis, "")

	// empty epic: zero everything
	g, _ := e.Get(ctx, owner, "Epic", epic.ID)
	if g.Values["total_points"] != 0.0 || g.Values["task_count"] != 0.0 {
		t.Fatalf("empty epic must be 0/0: %v", g.Values)
	}

	for _, p := range []float64{3, 5, 8} {
		_, err := e.Create(ctx, owner, "Task", map[string]any{"title": "t", "epic": epic.ID, "points": p}, basis, "")
		if err != nil {
			t.Fatal(err)
		}
	}
	// a task in no epic must not count
	_, _ = e.Create(ctx, owner, "Task", map[string]any{"title": "orphan", "points": 100.0}, basis, "")

	g, _ = e.Get(ctx, owner, "Epic", epic.ID)
	if g.Values["total_points"] != 16.0 {
		t.Fatalf("sum must be 16, got %v", g.Values["total_points"])
	}
	if g.Values["task_count"] != 3.0 {
		t.Fatalf("count must be 3, got %v", g.Values["task_count"])
	}
	if g.Values["avg_points"] != 16.0/3.0 {
		t.Fatalf("avg wrong: %v", g.Values["avg_points"])
	}
	if g.Values["max_points"] != 8.0 {
		t.Fatalf("max must be 8, got %v", g.Values["max_points"])
	}
}
