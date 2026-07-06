package engine

import (
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// LLD-09 §2/§4 test 4: a 7-day journal fixture must fold to correct counters,
// and a role missing rate/hours must be reported without hours×cost rather
// than crashing.

const reportTestPack = `
entity Ticket:
    title: string required
    status: enum[Open, Closed] default=Open

workflow Ticket on status:
    Open -> Closed: close

roles:
    Owner rate 2000 hours 0.5
    Agent agent rate 500
    NoRate agent

permissions:
    Owner:
        full [Ticket]
        act [close]
    Agent:
        read [Ticket]
        create [Ticket]
        deny [update Ticket.*, delete *]
    NoRate:
        read [Ticket]
        create [Ticket]
        deny [update Ticket.*, delete *]
`

func TestRoleRateHoursParse(t *testing.T) {
	model, errs := dsl.Compile(map[string]string{"r.dsl": reportTestPack})
	if len(errs) > 0 {
		t.Fatalf("must compile: %v", errs[0])
	}
	owner := model.Roles["Owner"]
	if !owner.HasRate || owner.Rate != 2000 || !owner.HasHours || owner.Hours != 0.5 {
		t.Fatalf("Owner must parse rate=2000 hours=0.5, got %+v", owner)
	}
	agent := model.Roles["Agent"]
	if !agent.IsAgent || !agent.HasRate || agent.Rate != 500 || agent.HasHours {
		t.Fatalf("Agent must parse agent+rate=500, no hours, got %+v", agent)
	}
	noRate := model.Roles["NoRate"]
	if noRate.HasRate || noRate.HasHours {
		t.Fatalf("NoRate must declare neither, got %+v", noRate)
	}
}

func TestReportFromJournal(t *testing.T) {
	model, errs := dsl.Compile(map[string]string{"r.dsl": reportTestPack})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	base := time.Date(2026, 6, 29, 9, 0, 0, 0, time.UTC) // start of the 7d window
	clock := base
	// the event TIMESTAMP comes from the STORE's clock (MemStore.now), not the
	// engine's WithClock (which only drives things like serial-number years
	// and business-calendar math) — both are wired to the same closure so
	// BuildReport's since/now filtering sees the times this test controls.
	store := eventstore.NewMemStore(func() time.Time { return clock })
	e, err := New(ctx, model, store, WithClock(func() time.Time { return clock }))
	if err != nil {
		t.Fatal(err)
	}

	ownerActor := eventstore.Actor{Type: eventstore.ActorHuman, ID: "own", Role: "Owner"}
	agentActor := eventstore.Actor{Type: eventstore.ActorAgent, ID: "ag", Role: "Agent"}

	// two agent creates, one human create, one human act — all inside the window
	clock = base.Add(1 * time.Hour)
	if _, err := e.Create(ctx, agentActor, "Ticket", map[string]any{"title": "a"}, basis, ""); err != nil {
		t.Fatal(err)
	}
	clock = base.Add(2 * time.Hour)
	if _, err := e.Create(ctx, agentActor, "Ticket", map[string]any{"title": "b"}, basis, ""); err != nil {
		t.Fatal(err)
	}
	clock = base.Add(3 * time.Hour)
	t3, err := e.Create(ctx, ownerActor, "Ticket", map[string]any{"title": "c"}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	clock = base.Add(4 * time.Hour)
	if _, err := e.Act(ctx, ownerActor, "Ticket", t3.ID, "close", basis, ""); err != nil {
		t.Fatal(err)
	}

	// one event well BEFORE the since cutoff — must not be counted
	clock = base.Add(-48 * time.Hour)
	if _, err := e.Create(ctx, agentActor, "Ticket", map[string]any{"title": "old"}, basis, ""); err != nil {
		t.Fatal(err)
	}

	now := base.Add(5 * time.Hour)
	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rep := BuildReport(events, model.Roles, base, now, "")

	if rep.Actions.Total != 4 {
		t.Fatalf("want 4 actions in window (excluding the pre-window create), got %d (%+v)", rep.Actions.Total, rep.Actions)
	}
	if rep.Actions.Agent != 2 || rep.Actions.Human != 2 {
		t.Fatalf("want 2 agent + 2 human, got %+v", rep.Actions)
	}

	var ownerStats, agentStats *RoleStats
	for i := range rep.ByRole {
		switch rep.ByRole[i].Role {
		case "Owner":
			ownerStats = &rep.ByRole[i]
		case "Agent":
			agentStats = &rep.ByRole[i]
		}
	}
	if ownerStats == nil || ownerStats.Actions != 2 {
		t.Fatalf("Owner must show 2 actions (create+close), got %+v", ownerStats)
	}
	if !ownerStats.HasCost || ownerStats.Cost != 2*0.5*2000 {
		t.Fatalf("Owner cost must be actions(2)*hours(0.5)*rate(2000)=2000, got %+v", ownerStats)
	}
	// Agent declares `rate` but not `hours`: hours×cost must be omitted with a
	// note, not fabricated or crashed on (LLD-09 §2/§4 test 4).
	if agentStats == nil || agentStats.Actions != 2 {
		t.Fatalf("Agent must show 2 actions, got %+v", agentStats)
	}
	if agentStats.HasCost || agentStats.HasHours || agentStats.Note == "" {
		t.Fatalf("Agent has no `hours` declared: cost must be omitted with a note, got %+v", agentStats)
	}

	// render must not panic and must mention both roles
	md := rep.RenderMarkdown()
	if md == "" {
		t.Fatal("markdown render must not be empty")
	}
	htmlOut := rep.RenderHTML()
	if htmlOut == "" {
		t.Fatal("html render must not be empty")
	}
}

// Golden slice of test 6: BuildReport on a v0-style event slice (no shadow /
// mutation events — indeed no events naming any of them) with roles == nil
// must not panic and must produce empty sections, not garbage.
func TestReportOnEmptyOrUnknownPack(t *testing.T) {
	now := time.Now()
	since := now.Add(-7 * 24 * time.Hour)
	rep := BuildReport(nil, nil, since, now, "")
	if rep.Actions.Total != 0 || len(rep.ByRole) != 0 || rep.Approvals.Pending != 0 {
		t.Fatalf("empty journal must yield an empty report, got %+v", rep)
	}
	if rep.PackKnown {
		t.Fatal("roles=nil must mark PackKnown=false")
	}
	if rep.RenderMarkdown() == "" || rep.RenderHTML() == "" {
		t.Fatal("render must not panic/empty on an empty report")
	}
}
