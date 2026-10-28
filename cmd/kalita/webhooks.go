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

// The node's webhook delivery runner (Cycle-B1 / D-011, English-first release
// hardening): turns the TaskWebhook intentions automation journals into real
// outbound POSTs, under a fail-closed node-level egress whitelist
// (KALITA_EGRESS_DOMAINS).
//
// Delivery contract (documented honestly, not promised silently):
//   - AT-LEAST-ONCE delivery: the runner may redeliver after a crash between
//     send and settle, and DLQ-recovery redelivers on purpose;
//   - IDEMPOTENT business effect: every attempt carries the task's
//     per-firing delivery identity (DeliveryID) as `idempotency_key`, stable
//     across redeliveries and manual retries, so a correct receiver collapses
//     them to one business effect;
//   - exactly-once is NEVER promised;
//   - the payload carries the record's values AS OF DELIVERY TIME (the intent
//     journals entity+record, not a values snapshot): a record that changed
//     between firing and delivery is delivered with its newer values under
//     the firing's identity — receivers must not treat the payload as the
//     firing-time state.
//
// Business semantics stay in the DSL (`webhook out` creates the intent); this
// file is pure delivery infrastructure.

// webhookDeliveryError is the typed delivery outcome the runner classifies.
// status==0 means a transport/network error (retryable); 3xx is non-retryable
// (no-redirect policy makes it permanent); 4xx is non-retryable EXCEPT
// 408/425/429 (explicitly retryable); 5xx is retryable.
type webhookDeliveryError struct {
	status   int
	category string
	err      error
}

func (d *webhookDeliveryError) Error() string {
	if d.err != nil {
		return d.err.Error()
	}
	return fmt.Sprintf("webhook delivery: HTTP %d", d.status)
}

// retryable classifies the failure per the release contract.
func (d *webhookDeliveryError) retryable() bool {
	if d.status == 0 {
		return true // network/timeout
	}
	switch d.status {
	case 408, 425, 429:
		return true
	}
	return d.status >= 500
}

// webhookDeliverFunc is the injection seam: production posts over HTTP; tests
// inject a stub. It must return nil only when the receiver ACKNOWLEDGED the
// request (HTTP 2xx). bearer, when non-empty, is sent as Authorization.
type webhookDeliverFunc func(ctx context.Context, url string, payload []byte, bearer string) *webhookDeliveryError

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
// lowercase, port stripped, trailing dot stripped, empty on garbage.
func normalizeHost(hostport string) string {
	h := strings.TrimSpace(strings.ToLower(hostport))
	if h == "" {
		return ""
	}
	if u, err := url.Parse("//" + h); err == nil {
		h = u.Hostname()
	}
	return strings.TrimSuffix(h, ".")
}

// allowed reports whether a webhook URL's host is in the node's allowlist.
// Fail-closed: an unparseable URL, an empty allowlist, or a URL smuggling its
// own credentials (userinfo) denies everything.
func allowed(domains []string, rawURL string) bool {
	if len(domains) == 0 {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.User != nil {
		return false // credentials belong in KALITA_EGRESS_SECRETS, never the URL
	}
	host := normalizeHost(u.Hostname())
	for _, d := range domains {
		if d == host {
			return true
		}
	}
	return false
}

// webhookHostOf extracts the normalized host of a webhook URL ("" on garbage).
func webhookHostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return normalizeHost(u.Hostname())
}

