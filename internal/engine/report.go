package engine

import (
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// "Week in numbers" (LLD-09 §2): `kalita report` is a PURE fold over the
// journal — zero writes, no engine instance required. BuildReport takes the
// events straight from Store.All/Since and (optionally) the compiled model's
// role declarations for the rate/hours cost-estimation figures (LLD-09 §2:
// "если роль не декларирует ставку — часы×ставка опускаются с пометкой, не
// падает"). roles == nil (pack unknown to the caller) degrades exactly the
// same way as an individual role missing rate/hours: every cost figure is
// omitted with a note, nothing panics — the golden/I-1 case (test 6) is this
// same path with an empty roles map on an event slice with zero role.* /
// mutation.* events.

// ActionCounts tallies record-mutating events by who did them.
type ActionCounts struct {
	Human  int
	Agent  int
	System int
	Total  int
}

// RoleStats is one role's slice of the report.
type RoleStats struct {
	Role    string
	Actions int
	// Hours/Cost are populated only when the role declares the corresponding
	// attribute (HasHours/HasRate) — see dsl.RoleDecl. Note explains what is
	// missing and why, rather than silently reporting 0.
	Hours    float64
	HasHours bool
	Cost     float64
	HasCost  bool
	Note     string
}

// ApprovalQueueStats describes the pending human-signature queue.
type ApprovalQueueStats struct {
	Pending   int
	OldestAge time.Duration // zero when Pending == 0
}

// SLAStats counts automation retry-exhaustion signals (LLD-01 §3's "stuck"
// mechanics) within the window — the report's SLA-violation proxy.
type SLAStats struct {
	Failed    int
	Abandoned int
}

// Report is the full "week in numbers" result.
type Report struct {
	Since time.Time
	Until time.Time
	Role  string // "" unless filtered

	Actions   ActionCounts
	ByRole    []RoleStats // sorted by role name
	Approvals ApprovalQueueStats
	SLA       SLAStats

	// PackKnown is false when roles == nil was passed to BuildReport: every
	// RoleStats note then says so, and this flag lets the renderer print one
	// summary note instead of repeating it per role.
	PackKnown bool
}

// BuildReport folds events within [since, now) into a Report. events need not
// be pre-filtered by time — BuildReport applies the since cutoff itself, so a
// caller can simply pass Store.All(ctx). roleFilter, if non-empty, restricts
// both the action counts and ByRole to that one role. roles may be nil.
func BuildReport(events []*eventstore.Event, roles map[string]*dsl.RoleDecl, since, now time.Time, roleFilter string) *Report {
	rep := &Report{Since: since, Until: now, Role: roleFilter, PackKnown: roles != nil}

	actionsByRole := map[string]int{}
	// approvalRequestedAt: approval.requested ts, keyed by approval_id;
	// deleted when a granted/rejected for the same id is seen — what remains
	// at the end is the pending queue, with its journaled request time.
	approvalRequestedAt := map[string]time.Time{}

	for _, ev := range events {
		if ev.TS.Before(since) {
			continue
		}
		switch ev.Kind {
		case eventstore.RecordCreated, eventstore.RecordUpdated, eventstore.RecordAction:
			if roleFilter != "" && ev.Actor.Role != roleFilter {
				continue
			}
			rep.Actions.Total++
			switch ev.Actor.Type {
			case eventstore.ActorHuman:
				rep.Actions.Human++
			case eventstore.ActorAgent:
				rep.Actions.Agent++
			default:
				rep.Actions.System++
			}
			if ev.Actor.Role != "" {
				actionsByRole[ev.Actor.Role]++
			}
		case eventstore.ApprovalRequested:
			approvalRequestedAt[ev.Subject.ApprovalID] = ev.TS
		case eventstore.ApprovalGranted, eventstore.ApprovalRejected:
			delete(approvalRequestedAt, ev.Subject.ApprovalID)
		case eventstore.AutomationFailed:
			rep.SLA.Failed++
		case eventstore.AutomationAbandoned:
			rep.SLA.Abandoned++
		}
	}

	rep.Approvals.Pending = len(approvalRequestedAt)
	for _, ts := range approvalRequestedAt {
		age := now.Sub(ts)
		if age > rep.Approvals.OldestAge {
			rep.Approvals.OldestAge = age
		}
	}

	roleNames := make([]string, 0, len(actionsByRole))
	for r := range actionsByRole {
		roleNames = append(roleNames, r)
	}
	sort.Strings(roleNames)
	for _, r := range roleNames {
		rs := RoleStats{Role: r, Actions: actionsByRole[r]}
		decl, ok := roles[r]
		switch {
		case !rep.PackKnown:
			rs.Note = "pack not given to the report (--pack) — rate/hours unknown"
		case !ok:
			rs.Note = "role " + r + " not found in the given pack — rate/hours unknown"
		case !decl.HasHours && !decl.HasRate:
			rs.Note = "role " + r + " declares neither `hours` nor `rate` — hours×rate omitted"
		case !decl.HasHours:
			rs.Note = "role " + r + " does not declare `hours` — hours×rate omitted"
		case !decl.HasRate:
			rs.Hours = decl.Hours * float64(rs.Actions)
			rs.HasHours = true
			rs.Note = "role " + r + " does not declare `rate` — cost omitted"
		default:
			rs.Hours = decl.Hours * float64(rs.Actions)
			rs.HasHours = true
			rs.Cost = rs.Hours * decl.Rate
			rs.HasCost = true
		}
		rep.ByRole = append(rep.ByRole, rs)
	}
	return rep
}

// RenderMarkdown renders the report as a Markdown document — no template
// engine, no build step (LLD-09 §2: "buildless", the same posture as the
// catalog's static HTML).
func (r *Report) RenderMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Kalita — week in numbers\n\n")
	fmt.Fprintf(&b, "Period: %s — %s", r.Since.Format(time.RFC3339), r.Until.Format(time.RFC3339))
	if r.Role != "" {
		fmt.Fprintf(&b, " (role: %s)", r.Role)
	}
	b.WriteString("\n\n")

	fmt.Fprintf(&b, "## Actions\n\n")
	fmt.Fprintf(&b, "- total: **%d**\n", r.Actions.Total)
	fmt.Fprintf(&b, "- humans: %d\n", r.Actions.Human)
	fmt.Fprintf(&b, "- agents: %d\n", r.Actions.Agent)
	fmt.Fprintf(&b, "- system: %d\n\n", r.Actions.System)

	fmt.Fprintf(&b, "## By role\n\n")
	if !r.PackKnown {
		fmt.Fprintf(&b, "_no --pack given — hours×rate unavailable for every role._\n\n")
	}
	if len(r.ByRole) == 0 {
		b.WriteString("_no actions in the period._\n\n")
	} else {
		b.WriteString("| role | actions | hours | cost | note |\n")
		b.WriteString("|---|---|---|---|---|\n")
		for _, rs := range r.ByRole {
			hours := "—"
			if rs.HasHours {
				hours = fmt.Sprintf("%.1f", rs.Hours)
			}
			cost := "—"
			if rs.HasCost {
				cost = fmt.Sprintf("%.2f", rs.Cost)
			}
			fmt.Fprintf(&b, "| %s | %d | %s | %s | %s |\n", rs.Role, rs.Actions, hours, cost, rs.Note)
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "## Signature queue\n\n")
	fmt.Fprintf(&b, "- pending: **%d**\n", r.Approvals.Pending)
	if r.Approvals.Pending > 0 {
		fmt.Fprintf(&b, "- oldest age: %s\n", r.Approvals.OldestAge.Round(time.Minute))
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## SLA breaches (automation)\n\n")
	fmt.Fprintf(&b, "- failures still retrying: %d\n", r.SLA.Failed)
	fmt.Fprintf(&b, "- finally abandoned (retries exhausted): %d\n", r.SLA.Abandoned)

	return b.String()
}

// RenderHTML renders the report as a single, self-contained HTML page — the
// same buildless posture as web/catalog.html (LLD-05): inline CSS, no JS, no
// external asset, nothing to bundle. Meant to be attached/sent by a worker
// (LLD-09 §2 — the kernel does not email anything itself).
func (r *Report) RenderHTML() string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\">")
	b.WriteString("<title>Kalita — week in numbers</title>")
	b.WriteString("<style>body{font:14px/1.5 -apple-system,Segoe UI,sans-serif;max-width:820px;margin:2rem auto;padding:0 1rem;color:#1a1a1a}" +
		"table{border-collapse:collapse;width:100%}td,th{border:1px solid #ccc;padding:4px 8px;text-align:left}" +
		"h1{font-size:1.4rem}h2{font-size:1.1rem;margin-top:1.5rem}.note{color:#666;font-size:0.9em}</style></head><body>")
	fmt.Fprintf(&b, "<h1>Kalita — week in numbers</h1>")
	fmt.Fprintf(&b, "<p>Period: %s — %s", html.EscapeString(r.Since.Format(time.RFC3339)), html.EscapeString(r.Until.Format(time.RFC3339)))
	if r.Role != "" {
		fmt.Fprintf(&b, " (role: %s)", html.EscapeString(r.Role))
	}
	b.WriteString("</p>")

	fmt.Fprintf(&b, "<h2>Actions</h2><ul><li>total: <b>%d</b></li><li>humans: %d</li><li>agents: %d</li><li>system: %d</li></ul>",
		r.Actions.Total, r.Actions.Human, r.Actions.Agent, r.Actions.System)

	b.WriteString("<h2>By role</h2>")
	if !r.PackKnown {
		b.WriteString("<p class=\"note\">no --pack given — hours×rate unavailable for every role.</p>")
	}
	if len(r.ByRole) == 0 {
		b.WriteString("<p class=\"note\">no actions in the period.</p>")
	} else {
		b.WriteString("<table><tr><th>role</th><th>actions</th><th>hours</th><th>cost</th><th>note</th></tr>")
		for _, rs := range r.ByRole {
			hours := "—"
			if rs.HasHours {
				hours = fmt.Sprintf("%.1f", rs.Hours)
			}
			cost := "—"
			if rs.HasCost {
				cost = fmt.Sprintf("%.2f", rs.Cost)
			}
			fmt.Fprintf(&b, "<tr><td>%s</td><td>%d</td><td>%s</td><td>%s</td><td class=\"note\">%s</td></tr>",
				html.EscapeString(rs.Role), rs.Actions, hours, cost, html.EscapeString(rs.Note))
		}
		b.WriteString("</table>")
	}

	fmt.Fprintf(&b, "<h2>Signature queue</h2><ul><li>pending: <b>%d</b></li>", r.Approvals.Pending)
	if r.Approvals.Pending > 0 {
		fmt.Fprintf(&b, "<li>oldest age: %s</li>", r.Approvals.OldestAge.Round(time.Minute))
	}
	b.WriteString("</ul>")

	fmt.Fprintf(&b, "<h2>SLA breaches (automation)</h2><ul><li>failures still retrying: %d</li><li>finally abandoned: %d</li></ul>",
		r.SLA.Failed, r.SLA.Abandoned)

	b.WriteString("</body></html>")
	return b.String()
}
