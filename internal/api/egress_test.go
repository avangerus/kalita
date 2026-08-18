package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// D-011 (English-first release) — the dead-letter queue over REST: the listing
// and the manual recovery are definition-approver-gated operational surface.

const egressAPIPack = `
entity Doc:
    title: string

roles:
    Owner
    Manager

permissions:
    Owner:
        full [Doc]
    Manager:
        read all

automation:
    on create Doc:
        webhook out "https://hooks.example.com/deliver"
`

type egressAPIFixture struct {
	srv      *httptest.Server
	eng      *engine.Engine
	ownerTok string
	mgrTok   string
}

func newEgressAPIFixture(t *testing.T) *egressAPIFixture {
	t.Helper()
	ctx := context.Background()
	model, errs := dsl.Compile(map[string]string{"e.dsl": egressAPIPack})
	if len(errs) > 0 {
		t.Fatalf("pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	ownerTok, err := reg.RegisterWithToken(ctx, registrar, "boss", eventstore.ActorHuman, "Owner", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	mgrTok, err := reg.RegisterWithToken(ctx, registrar, "staff-1", eventstore.ActorHuman, "Manager", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	t.Cleanup(srv.Close)
	return &egressAPIFixture{srv: srv, eng: eng, ownerTok: ownerTok, mgrTok: mgrTok}
}

func (f *egressAPIFixture) do(t *testing.T, method, path, token string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, f.srv.URL+path, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// deadLetterOne creates a Doc (firing the webhook automation) and settles the
// resulting delivery failed, so the DLQ has exactly one entry.
func deadLetterOne(t *testing.T, eng *engine.Engine) *engine.Task {
	t.Helper()
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "boss", Role: "Owner"}
	if _, err := eng.Create(context.Background(), owner, "Doc", map[string]any{"title": "t"},
		&eventstore.Basis{Type: "human", ID: "boss"}, ""); err != nil {
		t.Fatal(err)
	}
	tasks := eng.OpenWebhookTasks()
	if len(tasks) != 1 {
		t.Fatalf("want one open webhook task, got %d", len(tasks))
	}
	if err := eng.SettleWebhookTask(context.Background(), tasks[0].ID, false, "delivery failed", "settle-1"); err != nil {
		t.Fatal(err)
	}
	return tasks[0]
}

func TestEgressDlqAndRetryRoleGate(t *testing.T) {
	f := newEgressAPIFixture(t)
	dead := deadLetterOne(t, f.eng)

	// the listing is reserved to the definition approver
	if code, _ := f.do(t, http.MethodGet, "/api/egress/dlq", f.mgrTok); code != http.StatusForbidden {
		t.Fatalf("manager on /api/egress/dlq = %d, want 403", code)
	}
	code, body := f.do(t, http.MethodGet, "/api/egress/dlq", f.ownerTok)
	if code != http.StatusOK {
		t.Fatalf("owner on /api/egress/dlq = %d, want 200 (%v)", code, body)
	}
	if body["count"].(float64) != 1 {
		t.Fatalf("dlq count = %v, want 1", body["count"])
	}
	items := body["items"].([]any)
	item := items[0].(map[string]any)
	if item["destination"] != "https://hooks.example.com/deliver" || item["delivery_id"] != dead.DeliveryID {
		t.Fatalf("unexpected dlq item: %v", item)
	}

	// the recovery is approver-gated too
	if code, _ := f.do(t, http.MethodPost, "/api/egress/retry/"+dead.ID, f.mgrTok); code != http.StatusForbidden {
		t.Fatalf("manager retry = %d, want 403", code)
	}
	code, body = f.do(t, http.MethodPost, "/api/egress/retry/"+dead.ID, f.ownerTok)
	if code != http.StatusOK {
		t.Fatalf("owner retry = %d, want 200 (%v)", code, body)
	}
	if body["delivery_id"] != dead.DeliveryID {
		t.Fatalf("retry must carry the same delivery identity: %v", body)
	}
	if body["task_id"] == dead.ID {
		t.Fatalf("retry must be a new task: %v", body)
	}

	// unknown task ids answer 404
	code, _ = f.do(t, http.MethodPost, "/api/egress/retry/no-such-task", f.ownerTok)
	if code != http.StatusNotFound {
		t.Fatalf("unknown retry = %d, want 404", code)
	}
}
