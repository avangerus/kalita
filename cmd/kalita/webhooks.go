package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/avangerus/kalita/internal/engine"
)

// The node's webhook delivery runner (Cycle-B1 / D-011, Milestone B): turns the
// TaskWebhook intentions automation journals into real outbound POSTs, under a
// fail-closed node-level egress whitelist (KALITA_EGRESS_DOMAINS).
//
// Delivery is AT-LEAST-ONCE; the business EFFECT is made idempotent by the
// receiver using the deterministic idempotency_key this runner sends with every
// attempt (owner decision, G2). Kalita never promises exactly-once DELIVERY —
// only that a redelivery after a crash carries the same key, so a correct
// receiver collapses it to one effect (the same contract workers/lib/subscribe.py
// documents, LLD-21 §3).

// webhookDeliverFunc is the injection seam: production posts over HTTP; tests
// inject a stub. It must return a nil error only when the receiver ACKNOWLEDGED
// the request (HTTP 2xx); a non-2xx or transport error is a retryable failure.
type webhookDeliverFunc func(ctx context.Context, url string, payload []byte) error

const (
	webhookAttempts = 5
	webhookTimeout  = 30 * time.Second
)

// webhookBackoff is the base sleep between delivery retries (attempt-scaled).
// A var (not const) so the runner's own tests can shorten it — production uses
// the 2s default.
var webhookBackoff = 2 * time.Second

// parseEgressDomains parses KALITA_EGRESS_DOMAINS (comma-separated). Empty or
// unset means NO outbound webhooks are allowed (fail-closed).
func parseEgressDomains(raw string) []string {
	var out []string
	for _, d := range strings.Split(raw, ",") {
		d = normalizeHost(d)
		if d != "" {
			out = append(out, d)
		}
	}
	return out
}

// normalizeHost normalizes a host for whitelist comparison (Cycle-B1):
// lowercase, trailing dot stripped, default ports removed, empty on garbage.
func normalizeHost(hostport string) string {
	h := strings.TrimSpace(strings.ToLower(hostport))
	h = strings.TrimSuffix(h, ".")
	if h == "" {
		return ""
	}
	// strip a bare default port if present (example.com:443 == example.com)
	if u, err := url.Parse("//" + h); err == nil {
		return u.Hostname()
	}
	return h
}

// allowed reports whether a webhook URL's host is in the node's allowlist.
// Fail-closed: an unparseable URL or an empty allowlist denies everything.
func allowed(domains []string, rawURL string) bool {
	if len(domains) == 0 {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := normalizeHost(u.Hostname())
	for _, d := range domains {
		if d == host {
			return true
		}
	}
	return false
}

// deliverOnce posts payload to rawURL with a timeout; the client never follows
// a redirect (a redirect could escape the whitelist — the FIRST url is the only
// one the allowlist authorized).
func deliverOnce(ctx context.Context, rawURL string, payload []byte) error {
	client := &http.Client{
		Timeout: webhookTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // never follow — redirects bypass the whitelist
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook %s: HTTP %d", rawURL, resp.StatusCode)
	}
	return nil
}

// runWebhookDeliveries is the body of the runner tick: find open webhook tasks,
// enforce the whitelist, deliver with retries, and settle each task. It returns
// the number of tasks it processed (for logs/tests).
func runWebhookDeliveries(ctx context.Context, eng *engine.Engine, domains []string, deliver webhookDeliverFunc) int {
	tasks := eng.OpenWebhookTasks()
	processed := 0
	for _, t := range tasks {
		rawURL := strings.TrimSpace(t.Args)
		if rawURL == "" {
			// malformed intent: fail it so it never silently vanishes
			if err := eng.SettleWebhookTask(ctx, t.ID, false, "webhook task has no URL", "wh-settle:"+t.ID); err != nil {
				log.Printf("webhook: settle %s: %v", t.ID, err)
			}
			processed++
			continue
		}
		if !allowed(domains, rawURL) {
			// fail-closed: the intended domain is outside the node's allowlist
			log.Printf("webhook: %s denied — %q outside KALITA_EGRESS_DOMAINS", t.ID, rawURL)
			if err := eng.SettleWebhookTask(ctx, t.ID, false, "egress domain not in KALITA_EGRESS_DOMAINS", "wh-settle:"+t.ID); err != nil {
				log.Printf("webhook: settle %s: %v", t.ID, err)
			}
			processed++
			continue
		}

		values, _ := eng.WebhookRecordValues(t.Entity, t.RecordID)
		payload, err := json.Marshal(map[string]any{
			// the deterministic idempotency identity: the receiver collapses any
			// at-least-once redelivery of this same task to one business effect.
			"idempotency_key": "webhook:" + t.ID,
			"entity":          t.Entity,
			"record_id":       t.RecordID,
			"values":          values,
		})
		if err != nil {
			_ = eng.SettleWebhookTask(ctx, t.ID, false, "payload marshal: "+err.Error(), "wh-settle:"+t.ID)
			processed++
			continue
		}

		var lastErr error
		delivered := false
		for attempt := 1; attempt <= webhookAttempts; attempt++ {
			if err := deliver(ctx, rawURL, payload); err == nil {
				delivered = true
				break
			} else {
				lastErr = err
			}
			if attempt < webhookAttempts {
				select {
				case <-ctx.Done():
					return processed // shutting down: leave the task open for the next boot
				case <-time.After(webhookBackoff * time.Duration(attempt)):
				}
			}
		}
		if delivered {
			if err := eng.SettleWebhookTask(ctx, t.ID, true, "delivered", "wh-settle:"+t.ID); err != nil {
				log.Printf("webhook: settle %s: %v", t.ID, err)
			}
		} else {
			log.Printf("webhook: %s gave up after %d attempts: %v", t.ID, webhookAttempts, lastErr)
			if err := eng.SettleWebhookTask(ctx, t.ID, false, "delivery failed: "+lastErr.Error(), "wh-settle:"+t.ID); err != nil {
				log.Printf("webhook: settle %s: %v", t.ID, err)
			}
		}
		processed++
	}
	return processed
}
