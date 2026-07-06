package engine

import (
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Egress report (LLD-09 §3). This is deliberately a self-declaration +
// journaled-fact reconciliation, NOT network interception — the kernel
// cannot and must not inspect a worker's actual traffic (T8). The model:
//
//   - a worker declares its OWN whitelist of external domains in its actor
//     manifest (identity.ActorMeta.EgressDomains) at registration time;
//   - the worker itself journals every real external call as a domain event
//     named "egress.called" via the ordinary emit_event path (engine.EmitEvent
//     — the SAME choke point and audit trail as any other domain event, LLD-01
//     §4.2), with payload fields {domain, purpose, ref?}. There is no new
//     eventstore.Kind for this — it rides on the existing event.emitted
//     envelope; only the payload SHAPE and event NAME are a convention this
//     LLD fixes. A pack wanting egress accountability declares this event
//     itself (see SECURITY.md rule 3).
//   - BuildEgressReport reconciles observed calls against each worker's
//     declared whitelist. The identity of "who called" is the AUTHENTICATED
//     actor (ev.Actor.ID) that invoked emit_event — never a self-reported
//     "worker" field inside the payload, which a compromised/misbehaving
//     worker could spoof. A worker with NO declared whitelist at all is
//     treated as fully undeclared: every one of its calls is an anomaly —
//     the honest "lite" boundary (no declaration = no benefit of the doubt),
//     not a guarantee against a hostile worker (that needs full egress DLP,
//     out of scope — see SECURITY.md and RELEASE-1.0 §5 H4).

// EgressCalledEventName is the short (unqualified, own-pack) event name
// `kalita egress-report` looks for among event.emitted facts — matched via
// shortEventName, so it works regardless of which pack declares it. DSL event
// names are PascalCase identifiers (dots are reserved for the <pack>.<Event>
// qualifier, LLD-01 §7), so this is the DSL spelling of what LLD-09 §3
// describes informally as "egress.called {worker, domain, purpose, ref?}" —
// worker identity is NOT a payload field (see BuildEgressReport doc); a pack
// wiring worker egress accountability declares:
//
//	event EgressCalled:
//	    domain:  string required
//	    purpose: string
//	    ref:     string
const EgressCalledEventName = "EgressCalled"

// EgressCall is one observed external call.
type EgressCall struct {
	Worker  string // authenticated actor id (NOT a self-reported payload field)
	Domain  string
	Purpose string
	Ref     string
	TS      time.Time
	Anomaly bool
	Reason  string // why it is (or the worker is) flagged
}

// EgressReport is the "what left the perimeter" page.
type EgressReport struct {
	Since     time.Time
	Until     time.Time
	Calls     []EgressCall // chronological
	Anomalies int
}

// eventEmittedFold mirrors eventEmittedPayload (events.go) so this file does
// not depend on that unexported type's name staying stable across the
// package.
type eventEmittedFold struct {
	Name    string         `json:"name"`
	Payload map[string]any `json:"payload"`
}

// BuildEgressReport folds event.emitted "egress.called" facts within
// [since, now) against each worker's declared whitelist. whitelists maps
// actor id -> declared domains (identity.ActorMeta.EgressDomains); nil or an
// absent entry means "no declaration" — every call from that worker is then
// an anomaly, not silently passed (LLD-09 §3, honest lite boundary).
func BuildEgressReport(events []*eventstore.Event, whitelists map[string][]string, since, now time.Time) *EgressReport {
	rep := &EgressReport{Since: since, Until: now}
	for _, ev := range events {
		if ev.Kind != eventstore.EventEmitted {
			continue
		}
		if ev.TS.Before(since) {
			continue
		}
		var p eventEmittedFold
		if json.Unmarshal(ev.Payload, &p) != nil {
			continue
		}
		if shortEventName(p.Name) != EgressCalledEventName {
			continue
		}
		domain, _ := p.Payload["domain"].(string)
		purpose, _ := p.Payload["purpose"].(string)
		ref, _ := p.Payload["ref"].(string)
		call := EgressCall{
			Worker: ev.Actor.ID, Domain: domain, Purpose: purpose, Ref: ref, TS: ev.TS,
		}
		declared, ok := whitelists[ev.Actor.ID]
		switch {
		case !ok || len(declared) == 0:
			call.Anomaly = true
			call.Reason = "worker " + ev.Actor.ID + " has no declared egress whitelist"
		case !containsDomain(declared, domain):
			call.Anomaly = true
			call.Reason = fmt.Sprintf("domain %s is outside %s's declared whitelist", domain, ev.Actor.ID)
		}
		if call.Anomaly {
			rep.Anomalies++
		}
		rep.Calls = append(rep.Calls, call)
	}
	sort.SliceStable(rep.Calls, func(i, j int) bool { return rep.Calls[i].TS.Before(rep.Calls[j].TS) })
	return rep
}

func containsDomain(declared []string, domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	for _, d := range declared {
		if strings.ToLower(strings.TrimSpace(d)) == domain {
			return true
		}
	}
	return false
}

// RenderMarkdown renders "what left the perimeter" — anomalies first, then
// the full chronological log. Buildless, no template engine.
func (r *EgressReport) RenderMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Kalita — egress-отчёт (что покидало периметр)\n\n")
	fmt.Fprintf(&b, "Период: %s — %s\n\n", r.Since.Format(time.RFC3339), r.Until.Format(time.RFC3339))
	fmt.Fprintf(&b, "_Самодекларация воркеров + событийный лог — НЕ перехват трафика. "+
		"Граница: доверенная кооперация воркеров под контролем оператора (T8), не защита от враждебного воркера._\n\n")

	fmt.Fprintf(&b, "## Итого\n\n- вызовов: %d\n- аномалий: **%d**\n\n", len(r.Calls), r.Anomalies)

	if r.Anomalies > 0 {
		b.WriteString("## Аномалии\n\n")
		b.WriteString("| время | воркер | домен | причина |\n|---|---|---|---|\n")
		for _, c := range r.Calls {
			if !c.Anomaly {
				continue
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", c.TS.Format(time.RFC3339), c.Worker, c.Domain, c.Reason)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Журнал вызовов\n\n")
	if len(r.Calls) == 0 {
		b.WriteString("_нет вызовов за период._\n")
		return b.String()
	}
	b.WriteString("| время | воркер | домен | назначение | флаг |\n|---|---|---|---|---|\n")
	for _, c := range r.Calls {
		flag := ""
		if c.Anomaly {
			flag = "⚠ аномалия"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", c.TS.Format(time.RFC3339), c.Worker, c.Domain, c.Purpose, flag)
	}
	return b.String()
}

// RenderHTML renders the same report as a single self-contained HTML page
// (buildless, same posture as web/catalog.html).
func (r *EgressReport) RenderHTML() string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\">")
	b.WriteString("<title>Kalita — egress-отчёт</title>")
	b.WriteString("<style>body{font:14px/1.5 -apple-system,Segoe UI,sans-serif;max-width:900px;margin:2rem auto;padding:0 1rem;color:#1a1a1a}" +
		"table{border-collapse:collapse;width:100%}td,th{border:1px solid #ccc;padding:4px 8px;text-align:left}" +
		"h1{font-size:1.4rem}h2{font-size:1.1rem;margin-top:1.5rem}.note{color:#666;font-size:0.9em}.anom{background:#fff3cd}</style></head><body>")
	b.WriteString("<h1>Kalita — egress-отчёт (что покидало периметр)</h1>")
	fmt.Fprintf(&b, "<p>Период: %s — %s</p>", html.EscapeString(r.Since.Format(time.RFC3339)), html.EscapeString(r.Until.Format(time.RFC3339)))
	b.WriteString("<p class=\"note\">Самодекларация воркеров + событийный лог — НЕ перехват трафика. " +
		"Доверенная кооперация воркеров под контролем оператора (T8), не защита от враждебного воркера.</p>")

	fmt.Fprintf(&b, "<h2>Итого</h2><ul><li>вызовов: %d</li><li>аномалий: <b>%d</b></li></ul>", len(r.Calls), r.Anomalies)

	b.WriteString("<h2>Журнал вызовов</h2>")
	if len(r.Calls) == 0 {
		b.WriteString("<p class=\"note\">нет вызовов за период.</p></body></html>")
		return b.String()
	}
	b.WriteString("<table><tr><th>время</th><th>воркер</th><th>домен</th><th>назначение</th><th>флаг</th></tr>")
	for _, c := range r.Calls {
		row := ""
		if c.Anomaly {
			row = " class=\"anom\""
		}
		flag := ""
		if c.Anomaly {
			flag = "⚠ " + html.EscapeString(c.Reason)
		}
		fmt.Fprintf(&b, "<tr%s><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>",
			row, html.EscapeString(c.TS.Format(time.RFC3339)), html.EscapeString(c.Worker),
			html.EscapeString(c.Domain), html.EscapeString(c.Purpose), flag)
	}
	b.WriteString("</table></body></html>")
	return b.String()
}
