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

// newSecureServerWithStore is newSecureServer (auth_test.go) but also hands
// back the backing store, so a test can inspect the journal directly (e.g.
// for the auth.throttled event) instead of only observing HTTP responses.
func newSecureServerWithStore(t *testing.T) (*httptest.Server, string, eventstore.Store) {
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
	srv := httptest.NewServer(Secure(New(eng, reg)))
	t.Cleanup(srv.Close)
	return srv, token, store
}

// TestRESTRateLimit: an authenticated actor gets the same per-minute budget
// on REST as MCP (LLD-10 §4: "аналогично MCP 240/min"); the request that
// crosses it gets 429 RATE_LIMITED, not a silent pass-through or a crash.
func TestRESTRateLimit(t *testing.T) {
	srv, token, _ := newSecureServerWithStore(t)
	client := srv.Client()

	var last *http.Response
	for i := 0; i < apiPerMinute+1; i++ {
		req, _ := http.NewRequest("GET", srv.URL+"/api/meta", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if i < apiPerMinute {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("request %d within budget should be 200, got %d", i, resp.StatusCode)
			}
			continue
		}
		last = resp
	}
	defer last.Body.Close()
	if last.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("request past the per-minute budget should be 429, got %d", last.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(last.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "RATE_LIMITED" {
		t.Fatalf("want code RATE_LIMITED, got %v", body["code"])
	}
}

// TestRESTAuthFailureThrottled: repeated bad bearer tokens from one caller
// journal exactly one auth.throttled event at the threshold (LLD-10 §4) — an
// auth failure is a fact recorded in the log (I-3), not a silent 401 lost to
// a bare log line.
func TestRESTAuthFailureThrottled(t *testing.T) {
	srv, _, store := newSecureServerWithStore(t)
	client := srv.Client()

	for i := 0; i < identity.AuthFailThreshold; i++ {
		req, _ := http.NewRequest("GET", srv.URL+"/api/meta", nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("bad token must be 401, got %d", resp.StatusCode)
		}
	}

	events, err := store.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range events {
		if e.Kind == eventstore.AuthThrottled {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("want exactly 1 auth.throttled event after %d failed attempts, got %d", identity.AuthFailThreshold, count)
	}
}
