//go:build embedui

// Build with: go build -tags embedui ./cmd/kalita
// Produces a single-file box: the renderer travels inside the binary.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:embedded
var files embed.FS

func Embedded() bool { return true }

func EmbeddedHandler() http.Handler {
	sub, err := fs.Sub(files, "embedded")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
