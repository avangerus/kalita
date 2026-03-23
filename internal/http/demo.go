package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"time"

	"kalita/internal/controlplane"

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

func (c *demoOperatorClient) post(path string, out any) error {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(nil))
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
	ActiveCases            int
	BlockedCases           int
	DeferredWorkItems      int
	PendingApprovals       int
	ExecutingSessions      int
	FailedExecutions       int
	ActorTrustDistribution []trustBucket
}

type trustBucket struct {
	Level string
	Count int
}

type caseListRow struct {
	CaseID               string
	Type                 string
	Status               string
	BlockingOrDeferred   string
	PendingApprovalCount int
}

type caseDetailPage struct {
	Case     controlplane.CaseOverview
	WorkItem *controlplane.WorkItemOverview
	Timeline []controlplane.TimelineEntry
}

func registerDemoRoutes(r *gin.Engine) {
	client := newDemoOperatorClient(r)
	r.GET("/demo", func(c *gin.Context) { renderDemoDashboard(c, client) })
	r.GET("/demo/cases", func(c *gin.Context) { renderDemoCases(c, client) })
	r.GET("/demo/cases/:id", func(c *gin.Context) { renderDemoCaseDetail(c, client, c.Param("id")) })
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
	renderDemoHTML(c, "Kalita Demo Console", demoDashboardTemplate, map[string]any{"Metrics": metrics, "Now": time.Now().UTC()})
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
	page := caseDetailPage{Case: overview, Timeline: timeline}
	for _, wi := range workItems {
		if wi.CaseID == caseID {
			copy := wi
			page.WorkItem = &copy
		}
	}
	renderDemoHTML(c, "Case Detail · Kalita Demo Console", demoCaseDetailTemplate, map[string]any{"Page": page})
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
	if err := client.post(fmt.Sprintf("/api/operator/approvals/%s/%s", approvalID, action), &item); err != nil {
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
	metrics := dashboardMetrics{ActiveCases: summary.OpenCaseCount, PendingApprovals: summary.ApprovalPendingCount}
	trustCounts := map[string]int{}
	blockedCaseIDs := map[string]struct{}{}
	for _, wi := range workItems {
		if wi.Coordination.DecisionType == "defer" {
			metrics.DeferredWorkItems++
		}
		if wi.Coordination.DecisionType == "block" {
			blockedCaseIDs[wi.CaseID] = struct{}{}
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
		row := caseListRow{CaseID: item.CaseID, Type: item.Kind, Status: item.Status, PendingApprovalCount: pendingByCase[item.CaseID]}
		reasons := make([]string, 0)
		for _, wi := range workByCase[item.CaseID] {
			if wi.Coordination.Reason != "" {
				reasons = append(reasons, wi.Coordination.Reason)
			}
			if wi.PolicyApproval.Reason != "" {
				reasons = append(reasons, wi.PolicyApproval.Reason)
			}
		}
		row.BlockingOrDeferred = strings.Join(uniqueStrings(reasons), " | ")
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CaseID < rows[j].CaseID })
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
body{font-family:Arial,sans-serif;margin:24px;color:#222}nav a{margin-right:12px}table{border-collapse:collapse;width:100%;margin-top:12px}th,td{border:1px solid #ccc;padding:8px;vertical-align:top;text-align:left} .cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:12px;margin:16px 0}.card{border:1px solid #ccc;padding:12px}.muted{color:#666}.pill{display:inline-block;border:1px solid #999;border-radius:999px;padding:2px 8px;font-size:12px}.actions form{display:inline-block;margin-right:8px}pre{white-space:pre-wrap;word-break:break-word;margin:0}
</style></head><body>
<h1>{{.Title}}</h1>
<nav><a href="/demo">Dashboard</a><a href="/demo/cases">Cases</a><a href="/demo/approvals">Approval Inbox</a></nav>
{{template "body" .}}
</body></html>{{end}}`

const demoDashboardTemplate = `{{define "body"}}
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
<p class="muted">Refresh the page after approving work to watch the timeline continue.</p>
{{end}}`

const demoCasesTemplate = `{{define "body"}}
<table><thead><tr><th>Case ID</th><th>Type</th><th>Status</th><th>Blocking / deferred reason</th><th>Pending approval</th></tr></thead><tbody>
{{range .Cases}}<tr><td><a href="/demo/cases/{{.CaseID}}">{{.CaseID}}</a></td><td>{{.Type}}</td><td>{{.Status}}</td><td>{{if .BlockingOrDeferred}}{{.BlockingOrDeferred}}{{else}}-{{end}}</td><td>{{if gt .PendingApprovalCount 0}}<span class="pill">pending ({{.PendingApprovalCount}})</span>{{else}}-{{end}}</td></tr>{{else}}<tr><td colspan="5">No cases found.</td></tr>{{end}}
</tbody></table>
{{end}}`

const demoCaseDetailTemplate = `{{define "body"}}
<h2>Case header</h2>
<table><tbody>
<tr><th>Case ID</th><td>{{.Page.Case.CaseID}}</td></tr>
<tr><th>Type</th><td>{{.Page.Case.Kind}}</td></tr>
<tr><th>Status</th><td>{{.Page.Case.Status}}</td></tr>
<tr><th>Correlation ID</th><td>{{.Page.Case.CorrelationID}}</td></tr>
<tr><th>Subject</th><td>{{.Page.Case.SubjectRef}}</td></tr>
</tbody></table>
{{if .Page.WorkItem}}
<h2>Latest work item</h2>
<table><tbody>
<tr><th>Work item</th><td>{{.Page.WorkItem.WorkItemID}}</td></tr>
<tr><th>Status</th><td>{{.Page.WorkItem.Status}}</td></tr>
<tr><th>Queue</th><td>{{.Page.WorkItem.QueueID}}</td></tr>
<tr><th>Latest coordination</th><td>{{.Page.WorkItem.Coordination.DecisionType}} {{if .Page.WorkItem.Coordination.Reason}}— {{.Page.WorkItem.Coordination.Reason}}{{end}}</td></tr>
<tr><th>Latest policy</th><td>{{.Page.WorkItem.PolicyApproval.Outcome}} {{if .Page.WorkItem.PolicyApproval.Reason}}— {{.Page.WorkItem.PolicyApproval.Reason}}{{end}}</td></tr>
<tr><th>Latest approval state</th><td>{{if .Page.WorkItem.PolicyApproval.ApprovalRequestID}}{{.Page.WorkItem.PolicyApproval.ApprovalRequestStatus}} ({{.Page.WorkItem.PolicyApproval.ApprovalRequestID}}){{else}}-{{end}}</td></tr>
<tr><th>Latest execution state</th><td>{{if .Page.WorkItem.Execution.SessionID}}{{.Page.WorkItem.Execution.Status}} ({{.Page.WorkItem.Execution.SessionID}}){{else}}not started{{end}}</td></tr>
</tbody></table>
{{if and .Page.WorkItem.PolicyApproval.ApprovalRequestID (eq .Page.WorkItem.PolicyApproval.ApprovalRequestStatus "pending")}}
<div class="actions"><form method="post" action="/demo/approvals/{{.Page.WorkItem.PolicyApproval.ApprovalRequestID}}/approve"><input type="hidden" name="redirect" value="/demo/cases/{{.Page.Case.CaseID}}"><button type="submit">Approve</button></form><form method="post" action="/demo/approvals/{{.Page.WorkItem.PolicyApproval.ApprovalRequestID}}/reject"><input type="hidden" name="redirect" value="/demo/cases/{{.Page.Case.CaseID}}"><button type="submit">Reject</button></form></div>
{{end}}
{{end}}
<h2>Timeline</h2>
<table><thead><tr><th>Occurred at</th><th>Step</th><th>Status</th><th>Payload</th></tr></thead><tbody>
{{range .Page.Timeline}}<tr><td>{{fmtTime .OccurredAt}}</td><td>{{.Step}}</td><td>{{if .Status}}{{.Status}}{{else}}-{{end}}</td><td><pre>{{payload .Payload}}</pre></td></tr>{{else}}<tr><td colspan="4">No timeline entries found.</td></tr>{{end}}
</tbody></table>
{{end}}`

const demoApprovalsTemplate = `{{define "body"}}
<table><thead><tr><th>Approval</th><th>Case</th><th>Work item</th><th>Requested role</th><th>Reason</th><th>Created</th><th>Action</th></tr></thead><tbody>
{{range .Approvals}}<tr><td>{{.ApprovalRequestID}}</td><td><a href="/demo/cases/{{.CaseID}}">{{.CaseID}}</a></td><td>{{.WorkItemID}}</td><td>{{if .RequestedFromRole}}{{.RequestedFromRole}}{{else}}-{{end}}</td><td>{{if .PolicyApproval.Reason}}{{.PolicyApproval.Reason}}{{else}}{{.Coordination.Reason}}{{end}}</td><td>{{fmtTime .CreatedAt}}</td><td class="actions"><form method="post" action="/demo/approvals/{{.ApprovalRequestID}}/approve"><button type="submit">Approve</button></form><form method="post" action="/demo/approvals/{{.ApprovalRequestID}}/reject"><button type="submit">Reject</button></form></td></tr>{{else}}<tr><td colspan="7">No pending approvals.</td></tr>{{end}}
</tbody></table>
{{end}}`
