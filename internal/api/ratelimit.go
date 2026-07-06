package api

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/avangerus/kalita/internal/engine"
)

// Rate limiting for the REST surface (LLD-10 §4): the same token-bucket-per-
// window shape as internal/mcp/server.go's allowRate (240/min), reused here
// rather than reinvented — an authenticated actor gets the same budget
// whether it calls in over MCP or REST.
const apiPerMinute = 240

type rateWindow struct {
	count int
	reset time.Time
}

// rateLimiter is embedded in Server; kept as its own small type so the lock it
// needs (hot path: every request) stays separate from any other Server state.
type rateLimiter struct {
	mu      sync.Mutex
	windows map[string]*rateWindow
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if rl.windows == nil {
		rl.windows = map[string]*rateWindow{}
	}
	now := time.Now()
	w := rl.windows[key]
	if w == nil || now.After(w.reset) {
		rl.windows[key] = &rateWindow{count: 1, reset: now.Add(time.Minute)}
		return true
	}
	w.count++
	return w.count <= apiPerMinute
}

// rateLimited checks the per-actor REST budget for an already-authenticated
// request. Unauthenticated requests are not actor-rate-limited here — a bad
// or missing token is handled by the auth-fail throttle (authfail.go) instead,
// which exists precisely because that path must not get an actor-keyed budget
// (there is no actor yet to key on).
func (s *Server) rateLimited(actorID string) bool {
	return !s.limiter.allow(actorID)
}

func writeRateLimited(w http.ResponseWriter) {
	writeJSON(w, http.StatusTooManyRequests, &engine.Err{
		Code: "RATE_LIMITED", Message: "rate limit exceeded",
		FixHint: "retry after a minute",
	})
}

// clientIP extracts the caller's address for auth-fail throttling
// (LLD-10 §4: IP-rate-limit on the auth entry point). Best-effort: falls back
// to the raw RemoteAddr if it isn't a host:port pair (e.g. under some test
// transports), rather than dropping the signal entirely.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
