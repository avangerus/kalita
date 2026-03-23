package demo

import (
	"fmt"
	"strings"

	"kalita/internal/controlplane"
)

type DomainCaseContext struct {
	ScenarioKey         string
	Title               string
	CaseTypeLabel       string
	WorkTypeLabel       string
	ReasonLabel         string
	ApprovalLabel       string
	IncidentSummary     string
	OperationalType     string
	RouteID             string
	CarrierID           string
	ContainerSiteID     string
	ContainerID         string
	District            string
	Zone                string
	YardID              string
	IncidentSource      string
	ControlPlaneState   string
	ReferenceLine       string
	TimelineDescription string
}

type DomainTimelineEntry struct {
	Title       string
	Description string
}

func BuildDomainCaseContext(overview controlplane.CaseOverview, workItem *controlplane.WorkItemOverview, timeline []controlplane.TimelineEntry) DomainCaseContext {
	ctx := DomainCaseContext{ControlPlaneState: overview.Status}
	if overview.Kind != "missed_container_pickup_review" {
		return ctx
	}

	meta := metadataFromTimeline(timeline)
	ctx.ScenarioKey = "ais-otkhody"
	ctx.Title = "Missed Container Pickup Review"
	ctx.CaseTypeLabel = "Missed Pickup"
	ctx.WorkTypeLabel = "Fact Reconciliation"
	ctx.ReasonLabel = stringValue(meta["incident_reason"])
	ctx.ApprovalLabel = "Supervisor review required"
	ctx.OperationalType = "Route completed with expected container left unserviced"
	ctx.RouteID = stringValue(meta["route_id"])
	ctx.CarrierID = stringValue(meta["carrier_id"])
	ctx.ContainerSiteID = stringValue(meta["container_site_id"])
	ctx.ContainerID = stringValue(meta["container_id"])
	ctx.District = stringValue(meta["district"])
	ctx.Zone = stringValue(meta["zone"])
	ctx.YardID = stringValue(meta["yard_id"])
	ctx.IncidentSource = stringValue(meta["incident_source"])
	ctx.IncidentSummary = fmt.Sprintf(
		"Route %s completed, but container site %s remained unserviced after %s was detected.",
		fallback(ctx.RouteID, "-"),
		fallback(ctx.ContainerSiteID, "-"),
		strings.ToLower(fallback(ctx.ReasonLabel, "a fact mismatch")),
	)
	ctx.ReferenceLine = strings.Trim(
		strings.Join([]string{prefixed("Carrier", ctx.CarrierID), prefixed("District", ctx.District), prefixed("Zone", ctx.Zone), prefixed("Yard", ctx.YardID)}, " · "),
		" ·",
	)
	ctx.TimelineDescription = "Universal control-plane events rendered with AIS Otkhody incident labels across a deterministic multi-case workload."

	if workItem != nil {
		ctx.ControlPlaneState = fmt.Sprintf("case %s · coordination %s · policy %s", overview.Status, workItem.Coordination.DecisionType, workItem.PolicyApproval.Outcome)
	}
	return ctx
}

func BuildDomainTimelineEntry(caseKind string, entry controlplane.TimelineEntry) DomainTimelineEntry {
	if caseKind != "missed_container_pickup_review" {
		return DomainTimelineEntry{Title: entry.Step, Description: entry.Status}
	}

	title := map[string]string{
		"incident_detected":      "Incident detected",
		"case_created":           "Review case opened",
		"work_item_created":      "Reconciliation task created",
		"coordination_decided":   "Follow-up coordination performed",
		"policy_decided":         "Policy gate evaluated",
		"approval_requested":     "Supervisor approval requested",
		"approval_granted":       "Approval granted",
		"approval_rejected":      "Approval rejected",
		"execution_started":      "Execution started",
		"execution_succeeded":    "Execution succeeded",
		"execution_failed":       "Execution failed",
		"compensation_completed": "Compensation completed",
		"trust_updated":          "Trust updated",
		"escalation_waiting":     "Escalated for supervisor capacity",
	}[entry.Step]
	if title == "" {
		title = entry.Step
	}
	description := strings.TrimSpace(entry.Status)
	if entry.Step == "incident_detected" {
		description = stringValue(entry.Payload["incident_source"])
	}
	if entry.Step == "escalation_waiting" {
		description = "Long-waiting work remained queued after escalation."
	}
	if entry.Step == "trust_updated" {
		description = fmt.Sprintf("Actor trust is now %s.", stringValue(entry.Payload["trust_level"]))
	}
	return DomainTimelineEntry{Title: title, Description: description}
}

func metadataFromTimeline(timeline []controlplane.TimelineEntry) map[string]any {
	for _, entry := range timeline {
		if entry.Step == "incident_detected" && len(entry.Payload) > 0 {
			return entry.Payload
		}
	}
	return aisScenarioMetadata()
}

func prefixed(label string, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return fmt.Sprintf("%s %s", label, value)
}

func fallback(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}
