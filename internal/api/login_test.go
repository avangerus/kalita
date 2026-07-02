package api

import (
	"bytes"
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

// LLD-05 §4 test 2: TestPasswordLogin — actor.password_set journals only the
// bcrypt hash (I-4); POST /api/login trades login+password for a working
// bearer token of the SAME actor; a wrong password is rejected with 401.
func TestPasswordLogin(t *testing.T) {
	ctx := context.Background()
	model, errs := dsl.Compile(map[string]string{})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	if _, err := reg.RegisterWithToken(ctx, root, "mike", eventstore.ActorHuman, "Owner", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetPassword(ctx, root, "mike", "correct horse battery staple", &eventstore.Basis{Type: "human", ID: "root"}); err != nil {
		t.Fatal(err)
	}

	// the journal must carry only the bcrypt hash — never the raw password.
	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.Kind != eventstore.ActorPasswordSet {
			continue
		}
		found = true
		if strings.Contains(string(e.Payload), "correct horse battery staple") {
			t.Fatalf("journal must not contain the raw password: %s", e.Payload)
		}
		var p struct {
			BcryptHash []byte `json:"bcrypt_hash"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if len(p.BcryptHash) == 0 || !bytes.HasPrefix(p.BcryptHash, []byte("$2")) {
			t.Fatalf("payload must carry a bcrypt hash, got %v", p.BcryptHash)
		}
	}
	if !found {
		t.Fatal("actor.password_set must be journaled")
	}

	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()

	post := func(body string) (*http.Response, map[string]any) {
		resp, err := http.Post(srv.URL+"/api/login", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp, out
	}

	// correct password -> a working bearer token
	resp, out := post(`{"login":"mike","password":"correct horse battery staple"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login must succeed, got %d: %v", resp.StatusCode, out)
	}
	token, _ := out["token"].(string)
	if token == "" {
		t.Fatal("login must return a token")
	}
	req, _ := http.NewRequest("GET", srv.URL+"/api/meta", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	metaResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer metaResp.Body.Close()
	if metaResp.StatusCode != http.StatusOK {
		t.Fatalf("token from /api/login must authenticate, got %d", metaResp.StatusCode)
	}

	// wrong password -> 401
	resp2, _ := post(`{"login":"mike","password":"wrong password entirely"}`)
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password must be 401, got %d", resp2.StatusCode)
	}

	// unknown login -> 401 (same shape — no enumeration oracle)
	resp3, _ := post(`{"login":"ghost","password":"whatever12345"}`)
	if resp3.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unknown login must be 401, got %d", resp3.StatusCode)
	}
}

// TestPasswordLoginWithoutPasswordSet: an actor that never set a password
// cannot log in via /api/login (no password credential, no fallback guess).
func TestPasswordLoginWithoutPasswordSet(t *testing.T) {
	ctx := context.Background()
	model, errs := dsl.Compile(map[string]string{})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	if _, err := reg.RegisterWithToken(ctx, root, "nopass", eventstore.ActorHuman, "Owner", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/api/login", "application/json", strings.NewReader(`{"login":"nopass","password":"anything12345"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login without a password set must be 401, got %d", resp.StatusCode)
	}
}

// TestSetPasswordViaAPI: an authenticated human can set a password for an
// actor through POST /api/actors/{id}/password, then log in with it.
func TestSetPasswordViaAPI(t *testing.T) {
	ctx := context.Background()
	model, errs := dsl.Compile(map[string]string{})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	token0, err := reg.RegisterWithToken(ctx, root, "mike", eventstore.ActorHuman, "Owner", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/api/actors/mike/password", strings.NewReader(`{"password":"a fresh password 123"}`))
	req.Header.Set("Authorization", "Bearer "+token0)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set password must succeed, got %d", resp.StatusCode)
	}

	loginResp, err := http.Post(srv.URL+"/api/login", "application/json", strings.NewReader(`{"login":"mike","password":"a fresh password 123"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login with the freshly set password must succeed, got %d", loginResp.StatusCode)
	}
}
