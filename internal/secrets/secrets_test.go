package secrets

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSecretsFromFile0600: a 0600 secrets file resolves values without
// touching the environment (LLD-18 §1 acceptance test 1, file-tier only —
// the permission-refusal half is POSIX-only, see below).
func TestSecretsFromFile0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.env")
	if err := os.WriteFile(path, []byte("KALITA_PG_DSN=postgres://u:p@host/db\n# comment\n\nOTHER=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := NewLoader(path)
	if err != nil {
		t.Fatalf("0600 file must load: %v", err)
	}
	v, warn, err := l.Resolve("KALITA_PG_DSN")
	if err != nil {
		t.Fatal(err)
	}
	if v != "postgres://u:p@host/db" {
		t.Fatalf("want dsn from file, got %q", v)
	}
	if warn != "" {
		t.Fatalf("a file-sourced secret must not warn, got %q", warn)
	}
}

// TestSecretsFileWeakPermsRejected: group/world-readable secrets file refuses
// to load at all (fail-fast). POSIX permission bits only make sense on
// non-Windows — see internal/secrets/perm_windows.go for the documented gap.
func TestSecretsFileWeakPermsRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not enforceable on Windows — see perm_windows.go")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.env")
	if err := os.WriteFile(path, []byte("KALITA_PG_DSN=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLoader(path); err == nil {
		t.Fatal("world-readable secrets file must refuse to load")
	}
}

// TestSecretsDockerFileConvention: <NAME>_FILE (docker/systemd secrets) is
// read directly, independent of any secrets file / 0600 gate.
func TestSecretsDockerFileConvention(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dsn")
	if err := os.WriteFile(path, []byte("postgres://from-docker-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KALITA_TEST_DSN_FILE", path)
	l, err := NewLoader("")
	if err != nil {
		t.Fatal(err)
	}
	v, warn, err := l.Resolve("KALITA_TEST_DSN")
	if err != nil {
		t.Fatal(err)
	}
	if v != "postgres://from-docker-secret" {
		t.Fatalf("want value from docker secret file, got %q", v)
	}
	if warn != "" {
		t.Fatalf("a docker-secret-sourced value must not warn, got %q", warn)
	}
}

// TestSecretsDockerFileMissingFailsFast: an explicit _FILE pointing nowhere is
// a hard error, not a silent fallback to a weaker tier.
func TestSecretsDockerFileMissingFailsFast(t *testing.T) {
	t.Setenv("KALITA_TEST_DSN_FILE", filepath.Join(t.TempDir(), "does-not-exist"))
	l, err := NewLoader("")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.Resolve("KALITA_TEST_DSN"); err == nil {
		t.Fatal("a misconfigured _FILE path must fail, not silently fall through")
	}
}

// TestSecretsEnvFallbackWarns: falling back to a bare env var is allowed but
// must surface a "not for prod" warning (LLD-18 §1).
func TestSecretsEnvFallbackWarns(t *testing.T) {
	t.Setenv("KALITA_TEST_DSN", "postgres://from-env")
	l, err := NewLoader("")
	if err != nil {
		t.Fatal(err)
	}
	v, warn, err := l.Resolve("KALITA_TEST_DSN")
	if err != nil {
		t.Fatal(err)
	}
	if v != "postgres://from-env" {
		t.Fatalf("want env value, got %q", v)
	}
	if warn == "" {
		t.Fatal("plain env fallback must warn — not for prod")
	}
}

// TestSecretsMissingEverywhere: no file, no docker secret, no env — empty,
// no error (caller decides whether the secret is required).
func TestSecretsMissingEverywhere(t *testing.T) {
	l, err := NewLoader("")
	if err != nil {
		t.Fatal(err)
	}
	v, warn, err := l.Resolve("KALITA_TEST_TOTALLY_ABSENT")
	if err != nil {
		t.Fatal(err)
	}
	if v != "" || warn != "" {
		t.Fatalf("want empty value and no warning, got v=%q warn=%q", v, warn)
	}
}
