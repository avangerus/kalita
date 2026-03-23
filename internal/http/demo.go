package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"time"

	"kalita/internal/controlplane"
	"kalita/internal/demo"

	"github.com/gin-gonic/gin"
)

type demoOperatorClient struct{ handler http.Handler }

func newDemoOperatorClient(handler http.Handler) *demoOperatorClient {
	return &demoOperatorClient{handler: handler}
}

func (c *demoOperatorClient) get(path string, out any) error {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return fmt.Errorf("GET %s returned %d: %s", path, rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	return json.Unmarshal(rec.Body.Bytes(), out)
}

func (c *demoOperatorClient) post(path string, body []byte, contentType string, out any) error {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return fmt.Errorf("POST %s returned %d: %s", path, rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(rec.Body.Bytes(), out)
}

type dashboardMetrics struct {
	ActiveCases                 int
	BlockedCases                int
	DeferredWorkItems           int
	PendingApprovals            int
	ExecutingSessions           int
	FailedExecutions            int
	ActorTrustDistribution      []trustBucket
	UnresolvedRouteIncidents    int
	PendingSupervisorReviews    int
	DeferredReconciliationTasks int
}

type trustBucket struct {
	Level string
	Count int
}

type caseListRow struct {
	CaseID               string
	Type                 string
	TypeLabel            string
	Status               string
	StateBadge           string
	BlockingOrDeferred   string
	PendingApprovalCount int
	RouteRef             string
	DomainReason         string
	ReferenceLine        string
}

type caseDetailTimelineRow struct {
	OccurredAt   time.Time
	Step         string
	Status       string
	Payload      map[string]any
	DomainTitle  string
	DomainDetail string
}

type caseDetailPage struct {
	Case          controlplane.CaseOverview
	WorkItem      *controlplane.WorkItemOverview
	Timeline      []caseDetailTimelineRow
	DomainContext demo.DomainCaseContext
	ErrorMessage  string
}

func registerDemoRoutes(r *gin.Engine) {
	client := newDemoOperatorClient(r)
	r.GET("/demo", func(c *gin.Context) { renderDemoDashboard(c, client) })
	r.GET("/demo/cases", func(c *gin.Context) { renderDemoCases(c, client) })
	r.GET("/demo/cases/:id", func(c *gin.Context) { renderDemoCaseDetail(c, client, c.Param("id")) })
	r.POST("/demo/cases/:id/acknowledge", func(c *gin.Context) { postDemoCaseAction(c, client, c.Param("id"), "acknowledge") })
	r.POST("/demo/cases/:id/notes", func(c *gin.Context) { postDemoCaseAction(c, client, c.Param("id"), "notes") })
	r.POST("/demo/cases/:id/recoordinate", func(c *gin.Context) { postDemoCaseAction(c, client, c.Param("id"), "recoordinate") })
	r.POST("/demo/cases/:id/external-input", func(c *gin.Context) { postDemoCaseAction(c, client, c.Param("id"), "external-input") })
	r.GET("/demo/approvals", func(c *gin.Context) { renderDemoApprovals(c, client) })
	r.POST("/demo/approvals/:id/approve", func(c *gin.Context) { postDemoApprovalAction(c, client, c.Param("id"), "approve") })
	r.POST("/demo/approvals/:id/reject", func(c *gin.Context) { postDemoApprovalAction(c, client, c.Param("id"), "reject") })
}

func renderDemoDashboard(c *gin.Context, client *demoOperatorClient) {
	summary, workItems, actors, err := loadDashboardData(client)
	if err != nil {
		renderDemoError(c, err)
		return
	}
	metrics := buildDashboardMetrics(summary, workItems, actors)
	renderDemoHTML(c, "Kalita Demo Console", demoDashboardTemplate, map[string]any{"Metrics": metrics, "Actors": actors, "Now": time.Now().UTC()})
}

func renderDemoCases(c *gin.Context, client *demoOperatorClient) {
	cases, workItems, approvals, err := loadCaseListData(client)
	if err != nil {
		renderDemoError(c, err)
		return
	}
	rows := buildCaseRows(cases, workItems, approvals)
	renderDemoHTML(c, "Cases · Kalita Demo Console", demoCasesTemplate, map[string]any{"Cases": rows})
}

func renderDemoCaseDetail(c *gin.Context, client *demoOperatorClient, caseID string) {
	overview, workItems, timeline, err := loadCaseDetailData(client, caseID)
	if err != nil {
		renderDemoError(c, err)
		return
	}
	page := caseDetailPage{Case: overview}
	for _, wi := range workItems {
		if wi.CaseID == caseID {
			copy := wi
			page.WorkItem = &copy
		}
	}
	page.DomainContext = demo.BuildDomainCaseContext(overview, page.WorkItem, timeline)
	page.Timeline = buildCaseDetailTimeline(overview.Kind, timeline)
	page.ErrorMessage = strings.TrimSpace(c.Query("error"))
	renderDemoHTML(c, "Case Detail · Kalita Demo Console", demoCaseDetailTemplate, map[string]any{"Page": page})
}

func postDemoCaseAction(c *gin.Context, client *demoOperatorClient, caseID string, action string) {
	redirectTarget := "/demo/cases/" + caseID
	var (
		body        []byte
		contentType string
		updated     controlplane.CaseOverview
	)
	switch action {
	case "acknowledge", "recoordinate":
	case "notes":
		payload := map[string]string{"text": c.PostForm("text")}
		body, _ = json.Marshal(payload)
		contentType = "application/json"
	case "external-input":
		payload := map[string]string{"source": c.PostForm("source"), "text": c.PostForm("text")}
		body, _ = json.Marshal(payload)
		contentType = "application/json"
	}
	if err := client.post(fmt.Sprintf("/api/operator/cases/%s/%s", caseID, action), body, contentType, &updated); err != nil {
		c.Redirect(http.StatusSeeOther, redirectTarget+"?error="+url.QueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusSeeOther, redirectTarget)
}

func renderDemoApprovals(c *gin.Context, client *demoOperatorClient) {
	var approvals []controlplane.ApprovalInboxItem
	if err := client.get("/api/operator/approvals", &approvals); err != nil {
		renderDemoError(c, err)
		return
	}
	renderDemoHTML(c, "Approval Inbox · Kalita Demo Console", demoApprovalsTemplate, map[string]any{"Approvals": approvals})
}

func postDemoApprovalAction(c *gin.Context, client *demoOperatorClient, approvalID string, action string) {
	var item controlplane.ApprovalInboxItem
	if err := client.post(fmt.Sprintf("/api/operator/approvals/%s/%s", approvalID, action), nil, "", &item); err != nil {
		renderDemoError(c, err)
		return
	}
	redirectTarget := c.PostForm("redirect")
	if strings.TrimSpace(redirectTarget) == "" {
		redirectTarget = "/demo/approvals"
	}
	c.Redirect(http.StatusSeeOther, redirectTarget)
}

func loadDashboardData(client *demoOperatorClient) (controlplane.Summary, []controlplane.WorkItemOverview, []controlplane.ActorOverview, error) {
	var summary controlplane.Summary
	var workItems []controlplane.WorkItemOverview
	var actors []controlplane.ActorOverview
	if err := client.get("/api/operator/summary", &summary); err != nil {
		return controlplane.Summary{}, nil, nil, err
	}
	if err := client.get("/api/operator/work-items", &workItems); err != nil {
		return controlplane.Summary{}, nil, nil, err
	}
	if err := client.get("/api/operator/actors", &actors); err != nil {
		return controlplane.Summary{}, nil, nil, err
	}
	return summary, workItems, actors, nil
}

func loadCaseListData(client *demoOperatorClient) ([]controlplane.CaseOverview, []controlplane.WorkItemOverview, []controlplane.ApprovalInboxItem, error) {
	var cases []controlplane.CaseOverview
	var workItems []controlplane.WorkItemOverview
	var approvals []controlplane.ApprovalInboxItem
	if err := client.get("/api/operator/cases", &cases); err != nil {
		return nil, nil, nil, err
	}
	if err := client.get("/api/operator/work-items", &workItems); err != nil {
		return nil, nil, nil, err
	}
	if err := client.get("/api/operator/approvals", &approvals); err != nil {
		return nil, nil, nil, err
	}
	return cases, workItems, approvals, nil
}

func loadCaseDetailData(client *demoOperatorClient, caseID string) (controlplane.CaseOverview, []controlplane.WorkItemOverview, []controlplane.TimelineEntry, error) {
	var overview controlplane.CaseOverview
	var workItems []controlplane.WorkItemOverview
	var timeline []controlplane.TimelineEntry
	if err := client.get(fmt.Sprintf("/api/operator/cases/%s", caseID), &overview); err != nil {
		return controlplane.CaseOverview{}, nil, nil, err
	}
	if err := client.get("/api/operator/work-items", &workItems); err != nil {
		return controlplane.CaseOverview{}, nil, nil, err
	}
	if err := client.get(fmt.Sprintf("/api/operator/cases/%s/timeline", caseID), &timeline); err != nil {
		return controlplane.CaseOverview{}, nil, nil, err
	}
	return overview, workItems, timeline, nil
}

func buildDashboardMetrics(summary controlplane.Summary, workItems []controlplane.WorkItemOverview, actors []controlplane.ActorOverview) dashboardMetrics {
	metrics := dashboardMetrics{ActiveCases: summary.OpenCaseCount, PendingApprovals: summary.ApprovalPendingCount, DeferredWorkItems: summary.DeferredCount, ExecutingSessions: summary.ExecutingSessionCount, BlockedCases: summary.BlockedCount}
	trustCounts := map[string]int{}
	blockedCaseIDs := map[string]struct{}{}
	for _, wi := range workItems {
		if wi.Type == "missed_container_pickup_review" {
			metrics.UnresolvedRouteIncidents++
			if wi.Coordination.DecisionType == "defer" {
				metrics.DeferredReconciliationTasks++
			}
			if wi.PolicyApproval.ApprovalRequestStatus == "pending" {
				metrics.PendingSupervisorReviews++
			}
		}
		switch wi.Execution.Status {
		case "running":
			metrics.ExecutingSessions++
		case "failed":
			metrics.FailedExecutions++
		}
	}
	metrics.BlockedCases = len(blockedCaseIDs)
	for _, actor := range actors {
		level := actor.TrustLevel
		if strings.TrimSpace(level) == "" {
			level = "unclassified"
		}
		trustCounts[level]++
	}
	if len(summary.TrustLevelCounts) > 0 {
		trustCounts = summary.TrustLevelCounts
	}
	for level, count := range trustCounts {
		metrics.ActorTrustDistribution = append(metrics.ActorTrustDistribution, trustBucket{Level: level, Count: count})
	}
	sort.Slice(metrics.ActorTrustDistribution, func(i, j int) bool {
		return metrics.ActorTrustDistribution[i].Level < metrics.ActorTrustDistribution[j].Level
	})
	return metrics
}

func buildCaseRows(cases []controlplane.CaseOverview, workItems []controlplane.WorkItemOverview, approvals []controlplane.ApprovalInboxItem) []caseListRow {
	pendingByCase := map[string]int{}
	for _, approval := range approvals {
		pendingByCase[approval.CaseID]++
	}
	workByCase := map[string][]controlplane.WorkItemOverview{}
	for _, wi := range workItems {
		workByCase[wi.CaseID] = append(workByCase[wi.CaseID], wi)
	}
	rows := make([]caseListRow, 0, len(cases))
	for _, item := range cases {
		row := caseListRow{CaseID: item.CaseID, Type: item.Kind, TypeLabel: item.Kind, Status: item.Status, PendingApprovalCount: pendingByCase[item.CaseID]}
		reasons := make([]string, 0)
		for _, wi := range workByCase[item.CaseID] {
			if wi.Coordination.Reason != "" {
				reasons = append(reasons, wi.Coordination.Reason)
			}
			if wi.PolicyApproval.Reason != "" {
				reasons = append(reasons, wi.PolicyApproval.Reason)
			}
		}
		ctx := demo.BuildDomainCaseContext(item, latestWorkItem(workByCase[item.CaseID]), nil)
		if ctx.CaseTypeLabel != "" {
			row.TypeLabel = ctx.CaseTypeLabel
			row.RouteRef = ctx.RouteID
			row.DomainReason = ctx.ReasonLabel
			row.ReferenceLine = strings.Trim(strings.Join([]string{ctx.ContainerSiteID, ctx.ReferenceLine}, " · "), " ·")
		}
		row.BlockingOrDeferred = strings.Join(uniqueStrings(reasons), " | ")
		row.StateBadge = deriveStateBadge(workByCase[item.CaseID], row.PendingApprovalCount)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CaseID < rows[j].CaseID })
	return rows
}

func latestWorkItem(items []controlplane.WorkItemOverview) *controlplane.WorkItemOverview {
	if len(items) == 0 {
		return nil
	}
	best := items[0]
	for _, item := range items[1:] {
		if item.UpdatedAt.After(best.UpdatedAt) {
			best = item
		}
	}
	copy := best
	return &copy
}

func deriveStateBadge(items []controlplane.WorkItemOverview, pendingApprovals int) string {
	for _, wi := range items {
		if wi.Execution.Status == "running" {
			return "Executing"
		}
	}
	if pendingApprovals > 0 {
		return "Waiting Approval"
	}
	for _, wi := range items {
		switch wi.Coordination.DecisionType {
		case "block", "escalate":
			return "Blocked"
		case "defer":
			return "Deferred"
		}
	}
	return "Open"
}

func buildCaseDetailTimeline(caseKind string, timeline []controlplane.TimelineEntry) []caseDetailTimelineRow {
	rows := make([]caseDetailTimelineRow, 0, len(timeline))
	for _, entry := range timeline {
		mapped := demo.BuildDomainTimelineEntry(caseKind, entry)
		rows = append(rows, caseDetailTimelineRow{OccurredAt: entry.OccurredAt, Step: entry.Step, Status: entry.Status, Payload: entry.Payload, DomainTitle: mapped.Title, DomainDetail: mapped.Description})
	}
	return rows
}

func uniqueStrings(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func renderDemoError(c *gin.Context, err error) {
	c.String(http.StatusInternalServerError, "demo console error: %v", err)
}

func renderDemoHTML(c *gin.Context, title string, body string, data map[string]any) {
	funcs := template.FuncMap{
		"fmtTime": func(t time.Time) string {
			if t.IsZero() {
				return "-"
			}
			return t.UTC().Format(time.RFC3339)
		},
		"fmtTimePtr": func(t *time.Time) string {
			if t == nil || t.IsZero() {
				return "-"
			}
			return t.UTC().Format(time.RFC3339)
		},
		"payload": func(m map[string]any) string {
			if len(m) == 0 {
				return "-"
			}
			b, _ := json.Marshal(m)
			return string(b)
		},
	}
	tmpl := template.Must(template.New("page").Funcs(funcs).Parse(demoLayoutTemplate + body))
	if data == nil {
		data = map[string]any{}
	}
	data["Title"] = title
	c.Header("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.ExecuteTemplate(c.Writer, "layout", data)
}

const demoLayoutTemplate = `{{define "layout"}}<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>{{.Title}}</title>
<style>
body{font-family:Arial,sans-serif;margin:24px;color:#222}nav a{margin-right:12px}table{border-collapse:collapse;width:100%;margin-top:12px}th,td{border:1px solid #ccc;padding:8px;vertical-align:top;text-align:left} .cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:12px;margin:16px 0}.card{border:1px solid #ccc;padding:12px}.muted{color:#666}.pill{display:inline-block;border:1px solid #999;border-radius:999px;padding:2px 8px;font-size:12px}.actions form{display:inline-block;margin-right:8px}pre{white-space:pre-wrap;word-break:break-word;margin:0}.section-title{margin-top:28px}.lede{max-width:900px;margin-top:8px}.domain{background:#f7f8fa;border:1px solid #d8dce3;padding:16px;margin-top:16px}
</style></head><body>
<h1>{{.Title}}</h1>
<nav><a href="/demo">Dashboard</a><a href="/demo/cases">Cases</a><a href="/demo/approvals">Approval Inbox</a></nav>
{{template "body" .}}
</body></html>{{end}}`

const demoDashboardTemplate = `{{define "body"}}
<p class="lede">AIS Otkhody demo workload: multiple missed pickup incidents enter Kalita together, and the same deterministic control-plane pipeline fans them into executing, deferred, blocked, approval-gated, and escalated paths.</p>
<div class="cards">
  <div class="card"><strong>Unresolved route incidents</strong><div>{{.Metrics.UnresolvedRouteIncidents}}</div></div>
  <div class="card"><strong>Pending supervisor reviews</strong><div>{{.Metrics.PendingSupervisorReviews}}</div></div>
  <div class="card"><strong>Deferred reconciliation tasks</strong><div>{{.Metrics.DeferredReconciliationTasks}}</div></div>
</div>
<div class="cards">
  <div class="card"><strong>Active cases</strong><div>{{.Metrics.ActiveCases}}</div></div>
  <div class="card"><strong>Blocked cases</strong><div>{{.Metrics.BlockedCases}}</div></div>
  <div class="card"><strong>Deferred work items</strong><div>{{.Metrics.DeferredWorkItems}}</div></div>
  <div class="card"><strong>Pending approvals</strong><div>{{.Metrics.PendingApprovals}}</div></div>
  <div class="card"><strong>Executing sessions</strong><div>{{.Metrics.ExecutingSessions}}</div></div>
  <div class="card"><strong>Failed executions</strong><div>{{.Metrics.FailedExecutions}}</div></div>
</div>
<h2>Actor trust distribution</h2>
<table><thead><tr><th>Trust level</th><th>Actors</th></tr></thead><tbody>
{{range .Metrics.ActorTrustDistribution}}<tr><td>{{.Level}}</td><td>{{.Count}}</td></tr>{{else}}<tr><td colspan="2">No actors found.</td></tr>{{end}}
</tbody></table>
<h2>Actor trust state</h2>
<table><thead><tr><th>Actor</th><th>Role</th><th>Trust</th><th>Autonomy</th><th>Successes</th><th>Failures</th><th>Compensations</th></tr></thead><tbody>
{{range .Actors}}<tr><td>{{.ActorID}}</td><td>{{.Role}}</td><td>{{if .TrustLevel}}{{.TrustLevel}}{{else}}-{{end}}</td><td>{{if .AutonomyTier}}{{.AutonomyTier}}{{else}}-{{end}}</td><td>{{.SuccessCount}}</td><td>{{.FailureCount}}</td><td>{{.CompensationCount}}</td></tr>{{else}}<tr><td colspan="7">No actors found.</td></tr>{{end}}
</tbody></table>
<p class="muted">Refresh the page after approving work to watch the timeline continue.</p>
{{end}}`

const demoCasesTemplate = `{{define "body"}}
<table><thead><tr><th>Case ID</th><th>Operational type</th><th>Status</th><th>System state</th><th>Route / site reference</th><th>Domain reason</th><th>Blocking / deferred reason</th><th>Pending approval</th></tr></thead><tbody>
{{range .Cases}}<tr><td><a href="/demo/cases/{{.CaseID}}">{{.CaseID}}</a></td><td>{{.TypeLabel}}</td><td>{{.Status}}</td><td><span class="pill">{{.StateBadge}}</span></td><td>{{if .RouteRef}}Route {{.RouteRef}}{{if .ReferenceLine}} · {{.ReferenceLine}}{{end}}{{else if .ReferenceLine}}{{.ReferenceLine}}{{else}}-{{end}}</td><td>{{if .DomainReason}}{{.DomainReason}}{{else}}-{{end}}</td><td>{{if .BlockingOrDeferred}}{{.BlockingOrDeferred}}{{else}}-{{end}}</td><td>{{if gt .PendingApprovalCount 0}}<span class="pill">pending ({{.PendingApprovalCount}})</span>{{else}}-{{end}}</td></tr>{{else}}<tr><td colspan="8">No cases found.</td></tr>{{end}}
</tbody></table>
{{end}}`

const demoCaseDetailTemplate = `{{define "body"}}
{{if .Page.DomainContext.Title}}<div class="domain"><strong>{{.Page.DomainContext.Title}}</strong><p>{{.Page.DomainContext.IncidentSummary}}</p><p class="muted">{{.Page.DomainContext.TimelineDescription}}</p></div>{{end}}
{{if .Page.ErrorMessage}}<p class="muted">Last action error: {{.Page.ErrorMessage}}</p>{{end}}
<h2>Incident summary</h2>
<table><tbody>
<tr><th>Case ID</th><td>{{.Page.Case.CaseID}}</td></tr>
<tr><th>Operational type</th><td>{{if .Page.DomainContext.CaseTypeLabel}}{{.Page.DomainContext.CaseTypeLabel}}{{else}}{{.Page.Case.Kind}}{{end}}</td></tr>
<tr><th>Status</th><td>{{.Page.Case.Status}}</td></tr>
<tr><th>Acknowledged</th><td>{{if .Page.Case.Acknowledged}}{{fmtTimePtr .Page.Case.AcknowledgedAt}}{{else}}not yet{{end}}</td></tr>
<tr><th>Correlation ID</th><td>{{.Page.Case.CorrelationID}}</td></tr>
<tr><th>Subject</th><td>{{.Page.Case.SubjectRef}}</td></tr>
<tr><th>Route</th><td>{{if .Page.DomainContext.RouteID}}{{.Page.DomainContext.RouteID}}{{else}}-{{end}}</td></tr>
<tr><th>Container site</th><td>{{if .Page.DomainContext.ContainerSiteID}}{{.Page.DomainContext.ContainerSiteID}}{{else}}-{{end}}</td></tr>
<tr><th>Carrier</th><td>{{if .Page.DomainContext.CarrierID}}{{.Page.DomainContext.CarrierID}}{{else}}-{{end}}</td></tr>
<tr><th>Incident source</th><td>{{if .Page.DomainContext.IncidentSource}}{{.Page.DomainContext.IncidentSource}}{{else}}-{{end}}</td></tr>
<tr><th>Operational reason</th><td>{{if .Page.DomainContext.ReasonLabel}}{{.Page.DomainContext.ReasonLabel}}{{else}}-{{end}}</td></tr>
</tbody></table>
{{if .Page.WorkItem}}
<h2 class="section-title">Control plane state</h2>
<table><tbody>
<tr><th>Work item</th><td>{{.Page.WorkItem.WorkItemID}}</td></tr>
<tr><th>Work type</th><td>{{if .Page.DomainContext.WorkTypeLabel}}{{.Page.DomainContext.WorkTypeLabel}}{{else}}{{.Page.WorkItem.Type}}{{end}}</td></tr>
<tr><th>Queue</th><td>{{.Page.WorkItem.QueueID}}</td></tr>
<tr><th>Latest coordination</th><td>{{.Page.WorkItem.Coordination.DecisionType}} {{if .Page.WorkItem.Coordination.Reason}}— {{.Page.WorkItem.Coordination.Reason}}{{end}}</td></tr>
<tr><th>Latest policy</th><td>{{.Page.WorkItem.PolicyApproval.Outcome}} {{if .Page.DomainContext.ApprovalLabel}}— {{.Page.DomainContext.ApprovalLabel}}{{else if .Page.WorkItem.PolicyApproval.Reason}}— {{.Page.WorkItem.PolicyApproval.Reason}}{{end}}</td></tr>
<tr><th>Latest approval state</th><td>{{if .Page.WorkItem.PolicyApproval.ApprovalRequestID}}{{.Page.WorkItem.PolicyApproval.ApprovalRequestStatus}} ({{.Page.WorkItem.PolicyApproval.ApprovalRequestID}}){{else}}-{{end}}</td></tr>
<tr><th>Latest execution state</th><td>{{if .Page.WorkItem.Execution.SessionID}}{{.Page.WorkItem.Execution.Status}} ({{.Page.WorkItem.Execution.SessionID}}){{else}}not started{{end}}</td></tr>
</tbody></table>
{{if and .Page.WorkItem.PolicyApproval.ApprovalRequestID (eq .Page.WorkItem.PolicyApproval.ApprovalRequestStatus "pending")}}
<div class="actions"><form method="post" action="/demo/approvals/{{.Page.WorkItem.PolicyApproval.ApprovalRequestID}}/approve"><input type="hidden" name="redirect" value="/demo/cases/{{.Page.Case.CaseID}}"><button type="submit">Approve</button></form><form method="post" action="/demo/approvals/{{.Page.WorkItem.PolicyApproval.ApprovalRequestID}}/reject"><input type="hidden" name="redirect" value="/demo/cases/{{.Page.Case.CaseID}}"><button type="submit">Reject</button></form></div>
{{end}}
{{end}}
<h2 class="section-title">Operator workbench</h2>
<div class="actions"><form method="post" action="/demo/cases/{{.Page.Case.CaseID}}/acknowledge"><button type="submit">Acknowledge</button></form><form method="post" action="/demo/cases/{{.Page.Case.CaseID}}/recoordinate"><button type="submit">Request re-coordination</button></form></div>
<form method="post" action="/demo/cases/{{.Page.Case.CaseID}}/notes"><p><label>Operator note<br><textarea name="text" rows="3" cols="80"></textarea></label></p><button type="submit">Add note</button></form>
<form method="post" action="/demo/cases/{{.Page.Case.CaseID}}/external-input"><p><label>External source<br><input name="source"></label></p><p><label>External input<br><textarea name="text" rows="3" cols="80"></textarea></label></p><button type="submit">Record external input</button></form>
{{if .Page.Case.OperatorNotes}}<h3>Operator notes</h3><table><thead><tr><th>Recorded at</th><th>Note</th></tr></thead><tbody>{{range .Page.Case.OperatorNotes}}<tr><td>{{fmtTime .RecordedAt}}</td><td>{{.Text}}</td></tr>{{end}}</tbody></table>{{end}}
{{if .Page.Case.ExternalInputs}}<h3>External inputs</h3><table><thead><tr><th>Recorded at</th><th>Source</th><th>Text</th></tr></thead><tbody>{{range .Page.Case.ExternalInputs}}<tr><td>{{fmtTime .RecordedAt}}</td><td>{{.Source}}</td><td>{{.Text}}</td></tr>{{end}}</tbody></table>{{end}}
<h2 class="section-title">Timeline</h2>
<table><thead><tr><th>Occurred at</th><th>Domain event</th><th>Control plane step</th><th>Status</th><th>Payload</th></tr></thead><tbody>
{{range .Page.Timeline}}<tr><td>{{fmtTime .OccurredAt}}</td><td>{{.DomainTitle}}{{if .DomainDetail}}<div class="muted">{{.DomainDetail}}</div>{{end}}</td><td>{{.Step}}</td><td>{{if .Status}}{{.Status}}{{else}}-{{end}}</td><td><pre>{{payload .Payload}}</pre></td></tr>{{else}}<tr><td colspan="5">No timeline entries found.</td></tr>{{end}}
</tbody></table>
{{end}}`

const demoApprovalsTemplate = `{{define "body"}}
<table><thead><tr><th>Approval</th><th>Case</th><th>Work item</th><th>Requested role</th><th>Reason</th><th>Created</th><th>Action</th></tr></thead><tbody>
{{range .Approvals}}<tr><td>{{.ApprovalRequestID}}</td><td><a href="/demo/cases/{{.CaseID}}">{{.CaseID}}</a></td><td>{{.WorkItemID}}</td><td>{{if .RequestedFromRole}}{{.RequestedFromRole}}{{else}}-{{end}}</td><td>{{if .PolicyApproval.Reason}}{{.PolicyApproval.Reason}}{{else}}{{.Coordination.Reason}}{{end}}</td><td>{{fmtTime .CreatedAt}}</td><td class="actions"><form method="post" action="/demo/approvals/{{.ApprovalRequestID}}/approve"><button type="submit">Approve</button></form><form method="post" action="/demo/approvals/{{.ApprovalRequestID}}/reject"><button type="submit">Reject</button></form></td></tr>{{else}}<tr><td colspan="7">No pending approvals.</td></tr>{{end}}
</tbody></table>
{{end}}`
