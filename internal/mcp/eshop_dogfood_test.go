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

// Dogfood for the e-shop showcase pack: a master-detail order whose total rolls
// up the computed line totals of its lines (an aggregate OVER a computed field),
// plus HITL on refunds. Proves "run an online store on kalita".
func TestDogfoodEshopPack(t *testing.T) {
	ctx := context.Background()
	files := map[string]string{}
	for _, f := range []string{"pack.dsl", "eshop.dsl"} {
		src, err := os.ReadFile("../../packs/eshop/" + f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		files[f] = string(src)
	}
	model, errs := dsl.Compile(files)
	if len(errs) > 0 {
		t.Fatalf("eshop pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	clerk, _ := reg.RegisterWithToken(ctx, root, "clerk-1", eventstore.ActorAgent, "Clerk", nil, nil, nil)
	mgr, _ := reg.RegisterWithToken(ctx, root, "mgr-1", eventstore.ActorAgent, "StoreManager", nil, nil, nil)

	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()
	basis := map[string]any{"type": "human", "id": "seed"}
	mk := func(entity string, vals map[string]any) map[string]any {
		rec, e := call(t, srv.URL, clerk, "create_record", map[string]any{"entity": entity, "basis": basis, "values": vals})
		if e {
			t.Fatalf("create %s: %v", entity, rec)
		}
		return rec
	}

	p1 := mk("Product", map[string]any{"sku": "TS-1", "name": "T-shirt", "price": 1000, "in_stock": 50, "published": true})
	p2 := mk("Product", map[string]any{"sku": "MUG-1", "name": "Mug", "price": 500, "in_stock": 100, "published": true})
	cust := mk("Customer", map[string]any{"name": "Acme Inc", "email": "buy@acme.test"})
	ord := mk("Order", map[string]any{"customer": cust["id"]})
	ordID := ord["id"].(string)
	mk("OrderLine", map[string]any{"order": ordID, "product": p1["id"], "qty": 2, "unit_price": 1000}) // 2000
	mk("OrderLine", map[string]any{"order": ordID, "product": p2["id"], "qty": 1, "unit_price": 500})  // 500

	// the order total rolls up the two computed line totals: 2*1000 + 1*500 = 2500
	got, e := call(t, srv.URL, clerk, "get_record", map[string]any{"entity": "Order", "id": ordID})
	if e {
		t.Fatalf("get order: %v", got)
	}
	gv := got["values"].(map[string]any)
	if total, _ := gv["total"].(float64); total != 2500 {
		t.Errorf("order total = %v, want 2500 (sum of computed line totals)", gv["total"])
	}
	if lc, _ := gv["line_count"].(float64); lc != 2 {
		t.Errorf("line_count = %v, want 2", gv["line_count"])
	}

	// drive fulfilment; a refund on a paid order is HITL
	act := func(tk, action string) map[string]any {
		res, e := call(t, srv.URL, tk, "act", map[string]any{"entity": "Order", "id": ordID, "action": action, "basis": basis})
		if e {
			t.Fatalf("act %s: %v", action, res)
		}
		return res
	}
	act(clerk, "place")
	act(clerk, "pay")
	ref := act(mgr, "refund")
	if ref["status"] != "pending_approval" {
		t.Fatalf("refund must park for the manager's signature, got %v", ref)
	}
}
