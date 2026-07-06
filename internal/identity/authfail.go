package identity

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/avangerus/kalita/internal/eventstore"
)

// AuthFailThreshold is the number of failed authentications from one IP
// within one window before auth.throttled fires (LLD-10 §4). A node-level
// parameter (not DSL, I-4) — deliberately not configurable per pack.
const AuthFailThreshold = 20

const authFailWindowLen = time.Minute

// authFailWindow tracks one IP's failure count for the current window, plus
// whether this window already produced its one auth.throttled event —
// crossing the threshold must journal exactly once per window, not once per
// subsequent failed attempt (that repeat would itself be the log-flood DoS
// this guards against, T6).
type authFailWindow struct {
	count   int
	reset   time.Time
	flagged bool
}

// authThrottledPayload is the auth.throttled payload (LLD-10 §4, fixed
// shape): {ip, window, count}.
type authThrottledPayload struct {
	IP            string `json:"ip"`
	WindowSeconds int    `json:"window_seconds"`
	Count         int    `json:"count"`
}

// authFailState holds the per-IP windows, guarded by its own mutex — separate
// from Registry.mu (the actor-directory lock) since this is an unrelated,
// much hotter/higher-cardinality path (every failed request, from any IP,
// registered or not) and must never block actor lookups or vice versa.
type authFailState struct {
	mu      sync.Mutex
	windows map[string]*authFailWindow
}

// RecordAuthFailure registers one failed authentication attempt from ip and,
// on crossing AuthFailThreshold within the current window, journals a single
// auth.throttled event (LLD-10 §4). Safe to call from concurrent requests.
// ip may be empty (unresolvable remote address) — still tracked, just as one
// bucket shared by all such callers.
func (r *Registry) RecordAuthFailure(ctx context.Context, ip string) {
	r.authFails.mu.Lock()
	if r.authFails.windows == nil {
		r.authFails.windows = map[string]*authFailWindow{}
	}
	now := time.Now()
	w := r.authFails.windows[ip]
	if w == nil || now.After(w.reset) {
		w = &authFailWindow{reset: now.Add(authFailWindowLen)}
		r.authFails.windows[ip] = w
	}
	w.count++
	fire := w.count >= AuthFailThreshold && !w.flagged
	if fire {
		w.flagged = true
	}
	count := w.count
	r.authFails.mu.Unlock()

	if !fire {
		return
	}
	payload, err := json.Marshal(authThrottledPayload{IP: ip, WindowSeconds: int(authFailWindowLen / time.Second), Count: count})
	if err != nil {
		return
	}
	// Best-effort: a failure to journal the throttle event must not itself
	// block/crash the request path that discovered it.
	_, _ = r.store.Append(ctx, eventstore.AppendInput{
		Actor:   eventstore.Actor{Type: eventstore.ActorSystem, ID: "node"},
		Kind:    eventstore.AuthThrottled,
		Payload: payload,
		Basis:   &eventstore.Basis{Type: "rule", ID: "auth-throttle"},
		SchemaV: eventstore.CurrentSchemaV,
	})
}
