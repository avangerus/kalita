package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestMetricsPrometheus — the node exposes Prometheus-format metrics (O5),
// public like /healthz, with the red-state counters an operator's dashboard
// reads. Assert format + the load-bearing metric names, not exact values
// (journal seq grows as other tests in the package create records).
func TestMetricsPrometheus(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain (Prometheus)", ct)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)

	for _, name := range []string{
		"kalita_journal_seq", "kalita_def_version", "kalita_pending_approvals",
		"kalita_pending_proposals", "kalita_uptime_seconds", "kalita_goroutines",
	} {
		if !strings.Contains(text, name+" ") {
			t.Fatalf("/metrics must expose %s, got:\n%s", name, text)
		}
	}
	if !strings.Contains(text, "# HELP kalita_journal_seq") || !strings.Contains(text, "# TYPE kalita_journal_seq gauge") {
		t.Fatalf("/metrics must be Prometheus text (HELP/TYPE lines), got:\n%s", text)
	}
}
