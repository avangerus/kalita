package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// P0 security: dev headers are dead by default, tokens are the identity,
// cross-origin browser requests are rejected.

func newSecureServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"t.dsl": "entity Doc:\n    title: string required\n\nroles:\n    Owner\n\npermissions:\n    Owner:\n        full [Doc]\n"})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	token, err := reg.RegisterWithToken(context.Background(), registrar, "mike", eventstore.ActorHuman, "Owner", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(context.Background(), model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Secure(New(eng, reg))) // NO dev headers
	t.Cleanup(srv.Close)
	return srv, token
}

func TestDevHeadersDeadByDefault(t *testing.T) {
	srv, _ := newSecureServer(t)
	req, _ := http.NewRequest("GET", srv.URL+"/api/meta", nil)
	req.Header.Set("X-Actor-Id", "intruder")
	req.Header.Set("X-Actor-Role", "Owner")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("dev headers must not authenticate without opt-in, got %d", resp.StatusCode)
	}
}

func TestTokenAuth(t *testing.T) {
	srv, token := newSecureServer(t)
	req, _ := http.NewRequest("GET", srv.URL+"/api/meta", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token must authenticate, got %d", resp.StatusCode)
	}
	// security headers present
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatal("security headers must be set")
	}
	// wrong token
	req2, _ := http.NewRequest("GET", srv.URL+"/api/meta", nil)
	req2.Header.Set("Authorization", "Bearer wrong")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad token must be rejected, got %d", resp2.StatusCode)
	}
}

func TestCrossOriginRejected(t *testing.T) {
	srv, token := newSecureServer(t)
	req, _ := http.NewRequest("POST", srv.URL+"/api/records/Doc", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", "https://evil.example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin must be 403, got %d", resp.StatusCode)
	}
	// same-origin passes the origin gate (will fail later on body, not on origin)
	req2, _ := http.NewRequest("GET", srv.URL+"/api/meta", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Origin", srv.URL)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("same-origin must pass, got %d", resp2.StatusCode)
	}
}
