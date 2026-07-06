package api

// These two gates close a CLASS of "white screen in production" defects: the
// buildless UI loads only if a set of static paths stays in sync across four
// places — the import map in web/*.html, the /packages/ mount in
// cmd/kalita/main.go, the COPY lines in the repo Dockerfile, and the --ui-dir
// root. A mismatch makes the browser's module graph fail to load, yet the Go
// static-serving smoke test (which reads files off disk, not through a
// browser's module resolver) sees nothing wrong. Two real P0s slipped through
// this exact blind spot (import-map specifier resolving to null; Dockerfile
// not copying packages/). Each is now a deterministic gate, not a code review
// habit.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestImportMapSpecifiersResolvable: every address in a served import map must
// be a browser-resolvable URL. Per the import-maps spec, a relative address
// that does not start with "/", "./", or "../" (and is not an absolute URL)
// resolves to NULL — the mapping silently dies and every module importing that
// bare specifier fails to load. TestCSPImportMapHash only checks the hash
// matches the bytes; it cannot tell whether those bytes resolve in a browser.
func TestImportMapSpecifiersResolvable(t *testing.T) {
	for _, name := range []string{"index.html", "catalog.html"} {
		html := uiFile(t, name)
		m := importMapRe.FindStringSubmatch(html)
		if m == nil {
			t.Fatalf("web/%s: no inline <script type=\"importmap\"> found", name)
		}
		var parsed struct {
			Imports map[string]string `json:"imports"`
		}
		if err := json.Unmarshal([]byte(m[1]), &parsed); err != nil {
			t.Fatalf("web/%s: import map is not valid JSON: %v", name, err)
		}
		if len(parsed.Imports) == 0 {
			t.Fatalf("web/%s: import map has no imports", name)
		}
		for spec, addr := range parsed.Imports {
			if !resolvableAddress(addr) {
				t.Fatalf("web/%s: import-map specifier %q -> %q resolves to NULL in a browser "+
					"(an import-map address must start with \"/\", \"./\", \"../\", or be an absolute URL) "+
					"— the mapping dies silently and every module importing %q white-screens the app",
					name, spec, addr, spec)
			}
		}
	}
}

func resolvableAddress(addr string) bool {
	switch {
	case strings.HasPrefix(addr, "/"),
		strings.HasPrefix(addr, "./"),
		strings.HasPrefix(addr, "../"),
		strings.HasPrefix(addr, "http://"),
		strings.HasPrefix(addr, "https://"):
		return true
	}
	return false
}

// TestShippedStaticDirsAreCopied: every directory the node serves as front-end
// static content must actually be in the release image. The node serves two
// such trees — the --ui-dir root (web/) and each sibling dir mounted under its
// own prefix in main.go via webui.DirHandler("<literal>") (packages/). If the
// Dockerfile does not COPY one of them, the browser gets 404s (or the SPA
// catch-all's index.html with the wrong content-type) for those modules and
// the app white-screens — exactly the packages/ P0. This derives the mount
// list from main.go so a NEW static mount added without a matching COPY fails
// here instead of in a customer's browser.
func TestShippedStaticDirsAreCopied(t *testing.T) {
	dockerfile := readRepoFile(t, "Dockerfile")
	mainGo := readRepoFile(t, filepath.Join("cmd", "kalita", "main.go"))

	need := map[string]bool{}

	// (1) the --ui-dir root shipped in the image's CMD, e.g. --ui-dir=/opt/kalita/web
	// the flag and its value may be joined by "=", whitespace, or (in an exec-form
	// JSON CMD array) the `", "` element separator — accept any of them.
	if mm := regexp.MustCompile(`--ui-dir["\s=,]+/opt/kalita/([A-Za-z0-9_.-]+)`).FindStringSubmatch(dockerfile); mm != nil {
		need[mm[1]] = true
	} else {
		t.Fatal("Dockerfile CMD does not pass --ui-dir=/opt/kalita/<dir>; adjust this gate if the UI serving mode changed")
	}

	// (2) sibling dirs mounted under their own prefix: webui.DirHandler("<literal>")
	for _, mm := range regexp.MustCompile(`DirHandler\("([^"]+)"\)`).FindAllStringSubmatch(mainGo, -1) {
		need[mm[1]] = true
	}

	if len(need) < 2 {
		t.Fatalf("expected at least the ui-dir root and one sibling mount; derived only %v — the extraction regexes likely drifted from main.go", keys(need))
	}

	for dir := range need {
		// A COPY that lands the dir under /opt/kalita/<dir> (the WORKDIR the node
		// resolves relative paths against). Accept "COPY <dir> /opt/kalita/<dir>".
		re := regexp.MustCompile(`(?m)^COPY\s+` + regexp.QuoteMeta(dir) + `\s+/opt/kalita/` + regexp.QuoteMeta(dir) + `\b`)
		if !re.MatchString(dockerfile) {
			t.Fatalf("the node serves %q as front-end static (from --ui-dir or a DirHandler mount in main.go) "+
				"but the Dockerfile has no `COPY %s /opt/kalita/%s` — in the image every request under that path "+
				"404s and the UI white-screens", dir, dir, dir)
		}
	}
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
