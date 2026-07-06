package main

import "testing"

// TestResolveDSNPlainEnvFallback: resolveDSN's success path (no
// KALITA_SECRETS_FILE set) falls back to the bare KALITA_PG_DSN env var —
// the existing docker-compose dev flow (KALITA_PG_DSN set directly in
// environment:) must keep working unchanged (LLD-18 §1: env is tier 3, not
// removed). The failure branches of resolveDSN call log.Fatal (matching
// every other DSN-resolution call site in this package, e.g. registerActor)
// and so are not exercised here — killing the test process is not a
// reasonable thing for a unit test to trigger; that behavior is covered at
// the internal/secrets level instead (secrets_test.go), which resolveDSN is
// a thin, untested-by-design wrapper over.
func TestResolveDSNPlainEnvFallback(t *testing.T) {
	t.Setenv("KALITA_SECRETS_FILE", "")
	t.Setenv("KALITA_PG_DSN_FILE", "")
	t.Setenv("KALITA_PG_DSN", "postgres://from-env/db")
	if got := resolveDSN(); got != "postgres://from-env/db" {
		t.Fatalf("want dsn from plain env fallback, got %q", got)
	}
}
