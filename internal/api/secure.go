package api

import (
	"net/http"
	"strings"
)

// importMapHash is the SHA-256 (base64) of the exact inline import-map script
// bytes in web/index.html and web/catalog.html (LLD-05 PK-1). The kalita-ui
// modules import preact+htm through a single bare specifier ("kalita-vendor")
// so they resolve identically off disk, via --ui-dir, and in the embedded
// build; that mapping lives in an INLINE <script type="importmap">, which a
// strict CSP would otherwise block. We allow exactly this one script by hash
// — NOT `'unsafe-inline'` — so an attacker-injected inline script (different
// bytes → different hash) stays blocked. TestCSPImportMapHash locks this
// constant to the actual file bytes: change the import map and the gate fails
// until this hash is updated.
const importMapHash = "sha256-mbpAbMGb6L7t2iqKuS8n+a6145fuLoaq3TQkL8+pYxM="

// contentSecurityPolicy is the node's CSP. script-src stays strict: same-origin
// external modules (the /app.js and /catalog.js renderers) plus the one hashed
// import map — no blanket inline-script execution (the closed-registry
// invariant: nothing outside the served bundle produces or runs markup).
var contentSecurityPolicy = strings.Join([]string{
	"default-src 'self'",
	"script-src 'self' '" + importMapHash + "'",
	"style-src 'self' 'unsafe-inline'",
	"img-src 'self' data:",
}, "; ")

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
		h.Set("Content-Security-Policy", contentSecurityPolicy)
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
