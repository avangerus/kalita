//go:build !embedui

package webui

import "net/http"

// Embedded reports whether a renderer is baked into this binary. Without the
// embedui build tag, it is not.
func Embedded() bool { return false }

// EmbeddedHandler is nil in this build.
func EmbeddedHandler() http.Handler { return nil }
