package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// The founder's existential worry, answered with a measurement, not a claim:
// "they put a dictionary in an entity, add row-level permissions, and the
// 'give me only my records' query goes O(n) and dies — so kalita is junk."
//
// It does not die. When a role's read scope is `where field = $me[.attr]`, the
// secondary index serves the permitted set straight from that field's bucket:
// answering one actor's query examines only THAT actor's share of the rows, not
// the whole table — independent of how large the table is. can() still runs per
// candidate as the final authority, so the index narrows but never leaks.

const scaleN = 20000 // rows
const scaleM = 200   // distinct owners -> scaleN/scaleM rows each

func scaleModel(t testing.TB) *dsl.Model {
	src := `pack t
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1
entity Doc:
    owner: string
    body:  string
roles:
    Reader
    Admin
permissions:
    Reader:
        read Doc where owner = $me
    Admin:
        full [Doc]
`
	model, errs := dsl.Compile(map[string]string{"t.dsl": src})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	return model
}

func seedDocs(t testing.TB, n, m int) *Engine {
	ctx := context.Background()
	eng, err := New(ctx, scaleModel(t), eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	admin := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Admin"}
	basis := &eventstore.Basis{Type: "human", ID: "root"}
	for i := 0; i < n; i++ {
		owner := fmt.Sprintf("u%d", i%m)
		if _, err := eng.Create(ctx, admin, "Doc", map[string]any{
			"owner": owner, "body": "row"}, basis, ""); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	return eng
}

func TestPermittedSetNarrowsAtScale(t *testing.T) {
	if testing.Short() {
		t.Skip("scale seed is heavy; run without -short")
	}
	ctx := context.Background()
	eng := seedDocs(t, scaleN, scaleM)
	me := eventstore.Actor{Type: eventstore.ActorHuman, ID: "u7", Role: "Reader"}
	want := scaleN / scaleM // this owner's share

	// (1) the index narrows the candidate set to exactly this owner's rows —
	// O(owned), not O(table). This is the whole anti-O(n) guarantee, measured.
	ids, narrowed := eng.candidateIDs("Doc", me)
	if !narrowed {
		t.Fatal("a `where owner = $me` read scope MUST be index-backed, got a full scan")
	}
	if len(ids) != want {
		t.Fatalf("candidate set = %d rows, want %d (the index must serve only the owner's bucket, not scan %d)", len(ids), want, scaleN)
	}

	// (2) the result is correct and leaks nothing: exactly the owner's rows
	got, err := eng.Query(ctx, me, "Doc", QueryOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != want {
		t.Fatalf("query returned %d rows, want %d", len(got), want)
	}
	for _, r := range got {
		if r.Values["owner"] != "u7" {
			t.Fatalf("permitted-set query leaked a row owned by %v", r.Values["owner"])
		}
	}

	// (3) an unscoped role (Admin: read [Doc]) cannot be narrowed — it honestly
	// falls back to a scan. The index is an optimization for scoped reads, never
	// a correctness crutch.
	if _, narrowedAdmin := eng.candidateIDs("Doc", eventstore.Actor{ID: "root", Role: "Admin"}); narrowedAdmin {
		t.Error("an unconditional `read [Doc]` must NOT be index-narrowed")
	}
}

// The harder, more common scope: INDIRECT ownership. "I see tasks in projects I
// own" — read Task where project.owner = $me — is a two-level join the direct
// index misses, so it used to scan every task. The ref-path index now serves it
// from the union of the owner's project buckets: still O(owned), not O(table).
func TestRefPathPermittedSetNarrows(t *testing.T) {
	if testing.Short() {
		t.Skip("scale seed is heavy; run without -short")
	}
	ctx := context.Background()
	src := `pack t
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1
entity Project:
    owner: string
entity Task:
    project: ref[Project]
    title:   string
roles:
    Member
    Admin
permissions:
    Member:
        read Task where project.owner = $me
        read Project where owner = $me
    Admin:
        full [Project, Task]
`
	model, errs := dsl.Compile(map[string]string{"t.dsl": src})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	eng, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	admin := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Admin"}
	basis := &eventstore.Basis{Type: "human", ID: "root"}

	const owners, projPer, taskPer = 200, 5, 20 // 200 owners × 5 projects × 20 tasks = 20000 tasks
	mineProjects := map[string]bool{}           // u7's project ids — for the no-leak check
	for o := 0; o < owners; o++ {
		owner := fmt.Sprintf("u%d", o)
		for p := 0; p < projPer; p++ {
			proj, err := eng.Create(ctx, admin, "Project", map[string]any{"owner": owner}, basis, "")
			if err != nil {
				t.Fatalf("seed project: %v", err)
			}
			if owner == "u7" {
				mineProjects[proj.ID] = true
			}
			for t2 := 0; t2 < taskPer; t2++ {
				if _, err := eng.Create(ctx, admin, "Task", map[string]any{
					"project": proj.ID, "title": "t"}, basis, ""); err != nil {
					t.Fatalf("seed task: %v", err)
				}
			}
		}
	}
	me := eventstore.Actor{Type: eventstore.ActorHuman, ID: "u7", Role: "Member"}
	want := projPer * taskPer // 100 tasks across this owner's projects

	ids, narrowed := eng.candidateIDs("Task", me)
	if !narrowed {
		t.Fatal("a ref-path `where project.owner = $me` read scope MUST be index-backed, got a full scan")
	}
	if len(ids) != want {
		t.Fatalf("ref-path candidate set = %d, want %d (must union only the owner's project buckets, not scan 20000)", len(ids), want)
	}
	got, err := eng.Query(ctx, me, "Task", QueryOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != want {
		t.Fatalf("ref-path query returned %d rows, want %d", len(got), want)
	}
	for _, r := range got { // no leak: every task hangs off a project this actor owns
		if !mineProjects[fmt.Sprint(r.Values["project"])] {
			t.Fatalf("ref-path query leaked a task in project %v, not owned by u7", r.Values["project"])
		}
	}
}

// BenchmarkPermittedSet contrasts the two costs over the SAME 100k-row table:
// answering one actor's index-backed `where owner = $me` query, versus the
// full-table scan an unscoped reader pays. The ratio is the headline — the
// scoped query's cost tracks the owner's share, not the table size.
func BenchmarkPermittedSet(b *testing.B) {
	ctx := context.Background()
	eng := seedDocs(b, 100000, 1000) // 100 rows per owner
	scoped := eventstore.Actor{Type: eventstore.ActorHuman, ID: "u42", Role: "Reader"}
	admin := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Admin"}

	b.Run("scoped_index", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := eng.Query(ctx, scoped, "Doc", QueryOpts{}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("full_scan", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := eng.Query(ctx, admin, "Doc", QueryOpts{Limit: 1}); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkRefPathPermittedSet times the indirect-ownership query (tasks in the
// projects I own) over 100k tasks — the two-level join that used to scan.
func BenchmarkRefPathPermittedSet(b *testing.B) {
	ctx := context.Background()
	src := `pack t
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1
entity Project:
    owner: string
entity Task:
    project: ref[Project]
    title:   string
roles:
    Member
    Admin
permissions:
    Member:
        read Task where project.owner = $me
        read Project where owner = $me
    Admin:
        full [Project, Task]
`
	model, errs := dsl.Compile(map[string]string{"t.dsl": src})
	if len(errs) > 0 {
		b.Fatal(errs[0])
	}
	eng, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		b.Fatal(err)
	}
	admin := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Admin"}
	basis := &eventstore.Basis{Type: "human", ID: "root"}
	const owners, projPer, taskPer = 1000, 5, 20 // 1000×5×20 = 100k tasks, 100 per owner
	for o := 0; o < owners; o++ {
		owner := fmt.Sprintf("u%d", o)
		for p := 0; p < projPer; p++ {
			proj, _ := eng.Create(ctx, admin, "Project", map[string]any{"owner": owner}, basis, "")
			for t := 0; t < taskPer; t++ {
				eng.Create(ctx, admin, "Task", map[string]any{"project": proj.ID, "title": "t"}, basis, "")
			}
		}
	}
	me := eventstore.Actor{Type: eventstore.ActorHuman, ID: "u500", Role: "Member"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eng.Query(ctx, me, "Task", QueryOpts{}); err != nil {
			b.Fatal(err)
		}
	}
}
