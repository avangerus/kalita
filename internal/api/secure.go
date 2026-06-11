package api

import (
	"net/http"
	"strings"
)

// Secure wraps the node's whole handler with the P0 protections: same-origin
// enforcement for browser requests and standard security headers. Agents and
// curl (no Origin header) pass through; a browser page from another origin
// does not.
func Secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			if !sameOrigin(origin, r.Host) {
				http.Error(w, `{"code":"FORBIDDEN_ORIGIN","message":"cross-origin requests are not allowed"}`, http.StatusForbidden)
				return
			}
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy",
			"default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(origin, host string) bool {
	rest, ok := strings.CutPrefix(origin, "http://")
	if !ok {
		rest, ok = strings.CutPrefix(origin, "https://")
	}
	return ok && rest == host
}
