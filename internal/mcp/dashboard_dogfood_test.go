package mcp

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// Dogfood for the `dashboard` block (gap Б6): an agent builds a CRM pack with a
// sales-funnel dashboard over MCP, drives deals through their workflow, then
// reads the funnel back — and a row-scoped Rep sees totals over ONLY their own
// deals, proving aggregates respect ABAC (no "see all totals" leak).
const crmPack = `pack crm
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1

entity Deal:
    title:  string required
    amount: money
    stage:  enum[Lead, Qualified, Won, Lost] default=Lead
    owner:  ref[core.User] default=$me

workflow Deal on stage:
    Lead      -> Qualified: qualify
    Qualified -> Won:       win
    Qualified -> Lost:      lose
    any       -> Lead:      reopen

roles:
    Manager
    Rep

permissions:
    Manager:
        full [Deal]
        act  [qualify, win, lose, reopen]
    Rep:
        read Deal where owner = $me
        create [Deal]
        act    [qualify, win, lose]

dashboard SalesFunnel "Sales funnel":
    tile "Open deals":   count Deal where stage != Won and stage != Lost
    tile "Pipeline":     sum amount Deal where stage != Lost
    tile "By stage":     count Deal group by stage
    tile "Avg won deal": avg amount Deal where stage = Won
`

func TestDogfoodSalesFunnelDashboard(t *testing.T) {
	ctx := context.Background()
	model, errs := dsl.Compile(map[string]string{})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	mgrTok, err := reg.RegisterWithToken(ctx, registrar, "mgr-1", eventstore.ActorAgent, "Manager", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	repTok, err := reg.RegisterWithToken(ctx, registrar, "rep-1", eventstore.ActorAgent, "Rep", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "mike", Role: "Owner"}
	humanBasis := &eventstore.Basis{Type: "human", ID: "mike"}
	basis := map[string]any{"type": "human", "id": "mike"}

	// author + propose the CRM pack against the live def_version
	sys, _ := call(t, srv.URL, mgrTok, "describe_system", map[string]any{})
	v := call2ok(t, srv.URL, mgrTok, "validate_dsl", map[string]any{"files": map[string]string{"crm.dsl": crmPack}})
	if v["ok"] != true {
		t.Fatalf("crm pack must validate: %v", v)
	}
	prop, isErr := call(t, srv.URL, mgrTok, "propose_change", map[string]any{
		"files": map[string]string{"crm.dsl": crmPack}, "base_def_version": sys["def_version"],
		"description": "crm with sales funnel", "basis": basis})
	if isErr || prop["ok"] != true {
		t.Fatalf("propose: %v", prop)
	}
	if _, err := eng.DecideProposal(ctx, owner, prop["proposal_id"].(string), true, nil, humanBasis); err != nil {
		t.Fatalf("sign: %v", err)
	}

	// manager seeds deals and drives them through the funnel
	mk := func(title string, amount float64) string {
		rec, e := call(t, srv.URL, mgrTok, "create_record", map[string]any{
			"entity": "Deal", "basis": basis, "values": map[string]any{"title": title, "amount": amount}})
		if e {
			t.Fatalf("create %s: %v", title, rec)
		}
		return rec["id"].(string)
	}
	act := func(tok, id, action string) {
		res, e := call(t, srv.URL, tok, "act", map[string]any{
			"entity": "Deal", "id": id, "action": action, "basis": basis})
		if e || res["status"] != "applied" {
			t.Fatalf("act %s on %s: %v", action, id, res)
		}
	}
	mk("D1 lead", 1000)            // stays Lead
	d2 := mk("D2 qualified", 2000) // -> Qualified
	act(mgrTok, d2, "qualify")
	d3 := mk("D3 won", 5000) // -> Won
	act(mgrTok, d3, "qualify")
	act(mgrTok, d3, "win")
	d4 := mk("D4 lost", 500) // -> Lost
	act(mgrTok, d4, "qualify")
	act(mgrTok, d4, "lose")

	// list_dashboards finds it
	ld, _ := call(t, srv.URL, mgrTok, "list_dashboards", map[string]any{})
	if len(ld["dashboards"].([]any)) != 1 {
		t.Fatalf("one dashboard expected: %v", ld)
	}

	// manager reads the full funnel
	dash, isErr := call(t, srv.URL, mgrTok, "dashboard", map[string]any{"name": "SalesFunnel"})
	if isErr {
		t.Fatalf("dashboard: %v", dash)
	}
	if got := tileValue(t, dash, "Open deals"); got != 2 {
		t.Errorf("Open deals = %v, want 2 (Lead + Qualified)", got)
	}
	if got := tileValue(t, dash, "Pipeline"); got != 8000 {
		t.Errorf("Pipeline = %v, want 8000 (1000+2000+5000, Lost excluded)", got)
	}
	if got := tileValue(t, dash, "Avg won deal"); got != 5000 {
		t.Errorf("Avg won deal = %v, want 5000", got)
	}
	byStage := tileGroups(t, dash, "By stage")
	for _, want := range []struct {
		k string
		v float64
	}{{"Lead", 1}, {"Qualified", 1}, {"Won", 1}, {"Lost", 1}} {
		if byStage[want.k] != want.v {
			t.Errorf("By stage[%s] = %v, want %v (full: %v)", want.k, byStage[want.k], want.v, byStage)
		}
	}

	// ABAC safety: a Rep adds one deal and sees totals over ONLY their own rows
	rd, e := call(t, srv.URL, repTok, "create_record", map[string]any{
		"entity": "Deal", "basis": basis, "values": map[string]any{"title": "R1", "amount": 9999}})
	if e {
		t.Fatalf("rep create: %v", rd)
	}
	repDash, _ := call(t, srv.URL, repTok, "dashboard", map[string]any{"name": "SalesFunnel"})
	if got := tileValue(t, repDash, "Pipeline"); got != 9999 {
		t.Errorf("rep Pipeline = %v, want 9999 (only the rep's own deal, not the manager's 8000)", got)
	}
	repByStage := tileGroups(t, repDash, "By stage")
	if len(repByStage) != 1 || repByStage["Lead"] != 1 {
		t.Errorf("rep By stage = %v, want only {Lead:1}", repByStage)
	}

	// and the manager now sees the rep's deal folded into the totals
	mgrDash2, _ := call(t, srv.URL, mgrTok, "dashboard", map[string]any{"name": "SalesFunnel"})
	if got := tileValue(t, mgrDash2, "Open deals"); got != 3 {
		t.Errorf("manager Open deals = %v, want 3 (now incl. the rep's Lead)", got)
	}
}

// --- tiny result readers ------------------------------------------------------

func call2ok(t *testing.T, url, tok, tool string, args map[string]any) map[string]any {
	t.Helper()
	out, _ := call(t, url, tok, tool, args)
	return out
}

func findTile(t *testing.T, dash map[string]any, label string) map[string]any {
	t.Helper()
	for _, ti := range dash["tiles"].([]any) {
		m := ti.(map[string]any)
		if m["label"] == label {
			return m
		}
	}
	t.Fatalf("tile %q not found in %v", label, dash)
	return nil
}

func tileValue(t *testing.T, dash map[string]any, label string) float64 {
	return findTile(t, dash, label)["value"].(float64)
}

func tileGroups(t *testing.T, dash map[string]any, label string) map[string]float64 {
	out := map[string]float64{}
	g, ok := findTile(t, dash, label)["groups"].([]any)
	if !ok {
		return out
	}
	for _, gv := range g {
		m := gv.(map[string]any)
		out[m["key"].(string)] = m["value"].(float64)
	}
	return out
}