// deliverOnce posts payload to rawURL with a timeout; the client never follows
// a redirect (a redirect could escape the whitelist — the FIRST url is the only
// one the allowlist authorized).
func deliverOnce(ctx context.Context, rawURL string, payload []byte, bearer string) *webhookDeliveryError {
	client := &http.Client{
		Timeout: webhookTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // never follow — redirects bypass the whitelist
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(payload))
	if err != nil {
		return &webhookDeliveryError{category: "payload", err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", bearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		cat := "network"
		if ctx.Err() != nil {
			cat = "timeout"
		}
		return &webhookDeliveryError{category: cat, err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	cat := "http_4xx"
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		cat = "redirect"
	case resp.StatusCode >= 500:
		cat = "http_5xx"
	}
	return &webhookDeliveryError{status: resp.StatusCode, category: cat,
		err: fmt.Errorf("webhook %s: HTTP %d", rawURL, resp.StatusCode)}
}

// runWebhookDeliveries is the body of the runner tick: find open webhook tasks,
// enforce the whitelist, deliver with retries and per-attempt receipts, and
// settle each task. secrets maps domain -> bearer token (runtime-injected,
// never journaled). secretsConfigured reports whether the operator configured
// a secrets source at all — when true, a destination without a secret entry
// FAILS CLOSED (no unauthenticated send). Returns the number of tasks processed.
func runWebhookDeliveries(ctx context.Context, eng *engine.Engine, domains []string, secrets map[string]string, secretsConfigured bool, deliver webhookDeliverFunc) int {
	tasks := eng.OpenWebhookTasks()
	processed := 0
	for _, t := range tasks {
		rawURL := strings.TrimSpace(t.Args)
		if rawURL == "" {
			if err := eng.SettleWebhookTask(ctx, t.ID, false, "webhook task has no URL", "wh-settle:"+t.ID); err != nil {
				log.Printf("webhook: settle %s: %v", t.ID, err)
			}
			processed++
			continue
		}
		if !allowed(domains, rawURL) {
			// fail-closed: the intended domain is outside the node's allowlist
			// (or the URL smuggles its own credentials — a distinct receipt)
			cat, reason := "allowlist", "egress domain not in KALITA_EGRESS_DOMAINS"
			if u, uerr := url.Parse(rawURL); uerr == nil && u.User != nil {
				cat = "url_credentials"
				reason = "webhook URLs must not carry credentials (userinfo) — inject them via KALITA_EGRESS_SECRETS"
			}
			log.Printf("webhook: %s denied — %q outside KALITA_EGRESS_DOMAINS", t.ID, rawURL)
			_ = eng.RecordWebhookAttempt(ctx, t.ID, 1, "failed", cat,
				webhookHostOf(rawURL), 0, "wh-att:"+t.ID+":1")
			if err := eng.SettleWebhookTask(ctx, t.ID, false, reason, "wh-settle:"+t.ID); err != nil {
				log.Printf("webhook: settle %s: %v", t.ID, err)
			}
			processed++
			continue
		}

		// runtime-injected destination auth: never from DSL/prompt/journal.
		host := webhookHostOf(rawURL)
		bearer := secrets[host]
		if secretsConfigured && bearer == "" {
			// the operator declared secrets ARE in use: sending unauthenticated
			// would be a silent downgrade — fail safely instead.
			_ = eng.RecordWebhookAttempt(ctx, t.ID, 1, "failed", "auth_missing",
				host, 0, "wh-att:"+t.ID+":1")
			if err := eng.SettleWebhookTask(ctx, t.ID, false, "no egress secret configured for domain "+host, "wh-settle:"+t.ID); err != nil {
				log.Printf("webhook: settle %s: %v", t.ID, err)
			}
			processed++
			continue
		}
		// a credential must never ride cleartext, and a URL must not smuggle
		// its own credentials (userinfo) past the runtime-secret channel
		// (Security Pro MINOR-1 cleartext sub-case + MINOR-3 hygiene).
		if bearer != "" && strings.HasPrefix(strings.ToLower(rawURL), "http://") {
			_ = eng.RecordWebhookAttempt(ctx, t.ID, 1, "failed", "insecure_transport",
				host, 0, "wh-att:"+t.ID+":1")
			if err := eng.SettleWebhookTask(ctx, t.ID, false,
				"refusing to send a credentialed webhook over cleartext http:// — use https", "wh-settle:"+t.ID); err != nil {
				log.Printf("webhook: settle %s: %v", t.ID, err)
			}
			processed++
			continue
		}
		if u, err := url.Parse(rawURL); err == nil && u.User != nil {
			_ = eng.RecordWebhookAttempt(ctx, t.ID, 1, "failed", "url_credentials",
				host, 0, "wh-att:"+t.ID+":1")
			if err := eng.SettleWebhookTask(ctx, t.ID, false,
				"webhook URLs must not carry credentials (userinfo) — inject them via KALITA_EGRESS_SECRETS", "wh-settle:"+t.ID); err != nil {
				log.Printf("webhook: settle %s: %v", t.ID, err)
			}
			processed++
			continue
		}

		values, _ := eng.WebhookRecordValues(t.Entity, t.RecordID)
		payload, err := json.Marshal(map[string]any{
			// the per-firing idempotency identity: stable across redeliveries
			// AND across DLQ-recovery retries (DeliveryID carried forward), so
			// the receiver collapses at-least-once delivery to one effect.
			"idempotency_key": "webhook:" + webhookDeliveryID(t),
			"entity":          t.Entity,
			"record_id":       t.RecordID,
			"values":          values,
		})
		if err != nil {
			_ = eng.RecordWebhookAttempt(ctx, t.ID, 1, "failed", "payload",
				host, 0, "wh-att:"+t.ID+":1")
			_ = eng.SettleWebhookTask(ctx, t.ID, false, "payload marshal: "+err.Error(), "wh-settle:"+t.ID)
			processed++
			continue
		}

		var lastErr *webhookDeliveryError
		delivered := false
		lastAttempt := 0
		for attempt := 1; attempt <= webhookAttempts; attempt++ {
			lastAttempt = attempt
			start := time.Now()
			derr := deliver(ctx, rawURL, payload, bearer)
			latency := time.Since(start).Milliseconds()
			if derr == nil {
				delivered = true
				if err := eng.RecordWebhookAttempt(ctx, t.ID, attempt, "delivered",
					"", host, latency, "wh-att:"+t.ID+":"+fmt.Sprint(attempt)); err != nil {
					log.Printf("webhook: receipt %s/%d: %v", t.ID, attempt, err)
				}
				break
			}
			lastErr = derr
			if !derr.retryable() {
				if err := eng.RecordWebhookAttempt(ctx, t.ID, attempt, "failed",
					derr.category, host, latency, "wh-att:"+t.ID+":"+fmt.Sprint(attempt)); err != nil {
					log.Printf("webhook: receipt %s/%d: %v", t.ID, attempt, err)
				}
				break
			}
			if attempt < webhookAttempts {
				if err := eng.RecordWebhookAttempt(ctx, t.ID, attempt, "retrying",
					derr.category, host, latency, "wh-att:"+t.ID+":"+fmt.Sprint(attempt)); err != nil {
					log.Printf("webhook: receipt %s/%d: %v", t.ID, attempt, err)
				}
				select {
				case <-ctx.Done():
					return processed // shutting down: leave the task open for the next boot
				case <-time.After(webhookBackoff * time.Duration(attempt)):
				}
			} else {
				if err := eng.RecordWebhookAttempt(ctx, t.ID, attempt, "failed",
					"retry_exhausted", host, latency, "wh-att:"+t.ID+":"+fmt.Sprint(attempt)); err != nil {
					log.Printf("webhook: receipt %s/%d: %v", t.ID, attempt, err)
				}
			}
		}
		if delivered {
			if err := eng.SettleWebhookTask(ctx, t.ID, true, "delivered", "wh-settle:"+t.ID); err != nil {
				log.Printf("webhook: settle %s: %v", t.ID, err)
			}
		} else {
			log.Printf("webhook: %s dead-lettered after %d attempt(s): %v", t.ID, lastAttempt, lastErr)
			if err := eng.SettleWebhookTask(ctx, t.ID, false, "delivery failed: "+lastErr.Error(), "wh-settle:"+t.ID); err != nil {
				log.Printf("webhook: settle %s: %v", t.ID, err)
			}
		}
		processed++
	}
	return processed
}

// webhookDeliveryID returns the task's per-firing delivery identity; legacy
// tasks (pre-hardening journals) fall back to the task id itself.
func webhookDeliveryID(t *engine.Task) string {
	if t.DeliveryID != "" {
		return t.DeliveryID
	}
	return t.ID
}
