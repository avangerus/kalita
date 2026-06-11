package main

import (
	"context"
	"fmt"
	"log"

	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// seedDemo turns an empty node into a ten-minute wow: demo humans and an
// agent registered, sample records created, ready tokens printed. Refuses to
// touch a journal that already has actors — demo mode cannot pollute a real
// node.
func seedDemo(ctx context.Context, eng *engine.Engine, reg *identity.Registry, store eventstore.Store) {
	existing, err := reg.List(ctx)
	if err != nil {
		log.Fatalf("demo: %v", err)
	}
	if len(existing) > 0 {
		log.Print("demo: journal already has actors — skipping seed, tokens were printed at first start")
		return
	}
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "demo-seed", Role: "Owner"}
	basis := &eventstore.Basis{Type: "human", ID: "demo-seed"}

	model := eng.Model()
	fmt.Println("\n──────────────── DEMO NODE ────────────────")
	fmt.Println("tokens (paste into the web UI at /, or use as MCP bearer):")
	for role := range model.Roles {
		if model.Roles[role].IsAgent {
			continue
		}
		id := "demo-" + role
		token, err := reg.RegisterWithToken(ctx, registrar, id, eventstore.ActorHuman, role, nil,
			&identity.ActorMeta{Description: "demo user"}, basis)
		if err != nil {
			continue
		}
		fmt.Printf("  %-22s %s\n", id+" ("+role+"):", token)
	}
	for role := range model.Roles {
		if !model.Roles[role].IsAgent {
			continue
		}
		id := "demo-agent-" + role
		token, err := reg.RegisterWithToken(ctx, registrar, id, eventstore.ActorAgent, role, nil,
			&identity.ActorMeta{Model: "your-agent-here", Description: "demo agent slot"}, basis)
		if err != nil {
			continue
		}
		fmt.Printf("  %-22s %s\n", id+" (agent):", token)
	}

	// sample data for the collections pack (the default demo experience);
	// other packs start clean — their tokens still work
	if _, ok := model.Entities["Debtor"]; ok {
		anna := eventstore.Actor{Type: eventstore.ActorHuman, ID: "demo-Accountant", Role: "Accountant"}
		if c, err := eng.Create(ctx, anna, "Contract", map[string]any{
			"company": "Vector LLC", "due_date": "2026-05-01", "amount": 100000.0}, basis, ""); err == nil {
			_, _ = eng.Create(ctx, anna, "Debtor", map[string]any{
				"company": "Vector LLC", "contract": c.ID, "debt": 100000.0}, basis, "")
		}
		if c2, err := eng.Create(ctx, anna, "Contract", map[string]any{
			"company": "Horizon Ltd", "due_date": "2026-06-20", "amount": 50000.0}, basis, ""); err == nil {
			_, _ = eng.Create(ctx, anna, "Debtor", map[string]any{
				"company": "Horizon Ltd", "contract": c2.ID, "debt": 50000.0}, basis, "")
		}
		fmt.Println("\nseeded: 2 contracts, 2 debtors (one already overdue — watch the workflow)")
	}
	fmt.Println("───────────────────────────────────────────")
}
