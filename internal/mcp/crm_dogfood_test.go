package mcp

import (
	"context"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// Dogfood for the CRM showcase pack: an opportunity carries a weighted forecast
// (amount * probability / 100), the pipeline dashboard sums it, and closing a
// win is gated behind the sales manager's signature. Proves the README's "run a
// CRM on kalita" claim end-to-end through the MCP path.
func TestDogfoodCRMPack(t *testing.T) {
	ctx := context.Background()
	files := map[string]string{}
	for _, f := range []string{"pack.dsl", "crm.dsl"} {
		src, err := os.ReadFile("../../packs/crm/" + f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		files[f] = string(src)
	}
	model, errs := dsl.Compile(files)
	if len(errs) > 0 {
		t.Fatalf("crm pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	rep, _ := reg.RegisterWithToken(ctx, root, "rep-1", eventstore.ActorAgent, "SalesRep", nil, nil, nil)
	mgr, _ := reg.RegisterWithToken(ctx, root, "mgr-1", eventstore.ActorAgent, "SalesManager", nil, nil, nil)

	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()
	basis := map[string]any{"type": "human", "id": "seed"}

	// a rep opens an account and an opportunity worth 100k at 60% probability
	acc, e := call(t, srv.URL, rep, "create_record", map[string]any{
		"entity": "Account", "basis": basis, "values": map[string]any{"name": "Vector LLC", "industry": "Tech"}})
	if e {
		t.Fatalf("create account: %v", acc)
	}
	opp, e := call(t, srv.URL, rep, "create_record", map[string]any{
		"entity": "Opportunity", "basis": basis,
		"values": map[string]any{"name": "Vector platform deal", "account": acc["id"], "amount": 100000, "probability": 60}})
	if e {
		t.Fatalf("create opportunity: %v", opp)
	}
	// forecast = 100000 * 60 / 100 = 60000, computed and returned on create
	if f, _ := opp["values"].(map[string]any)["forecast"].(float64); f != 60000 {
		t.Errorf("forecast = %v, want 60000 (amount*probability/100)", opp["values"])
	}
	oppID := opp["id"].(string)

	// drive the pipeline to the closing gate
	act := func(tk, action string) map[string]any {
		res, e := call(t, srv.URL, tk, "act", map[string]any{
			"entity": "Opportunity", "id": oppID, "action": action, "basis": basis})
		if e {
			t.Fatalf("act %s: %v", action, res)
		}
		return res
	}
	act(rep, "qualify_opp")
	act(rep, "send_proposal")
	act(rep, "negotiate")
	// close_won is HITL: the rep can't do it (no act grant), the manager parks it for signature
	won := act(mgr, "close_won")
	if won["status"] != "pending_approval" {
		t.Fatalf("close_won must park for the manager's signature, got %v", won)
	}

	// the manager reads the pipeline: weighted forecast and by-stage breakdown
	dash, isErr := call(t, srv.URL, mgr, "dashboard", map[string]any{"name": "Pipeline"})
	if isErr {
		t.Fatalf("dashboard: %v", dash)
	}
	if got := tileValue(t, dash, "Weighted forecast"); got != 60000 {
		t.Errorf("Weighted forecast = %v, want 60000", got)
	}
	if got := tileValue(t, dash, "Open opportunities"); got != 1 {
		t.Errorf("Open opportunities = %v, want 1 (still in Negotiation)", got)
	}
	byStage := tileGroups(t, dash, "By stage")
	if byStage["Negotiation"] != 1 {
		t.Errorf("By stage = %v, want Negotiation:1", byStage)
	}
}
