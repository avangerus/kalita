package api

import (
	"net/http"
	"testing"
)

// K-BUG-06: liveness/readiness endpoints for orchestrators, public (no auth).
func TestHealthzReadyz(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	c := &apiClient{t, srv.URL, "", ""} // no auth — these must be public
	if resp, body := c.do("GET", "/healthz", nil); resp.StatusCode != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("/healthz = %d %v, want 200 {status:ok}", resp.StatusCode, body)
	}
	if resp, body := c.do("GET", "/readyz", nil); resp.StatusCode != http.StatusOK || body["status"] != "ready" {
		t.Fatalf("/readyz = %d %v, want 200 {status:ready}", resp.StatusCode, body)
	}
}
