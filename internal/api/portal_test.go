package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// The customer-portal entry (PORTAL-VISION §1): an admin invites a customer,
// the customer self-registers via the ONLY public endpoint, gets a token,
// and sees exactly their own records — row-level `where user = $me`.

const portalPack = `
entity Customer:
    name: string required
    user: string

entity Order:
    customer: ref[Customer] on_delete=restrict
    item: string required
    status: enum[New, Shipped, Done] default=New

roles:
    Manager
    Client

permissions:
    Manager:
        full [Customer, Order]
    Client:
        read Customer where user = $me
        read Order
        deny [delete *, update Customer.*, update Order.*]
`

func TestCustomerPortalSelfRegistration(t *testing.T) {
	model, errs := dsl.Compile(map[string]string{"portal.kal": portalPack})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	ctx := context.Background()
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	managerToken, err := reg.RegisterWithToken(ctx, registrar, "manager", eventstore.ActorHuman, "Manager", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Secure(New(eng, reg)))
	defer srv.Close()

	post := func(path, token string, body any) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", srv.URL+path, bytes.NewReader(raw))
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
	get := func(path, token string) map[string]any {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}
	basis := map[string]any{"type": "human", "id": "manager"}

	// manager sets up two customers
	_, vector := post("/api/records/Customer", managerToken, map[string]any{
		"values": map[string]any{"name": "Vector LLC"}, "basis": basis})
	_, other := post("/api/records/Customer", managerToken, map[string]any{
		"values": map[string]any{"name": "Secret Corp"}, "basis": basis})

	// manager issues an invite bound to Vector
	code, inv := post("/api/invites", managerToken, map[string]any{
		"role": "Client", "entity": "Customer", "record_id": vector["id"]})
	if code != http.StatusCreated || inv["invite_code"] == "" {
		t.Fatalf("invite: %d %v", code, inv)
	}

	// anonymous customer redeems — the only public endpoint
	code, redeemed := post("/api/register", "", map[string]any{
		"invite": inv["invite_code"], "id": "ivan@vector.ru"})
	if code != http.StatusCreated || redeemed["token"] == "" || redeemed["role"] != "Client" {
		t.Fatalf("register: %d %v", code, redeemed)
	}
	if redeemed["bound"] == nil {
		t.Fatalf("record binding must apply: %v", redeemed)
	}
	clientToken := redeemed["token"].(string)

	// second redemption of the same code fails
	code, _ = post("/api/register", "", map[string]any{"invite": inv["invite_code"], "id": "eve@evil.com"})
	if code != http.StatusForbidden {
		t.Fatalf("invite must be single-use, got %d", code)
	}

	// the client sees ONLY their own customer record
	rows := get("/api/records/Customer", clientToken)["records"].([]any)
	if len(rows) != 1 {
		t.Fatalf("client must see exactly their record, got %d", len(rows))
	}
	rec := rows[0].(map[string]any)
	if rec["id"] != vector["id"] || rec["values"].(map[string]any)["user"] != "ivan@vector.ru" {
		t.Fatalf("wrong record bound: %v", rec)
	}
	_ = other

	// and cannot touch anything
	code, _ = post("/api/records/Customer", clientToken, map[string]any{
		"values": map[string]any{"name": "Hacked"}, "basis": map[string]any{"type": "human", "id": "ivan@vector.ru"}})
	if code != http.StatusForbidden {
		t.Fatalf("client create must be denied, got %d", code)
	}
}
