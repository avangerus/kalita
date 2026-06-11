// Package webui embeds the universal client. The UI is a pure projection of
// /api/meta — it ships inside the binary so the on-prem delivery unit stays a
// single file and the client can never drift from the kernel version.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var files embed.FS

// Handler serves the embedded client.
func Handler() http.Handler {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic(err) // embed is broken at build time, not runtime
	}
	return http.FileServer(http.FS(sub))
}
