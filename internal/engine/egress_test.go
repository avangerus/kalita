package engine

import (
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// LLD-09 §3/§4 test 5: a declared-domain call reconciles clean; a call to an
// undeclared domain is flagged an anomaly.

const egressTestPack = `
entity Ticket:
    title: string required

event EgressCalled:
    domain: string required
    purpose: string
    ref: string

roles:
    Worker agent

permissions:
    Worker:
        read [Ticket]
        emit [EgressCalled]
        deny [update Ticket.*, delete *, create *]
`

func egressEngine(t *testing.T) (*Engine, eventstore.Store, time.Time, func(time.Time)) {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"e.dsl": egressTestPack})
	if len(errs) > 0 {
		t.Fatalf("egress test pack must compile: %v", errs[0])
	}
	base := time.Date(2026, 6, 29, 9, 0, 0, 0, time.UTC)
	clock := base
	store := eventstore.NewMemStore(func() time.Time { return clock })
	e, err := New(ctx, model, store, WithClock(func() time.Time { return clock }))
	if err != nil {
		t.Fatal(err)
	}
	return e, store, base, func(t time.Time) { clock = t }
}

func TestEgressReportReconciles(t *testing.T) {
	e, store, base, setClock := egressEngine(t)
	worker := eventstore.Actor{Type: eventstore.ActorAgent, ID: "worker-1", Role: "Worker"}

	setClock(base.Add(1 * time.Hour))
	if _, err := e.EmitEvent(ctx, worker, "EgressCalled",
		map[string]any{"domain": "api.openai.com", "purpose": "draft"}, ""); err != nil {
		t.Fatal(err)
	}
	setClock(base.Add(2 * time.Hour))
	if _, err := e.EmitEvent(ctx, worker, "EgressCalled",
		map[string]any{"domain": "evil.example.com", "purpose": "?"}, ""); err != nil {
		t.Fatal(err)
	}

	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	whitelists := map[string][]string{"worker-1": {"api.openai.com", "smtp.sendgrid.net"}}
	rep := BuildEgressReport(events, whitelists, base, base.Add(3*time.Hour))

	if len(rep.Calls) != 2 {
		t.Fatalf("want 2 observed calls, got %d", len(rep.Calls))
	}
	if rep.Anomalies != 1 {
		t.Fatalf("want exactly 1 anomaly, got %d (%+v)", rep.Anomalies, rep.Calls)
	}
	var declaredCall, anomalousCall *EgressCall
	for i := range rep.Calls {
		if rep.Calls[i].Domain == "api.openai.com" {
			declaredCall = &rep.Calls[i]
		}
		if rep.Calls[i].Domain == "evil.example.com" {
			anomalousCall = &rep.Calls[i]
		}
	}
	if declaredCall == nil || declaredCall.Anomaly {
		t.Fatalf("declared domain must reconcile clean, got %+v", declaredCall)
	}
	if anomalousCall == nil || !anomalousCall.Anomaly || anomalousCall.Reason == "" {
		t.Fatalf("undeclared domain must be flagged with a reason, got %+v", anomalousCall)
	}

	// an undeclared WORKER (no whitelist entry at all): every call is an anomaly
	rep2 := BuildEgressReport(events, nil, base, base.Add(3*time.Hour))
	if rep2.Anomalies != 2 {
		t.Fatalf("with no whitelists at all, every call must be an anomaly, got %d", rep2.Anomalies)
	}

	if rep.RenderMarkdown() == "" || rep.RenderHTML() == "" {
		t.Fatal("render must not be empty")
	}
}

// Golden slice of test 6: an empty/v0 journal (no egress.called events at
// all) must not panic and must produce an empty report.
func TestEgressReportOnEmptyJournal(t *testing.T) {
	now := time.Now()
	since := now.Add(-7 * 24 * time.Hour)
	rep := BuildEgressReport(nil, nil, since, now)
	if len(rep.Calls) != 0 || rep.Anomalies != 0 {
		t.Fatalf("empty journal must yield an empty egress report, got %+v", rep)
	}
	if rep.RenderMarkdown() == "" || rep.RenderHTML() == "" {
		t.Fatal("render must not panic/empty on an empty report")
	}
}
