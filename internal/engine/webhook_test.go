package engine

import (
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Cycle-B1 (D-011) — the webhook delivery task lifecycle: `webhook out` in
// automation journals a TaskWebhook; the node runner settles it directly (it is
// never role-scoped/taken), and the settlement folds into egress-report.

const webhookTestPack = `
entity Doc:
    title: string

roles:
    Owner

permissions:
    Owner:
        full [Doc]

automation:
    on create Doc:
        webhook out "https://example.com/hook"
`

func webhookEngine(t *testing.T) (*Engine, eventstore.Store) {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"w.dsl": webhookTestPack})
	if len(errs) > 0 {
		t.Fatalf("webhook pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	e, err := New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	return e, store
}

// TestWebhookTaskLifecycle: an on-create automation journals a TaskWebhook;
// OpenWebhookTasks sees it; SettleWebhookTask settles an OPEN task (the finishTask
// taken-by gate does not apply) and a double settle is a conflict.
func TestWebhookTaskLifecycle(t *testing.T) {
	e, _ := webhookEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}

	if _, err := e.Create(ctx, owner, "Doc", map[string]any{"title": "t"}, basis, ""); err != nil {
		t.Fatal(err)
	}
	tasks := e.OpenWebhookTasks()
	if len(tasks) != 1 {
		t.Fatalf("on-create webhook must journal one open TaskWebhook, got %d", len(tasks))
	}
	if tasks[0].Args != "https://example.com/hook" || tasks[0].Kind != TaskWebhook {
		t.Fatalf("unexpected webhook task: %+v", tasks[0])
	}

	if err := e.SettleWebhookTask(ctx, tasks[0].ID, true, "delivered", "idem-1"); err != nil {
		t.Fatalf("settle an OPEN webhook task must work: %v", err)
	}
	if got := e.OpenWebhookTasks(); len(got) != 0 {
		t.Fatalf("settled task must leave the open queue, got %d", len(got))
	}
	// decisions are final
	if err := e.SettleWebhookTask(ctx, tasks[0].ID, true, "again", "idem-2"); err == nil {
		t.Fatal("double settle must conflict")
	}
}

// TestWebhookDeliveryInEgressReport: a settled webhook delivery folds into
// BuildEgressReport as the node runner's egress — in-allowlist is NOT an
// anomaly, and the domain comes from the task's URL.
func TestWebhookDeliveryInEgressReport(t *testing.T) {
	e, store := webhookEngine(t)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	if _, err := e.Create(ctx, owner, "Doc", map[string]any{"title": "t"}, basis, ""); err != nil {
		t.Fatal(err)
	}
	task := e.OpenWebhookTasks()[0]
	if err := e.SettleWebhookTask(ctx, task.ID, true, "delivered", "idem-1"); err != nil {
		t.Fatal(err)
	}

	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	rep := BuildEgressReport(events, map[string][]string{WebhookRunnerActorID: {"example.com"}}, now.Add(-time.Hour), now)
	if len(rep.Calls) != 1 {
		t.Fatalf("want 1 egress call, got %d", len(rep.Calls))
	}
	c := rep.Calls[0]
	if c.Worker != WebhookRunnerActorID || c.Domain != "example.com" || c.Anomaly {
		t.Fatalf("in-allowlist delivery must not be an anomaly: %+v", c)
	}

	// an allowlist WITHOUT example.com flags it
	rep2 := BuildEgressReport(events, map[string][]string{WebhookRunnerActorID: {"other.com"}}, now.Add(-time.Hour), now)
	if len(rep2.Calls) != 1 || !rep2.Calls[0].Anomaly || rep2.Anomalies != 1 {
		t.Fatalf("out-of-allowlist delivery must be an anomaly: %+v", rep2)
	}
}
