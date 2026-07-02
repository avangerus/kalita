package api

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The kalita-ui buildless UI (LLD-05 PK-1) resolves preact+htm through one
// bare specifier mapped by an INLINE <script type="importmap"> in the served
// HTML. A strict CSP blocks inline scripts, so secure.go allows exactly that
// one script by SHA-256 hash. These tests keep the three facts in lockstep so
// a browser never silently fails to load the admin client under the node's
// own CSP (a failure the Go static-serving smoke test cannot observe):
//   1. the CSP hash matches the actual import-map bytes in the HTML,
//   2. the script policy stays strict (no blanket 'unsafe-inline'),
//   3. the served HTML runs NO inline script other than that import map.

var importMapRe = regexp.MustCompile(`(?s)<script type="importmap">(.*?)</script>`)

func uiFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "web", name))
	if err != nil {
		t.Fatalf("read web/%s: %v", name, err)
	}
	return string(b)
}

// TestCSPImportMapHash: the hash pinned in secure.go equals the SHA-256 of the
// exact inline import-map bytes shipped in index.html and catalog.html. Edit
// the import map without updating importMapHash and this gate fails (instead
// of the browser silently refusing to run it).
func TestCSPImportMapHash(t *testing.T) {
	for _, name := range []string{"index.html", "catalog.html"} {
		html := uiFile(t, name)
		m := importMapRe.FindStringSubmatch(html)
		if m == nil {
			t.Fatalf("web/%s: no inline <script type=\"importmap\"> found", name)
		}
		sum := sha256.Sum256([]byte(m[1]))
		got := "sha256-" + base64.StdEncoding.EncodeToString(sum[:])
		if got != importMapHash {
			t.Fatalf("web/%s: import-map hash %s does not match secure.go importMapHash %s — update the constant (and any peer HTML) so the CSP allows the served import map", name, got, importMapHash)
		}
	}
}

// TestCSPAllowsImportMapNotInlineScripts: the CSP carries the import-map hash,
// keeps an explicit strict script-src, and never opens blanket inline-script
// execution — the closed-registry invariant (nothing outside the served
// bundle runs as script).
func TestCSPAllowsImportMapNotInlineScripts(t *testing.T) {
	if !strings.Contains(contentSecurityPolicy, importMapHash) {
		t.Fatalf("CSP must allow the import-map hash: %q", contentSecurityPolicy)
	}
	if !strings.Contains(contentSecurityPolicy, "script-src ") {
		t.Fatalf("CSP must set an explicit script-src: %q", contentSecurityPolicy)
	}
	// script-src must not grant 'unsafe-inline' — that would defeat the point
	// of hashing exactly one script.
	for _, dir := range strings.Split(contentSecurityPolicy, ";") {
		dir = strings.TrimSpace(dir)
		if strings.HasPrefix(dir, "script-src") && strings.Contains(dir, "'unsafe-inline'") {
			t.Fatalf("script-src must not use 'unsafe-inline': %q", dir)
		}
	}
}

// TestServedHTMLHasNoStrayInlineScripts: every <script> in the served HTML is
// either the hashed import map or an EXTERNAL same-origin module (has a src);
// no other inline script exists to smuggle markup/logic past the registry.
func TestServedHTMLHasNoStrayInlineScripts(t *testing.T) {
	scriptOpen := regexp.MustCompile(`(?i)<script\b([^>]*)>`)
	for _, name := range []string{"index.html", "catalog.html"} {
		html := uiFile(t, name)
		for _, m := range scriptOpen.FindAllStringSubmatch(html, -1) {
			attrs := strings.ToLower(m[1])
			hasSrc := strings.Contains(attrs, "src=")
			isImportMap := strings.Contains(attrs, `type="importmap"`)
			if !hasSrc && !isImportMap {
				t.Fatalf("web/%s: inline <script%s> is neither an external (src=) module nor the hashed import map — inline logic is not allowed under the CSP (LLD-05 closed-registry invariant)", name, m[1])
			}
		}
	}
}
