// Package webui serves the built-in renderer (a pure projection of /api/meta).
// The renderer is decoupled from the binary (ADR-004): by default the node
// serves a UI directory from disk (edit a file, refresh — no Go rebuild). The
// embedded copy is opt-in behind the `embedui` build tag for a single-file
// on-prem box.
package webui

import (
	"net/http"
	"os"
)

// DirHandler serves a UI directory from disk. Returns nil if the directory
// does not exist, so the node can fall back to embedded or API-only.
func DirHandler(dir string) http.Handler {
	if dir == "" {
		return nil
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil
	}
	return http.FileServer(http.Dir(dir))
}

// apiOnly is the fallback when there is no UI: a terse pointer, not a 404.
func APIOnly() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("kalita node — API only. Mount a UI with --ui-dir, " +
			"build with -tags embedui for the bundled renderer, or use @kalita/sdk.\n"))
	})
}
