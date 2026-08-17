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

// Cycle-A2 (D-014) regressions: the identity admin boundary. The v0 "any
// human is admin" model allowed any staff member to reset anyone's password,
// disable anyone, and invite themself into any role (SEC-1 impersonation
// class). Now: self-or-admin for passwords, admin-only for disable, and
// admin-role invites reserved to the admin role.

type boundaryFixture struct {
	srv   *httptest.Server
	store eventstore.Store
	admin string // Owner token
	staff string // Manager token
	staff2 string
	agent string
}

func boundarySetup(t *testing.T) *boundaryFixture {
	t.Helper()
	ctx := context.Background()
	model, errs := dsl.Compile(map[string]string{})
	if len(errs) > 0 {
		t.Fatalf("model must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	adminTok, err := reg.RegisterWithToken(ctx, registrar, "boss", eventstore.ActorHuman, "Owner", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	staffTok, err := reg.RegisterWithToken(ctx, registrar, "staff-1", eventstore.ActorHuman, "Manager", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	staff2Tok, err := reg.RegisterWithToken(ctx, registrar, "staff-2", eventstore.ActorHuman, "Manager", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	agentTok, err := reg.RegisterWithToken(ctx, registrar, "bot-1", eventstore.ActorAgent, "APChecker", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	t.Cleanup(srv.Close)
	return &boundaryFixture{srv: srv, store: store, admin: adminTok, staff: staffTok, staff2: staff2Tok, agent: agentTok}
}

func (f *boundaryFixture) do(t *testing.T, method, path, token string, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, f.srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (f *boundaryFixture) login(t *testing.T, id, password string) int {
	t.Helper()
	resp, err := http.Post(f.srv.URL+"/api/login", "application/json",
		strings.NewReader(`{"login":"`+id+`","password":"`+password+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func countKinds(t *testing.T, store eventstore.Store, kind eventstore.Kind) int {
	t.Helper()
	events, err := store.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ev := range events {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

// TestSetPasswordSelfOrAdmin: a human sets ONLY their own password; the node
// admin may reset anyone's. A cross-actor attempt by non-admin is 403 and
// journals nothing — the victim's old password keeps working.
func TestSetPasswordSelfOrAdmin(t *testing.T) {
	f := boundarySetup(t)

	// self-service: staff-1 sets their own password
	code, _ := f.do(t, "POST", "/api/actors/staff-1/password", f.staff, `{"password":"self chosen secret 1"}`)
	if code != http.StatusOK {
		t.Fatalf("self setPassword must be 200, got %d", code)
	}
	if got := f.login(t, "staff-1", "self chosen secret 1"); got != http.StatusOK {
		t.Fatalf("login with self-set password must work, got %d", got)
	}

	// cross-actor by another staff member: 403, nothing journaled, password unchanged
	before := countKinds(t, f.store, eventstore.ActorPasswordSet)
	code, out := f.do(t, "POST", "/api/actors/staff-1/password", f.staff2, `{"password":"hijack attempt 123"}`)
	if code != http.StatusForbidden {
		t.Fatalf("cross-actor setPassword must be 403, got %d: %v", code, out)
	}
	if out["rule"] == nil || !strings.Contains(out["rule"].(string), "self-or-admin") {
		t.Fatalf("403 must carry the boundary rule, got %v", out)
	}
	if after := countKinds(t, f.store, eventstore.ActorPasswordSet); after != before {
		t.Fatalf("a refused setPassword must journal nothing: %d -> %d", before, after)
	}
	if got := f.login(t, "staff-1", "self chosen secret 1"); got != http.StatusOK {
		t.Fatalf("victim's password must be unchanged after the refused attempt, login got %d", got)
	}

	// admin reset: allowed
	code, _ = f.do(t, "POST", "/api/actors/staff-1/password", f.admin, `{"password":"admin reset secret 1"}`)
	if code != http.StatusOK {
		t.Fatalf("admin reset must be 200, got %d", code)
	}
	if got := f.login(t, "staff-1", "admin reset secret 1"); got != http.StatusOK {
		t.Fatalf("login with admin-reset password must work, got %d", got)
	}

	// agents can never set passwords (human-only gate)
	code, _ = f.do(t, "POST", "/api/actors/staff-1/password", f.agent, `{"password":"bot hijack 123456"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("agent setPassword must be 401, got %d", code)
	}
}

// TestDisableActorAdminOnly: disabling is a node-admin action; a refused
// disable leaves the victim's token working, a real one kills it.
func TestDisableActorAdminOnly(t *testing.T) {
	f := boundarySetup(t)

	code, _ := f.do(t, "POST", "/api/actors/staff-1/disable", f.staff2, "")
	if code != http.StatusForbidden {
		t.Fatalf("non-admin disable must be 403, got %d", code)
	}
	// victim's token still works (auth check on a trivial authenticated call)
	if got, _ := f.do(t, "GET", "/api/meta", f.staff, ""); got != http.StatusOK {
		t.Fatalf("victim's token must still authenticate after refused disable, got %d", got)
	}

	code, _ = f.do(t, "POST", "/api/actors/staff-1/disable", f.admin, "")
	if code != http.StatusOK {
		t.Fatalf("admin disable must be 200, got %d", code)
	}
	if got, _ := f.do(t, "GET", "/api/meta", f.staff, ""); got != http.StatusUnauthorized {
		t.Fatalf("disabled actor's token must stop working, got %d", got)
	}
}

// TestInviteAdminRoleReserved: only the admin role may invite the admin role;
// staff→staff/customer invites stay open (portal flow); a refused invite
// journals nothing.
func TestInviteAdminRoleReserved(t *testing.T) {
	f := boundarySetup(t)

	// staff cannot invite the admin role
	before := countKinds(t, f.store, eventstore.InviteCreated)
	code, out := f.do(t, "POST", "/api/invites", f.staff, `{"role":"Owner"}`)
	if code != http.StatusForbidden {
		t.Fatalf("non-admin invite of admin role must be 403, got %d: %v", code, out)
	}
	if after := countKinds(t, f.store, eventstore.InviteCreated); after != before {
		t.Fatalf("a refused invite must journal nothing: %d -> %d", before, after)
	}

	// staff can still invite a normal role (portal: Manager -> Client)
	code, inv := f.do(t, "POST", "/api/invites", f.staff, `{"role":"Client"}`)
	if code != http.StatusCreated || inv["invite_code"] == "" {
		t.Fatalf("staff invite of a non-admin role must work (portal flow), got %d %v", code, inv)
	}

	// the admin can invite the admin role
	code, inv = f.do(t, "POST", "/api/invites", f.admin, `{"role":"Owner"}`)
	if code != http.StatusCreated || inv["invite_code"] == "" {
		t.Fatalf("admin invite of the admin role must work, got %d %v", code, inv)
	}

	// agents cannot issue invites (human-only gate)
	code, _ = f.do(t, "POST", "/api/invites", f.agent, `{"role":"Client"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("agent invite must be 401, got %d", code)
	}
}

// TestRevokeCredentialSelfOrAdmin: a human may revoke only their OWN WebAuthn
// credential; the node admin may revoke anyone's. A cross-actor attempt by a
// non-admin is 403 (D-014 — the v0 "any human revokes any credential" was the
// same anti-pattern as setPassword/disable/invite).
func TestRevokeCredentialSelfOrAdmin(t *testing.T) {
	f := boundarySetup(t)

	// self-revocation: open to the actor (no actor_id → targets the caller)
	if code, _ := f.do(t, "DELETE", "/api/credentials/device-1", f.staff, `{}`); code != http.StatusOK {
		t.Fatalf("self revoke must be 200, got %d", code)
	}

	// cross-actor by a non-admin: 403
	code, out := f.do(t, "DELETE", "/api/credentials/device-1", f.staff2, `{"actor_id":"staff-1"}`)
	if code != http.StatusForbidden {
		t.Fatalf("cross-actor revoke by non-admin must be 403, got %d: %v", code, out)
	}

	// cross-actor by the node admin: allowed (boundary passed → 200)
	if code, _ := f.do(t, "DELETE", "/api/credentials/device-1", f.admin, `{"actor_id":"staff-1"}`); code != http.StatusOK {
		t.Fatalf("admin revoke must pass the boundary, got %d", code)
	}
}
