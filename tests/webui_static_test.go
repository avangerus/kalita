// TestWebUIStaticServing and TestWebUIBuildless are the LLD-05 PK-1 §4 item-4
// automated smoke test: "each file opens without a build step". No JS test
// runner exists in this repo (buildless, no npm — ADR-004), so the gate is a
// Go test that (a) serves web/ + packages/ over httptest exactly the way
// `kalita serve --ui-dir ./web` mounts them (see cmd/kalita/main.go's
// "/packages/" mount, added for this same LLD) and asserts every UI file
// involved in PR-1/PR-2 is reachable with a sane content-type, and (b)
// statically greps the new/changed .js files for bundler-only patterns that
// would silently break "open without a build step": require(), dynamic
// import(), and relative ESM specifiers missing their .js extension (Node
// and browsers require the explicit extension for relative imports — a bare
// specifier like "kalita-vendor" or "react" is fine, those resolve via an
// import map / npm respectively).
package tests

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestWebUIStaticServing mirrors cmd/kalita/main.go's disk-serving topology:
// "/" -> web/ (http.Dir), "/packages/" -> packages/ (http.Dir, stripped
// prefix) — see that file's comment for why packages/ needs its own mount
// (it is a SIBLING of web/, not a subdirectory, so web/'s FileServer alone
// cannot reach it).
func TestWebUIStaticServing(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "web"))
	if err != nil {
		t.Fatalf("resolve web/ dir: %v", err)
	}
	pkgRoot, err := filepath.Abs(filepath.Join("..", "packages"))
	if err != nil {
		t.Fatalf("resolve packages/ dir: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(root)))
	mux.Handle("/packages/", http.StripPrefix("/packages/", http.FileServer(http.Dir(pkgRoot))))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cases := []struct {
		path        string
		contentType string // prefix match
	}{
		{"/index.html", "text/html"},
		{"/app.js", "text/javascript"},
		{"/catalog.html", "text/html"},
		{"/catalog.js", "text/javascript"},
		{"/catalog-fixtures.js", "text/javascript"},
		{"/vendor/preact-standalone.module.js", "text/javascript"},
		{"/packages/kalita-ui/tokens.css", "text/css"},
		{"/packages/kalita-ui/atoms.js", "text/javascript"},
		{"/packages/kalita-ui/molecules.js", "text/javascript"},
		{"/packages/kalita-ui/organisms.js", "text/javascript"},
		{"/packages/kalita-ui/faces.js", "text/javascript"},
		{"/packages/kalita-ui/hooks.js", "text/javascript"},
		{"/packages/kalita-ui/tokens/format.js", "text/javascript"},
		{"/packages/kalita-sdk/components.js", "text/javascript"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.path, func(t *testing.T) {
			resp, err := http.Get(srv.URL + c.path)
			if err != nil {
				t.Fatalf("GET %s: %v", c.path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s: status %d, want 200", c.path, resp.StatusCode)
			}
			ct := resp.Header.Get("Content-Type")
			// .js content-type varies by Go version's mime.TypeByExtension
			// ("text/javascript" on modern Go, "application/javascript" on
			// older) — accept either JS MIME for .js paths.
			if strings.HasSuffix(c.path, ".js") {
				if !strings.HasPrefix(ct, "text/javascript") && !strings.HasPrefix(ct, "application/javascript") {
					t.Fatalf("GET %s: content-type %q, want a JS MIME type", c.path, ct)
				}
				return
			}
			if !strings.HasPrefix(ct, c.contentType) {
				t.Fatalf("GET %s: content-type %q, want prefix %q", c.path, ct, c.contentType)
			}
		})
	}
}

// jsFiles: every .js file this LLD-05 implementation created or changed —
// the buildless-import-hygiene gate below runs against exactly this list
// (not a repo-wide walk), since older/unrelated JS is out of scope here.
func jsFiles(t *testing.T) []string {
	t.Helper()
	rel := []string{
		filepath.Join("..", "web", "app.js"),
		filepath.Join("..", "web", "catalog.js"),
		filepath.Join("..", "web", "catalog-fixtures.js"),
		filepath.Join("..", "packages", "kalita-ui", "atoms.js"),
		filepath.Join("..", "packages", "kalita-ui", "molecules.js"),
		filepath.Join("..", "packages", "kalita-ui", "organisms.js"),
		filepath.Join("..", "packages", "kalita-ui", "faces.js"),
		filepath.Join("..", "packages", "kalita-ui", "hooks.js"),
		filepath.Join("..", "packages", "kalita-ui", "tokens", "format.js"),
		filepath.Join("..", "packages", "kalita-sdk", "components.js"),
	}
	for _, p := range rel {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected LLD-05 file missing: %s (%v)", p, err)
		}
	}
	return rel
}

var (
	importLineRe  = regexp.MustCompile(`(?m)^\s*import\s+.*?\bfrom\s+['"]([^'"]+)['"]`)
	requireCallRe = regexp.MustCompile(`\brequire\s*\(`)
	dynImportRe   = regexp.MustCompile(`\bimport\s*\(`)
)

// TestWebUIBuildless is the "opens without a build step" gate: no CommonJS
// require(), no dynamic import(), and every relative/absolute ESM import
// specifier carries an explicit .js extension (bare specifiers — resolved
// via <script type="importmap"> or, for @kalita/sdk's react.js, npm — are
// exempt, since those are not filesystem paths).
func TestWebUIBuildless(t *testing.T) {
	for _, path := range jsFiles(t) {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			src := string(raw)

			if loc := requireCallRe.FindStringIndex(src); loc != nil {
				t.Fatalf("%s: found require(...) at byte %d — CommonJS is not buildless ESM (ADR-004)", path, loc[0])
			}
			if loc := dynImportRe.FindStringIndex(src); loc != nil {
				t.Fatalf("%s: found dynamic import(...) at byte %d — not used by this LLD's design, flags a bundler-oriented pattern", path, loc[0])
			}

			for _, m := range importLineRe.FindAllStringSubmatch(src, -1) {
				spec := m[1]
				isPathLike := strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/")
				if isPathLike && !strings.HasSuffix(spec, ".js") {
					t.Fatalf("%s: relative import %q is missing its .js extension — required for bare ESM resolution in browsers/Node (no bundler here, ADR-004)", path, spec)
				}
			}
		})
	}
}
