package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// LLD-05 §4 test 5: TestScaffoldGeneratesCompose — `kalita product scaffold
// knowvault` creates a valid compose + status + manifest skeleton. Exercises
// scaffoldProduct directly (the function main()'s "product scaffold" dispatch
// calls) rather than exec-ing the built binary.
func TestScaffoldGeneratesCompose(t *testing.T) {
	repoRoot := repoRootForTest(t)
	packDir := filepath.Join(repoRoot, "packs", "knowvault")
	if st, err := os.Stat(packDir); err != nil || !st.IsDir() {
		t.Skipf("packs/knowvault not found at %s (repo layout changed?): %v", packDir, err)
	}
	out := t.TempDir()

	if err := scaffoldProduct("knowvault", packDir, out); err != nil {
		t.Fatalf("scaffoldProduct: %v", err)
	}

	for name := range scaffoldTemplates {
		p := filepath.Join(out, name)
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("expected %s to be generated: %v", name, err)
		}
		if st.Size() == 0 {
			t.Fatalf("%s must not be empty", name)
		}
	}

	compose, err := os.ReadFile(filepath.Join(out, "docker-compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	composeStr := string(compose)
	for _, want := range []string{
		"name: knowvault",
		"services:",
		"postgres:",
		"node:",
		"--pack=/opt/kalita/",
		"knowvault", // pack name substituted throughout
	} {
		if !strings.Contains(composeStr, want) {
			t.Errorf("docker-compose.yml missing %q:\n%s", want, composeStr)
		}
	}
	// docker-compose ${VAR:-default} interpolation syntax must survive the Go
	// text/template render untouched — a common Go-template/compose-syntax
	// collision (both use braces) would corrupt this into garbage.
	if !strings.Contains(composeStr, "${POSTGRES_PASSWORD:-knowvault}") {
		t.Errorf("docker-compose.yml must preserve compose-syntax var interpolation, got:\n%s", composeStr)
	}
	if strings.Contains(composeStr, "{{") || strings.Contains(composeStr, "}}") {
		t.Errorf("docker-compose.yml must not contain unrendered template delimiters:\n%s", composeStr)
	}

	status, err := os.ReadFile(filepath.Join(out, "status.html"))
	if err != nil {
		t.Fatal(err)
	}
	statusStr := string(status)
	if !strings.Contains(statusStr, "/healthz") || !strings.Contains(statusStr, "/readyz") {
		t.Errorf("status.html must probe /healthz and /readyz, got:\n%s", statusStr)
	}

	manifest, err := os.ReadFile(filepath.Join(out, "product-manifest.dsl.skeleton"))
	if err != nil {
		t.Fatal(err)
	}
	manifestStr := string(manifest)
	for _, want := range []string{"product:", `name "knowvault"`, "auth password"} {
		if !strings.Contains(manifestStr, want) {
			t.Errorf("product-manifest.dsl.skeleton missing %q:\n%s", want, manifestStr)
		}
	}
}

// TestScaffoldRejectsMissingPackDir: scaffolding without a real pack behind
// it fails loudly (I-3 — no silent success on a bad --pack-dir).
func TestScaffoldRejectsMissingPackDir(t *testing.T) {
	out := t.TempDir()
	if err := scaffoldProduct("ghost", filepath.Join(out, "no-such-pack"), filepath.Join(out, "deploy")); err == nil {
		t.Fatal("expected an error for a missing pack directory")
	}
}

// repoRootForTest walks up from the test's working directory (cmd/kalita) to
// the repo root, where packs/ and deploy/ live.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..")
}
