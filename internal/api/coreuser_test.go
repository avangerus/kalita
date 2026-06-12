package api

import (
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

// core.User is the built-in people directory backed by the identity registry:
// ref[core.User] pickers query it and labels resolve through it, with no User
// table in the pack.
func TestCoreUserDirectory(t *testing.T) {
	ctx := context.Background()
	model, errs := dsl.Compile(map[string]string{})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	for _, a := range []struct{ id, role string }{{"alice", "Supervisor"}, {"bob", "OperatorL2"}, {"carol", "Admin"}} {
		if _, err := reg.RegisterWithToken(ctx, root, a.id, eventstore.ActorHuman, a.role, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg, WithDevHeaders()))
	defer srv.Close()

	get := func(path string) map[string]any {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Header.Set("X-Actor-Id", "alice")
		req.Header.Set("X-Actor-Role", "Supervisor")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return out
	}

	// list everyone
	all := get("/api/records/core.User")["records"].([]any)
	if len(all) != 3 {
		t.Fatalf("directory should list 3 actors, got %d", len(all))
	}
	// search narrows by id/role
	one := get("/api/records/core.User?search=bob")["records"].([]any)
	if len(one) != 1 || one[0].(map[string]any)["values"].(map[string]any)["id"] != "bob" {
		t.Fatalf("search=bob should return bob, got %v", one)
	}
	byRole := get("/api/records/core.User?search=admin")["records"].([]any)
	if len(byRole) != 1 || byRole[0].(map[string]any)["values"].(map[string]any)["id"] != "carol" {
		t.Fatalf("search by role should find carol, got %v", byRole)
	}
	// fetch one by id resolves a label-bearing record
	rec := get("/api/records/core.User/carol")
	if rec["values"].(map[string]any)["role"] != "Admin" {
		t.Fatalf("get carol should carry role Admin, got %v", rec)
	}
}
