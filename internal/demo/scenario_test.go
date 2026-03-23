package demo

import (
	"context"
	"testing"

	"kalita/internal/policy"
	"kalita/internal/workplan"
)

func TestRunDemoScenarioProducesDeferredApprovalPathVisibleInControlPlane(t *testing.T) {
	t.Parallel()

	result, err := RunDemoScenario(context.Background())
	if err != nil {
		t.Fatalf("RunDemoScenario error = %v", err)
	}

	summary, err := result.ControlPlane.GetSummary(context.Background())
	if err != nil {
		t.Fatalf("GetSummary error = %v", err)
	}
	if summary.OpenCaseCount != 1 || summary.WorkItemCount != 1 || summary.ApprovalPendingCount != 1 || summary.BlockedOrDeferredCount != 1 {
		t.Fatalf("summary = %#v", summary)
	}

	cases, err := result.ControlPlane.ListCases(context.Background())
	if err != nil {
		t.Fatalf("ListCases error = %v", err)
	}
	if len(cases) != 1 || cases[0].CaseID != DemoCaseID {
		t.Fatalf("cases = %#v", cases)
	}
}

func TestRunAISOtkhodyDemoScenarioBootstrapsDomainContextAndApprovalFlow(t *testing.T) {
	t.Parallel()

	result, err := RunAISOtkhodyDemoScenario(context.Background())
	if err != nil {
		t.Fatalf("RunAISOtkhodyDemoScenario error = %v", err)
	}

	caseOverview, err := result.ControlPlane.GetCaseOverview(context.Background(), AISDemoCaseID)
	if err != nil {
		t.Fatalf("GetCaseOverview error = %v", err)
	}
	if caseOverview.Kind != "missed_container_pickup_review" || caseOverview.CorrelationID != AISDemoCorrelationID {
		t.Fatalf("caseOverview = %#v", caseOverview)
	}

	workItem, err := result.ControlPlane.GetWorkItemOverview(context.Background(), AISDemoWorkItemID)
	if err != nil {
		t.Fatalf("GetWorkItemOverview error = %v", err)
	}
	if workItem.Coordination.DecisionType != string(workplan.CoordinationDefer) {
		t.Fatalf("coordination = %#v", workItem.Coordination)
	}
	if workItem.PolicyApproval.Outcome != string(policy.PolicyRequireApproval) || workItem.PolicyApproval.ApprovalRequestID != AISDemoApprovalRequestID {
		t.Fatalf("policy approval = %#v", workItem.PolicyApproval)
	}
	if workItem.Execution.SessionID != "" {
		t.Fatalf("expected no execution session, got %#v", workItem.Execution)
	}

	events, _, err := result.EventLog.ListByCorrelation(context.Background(), AISDemoCorrelationID)
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected recorded domain events")
	}
	for key, want := range map[string]string{"route_id": "R-2048", "container_site_id": "SITE-881", "incident_source": "photo/GPS", "incident_reason": "Photo/GPS mismatch"} {
		if got := events[0].Payload[key]; got != want {
			t.Fatalf("event payload[%s] = %v, want %q", key, got, want)
		}
	}

	approved, err := result.ControlPlane.ApproveApprovalRequest(context.Background(), AISDemoApprovalRequestID)
	if err != nil {
		t.Fatalf("ApproveApprovalRequest error = %v", err)
	}
	if approved.Status != string(policy.ApprovalApproved) {
		t.Fatalf("approved = %#v", approved)
	}

	updatedWorkItem, err := result.ControlPlane.GetWorkItemOverview(context.Background(), AISDemoWorkItemID)
	if err != nil {
		t.Fatalf("GetWorkItemOverview after approval error = %v", err)
	}
	if updatedWorkItem.PolicyApproval.ApprovalRequestStatus != string(policy.ApprovalApproved) {
		t.Fatalf("updatedWorkItem = %#v", updatedWorkItem)
	}

	updatedTimeline, err := result.ControlPlane.GetCaseTimeline(context.Background(), AISDemoCaseID)
	if err != nil {
		t.Fatalf("GetCaseTimeline after approval error = %v", err)
	}
	steps := make([]string, 0, len(updatedTimeline))
	for _, entry := range updatedTimeline {
		steps = append(steps, entry.Step)
	}
	for _, step := range []string{"approval_requested", "approval_granted", "coordination_decided"} {
		if !contains(steps, step) {
			t.Fatalf("timeline steps = %#v, missing %q", steps, step)
		}
	}
	if countOccurrences(steps, "coordination_decided") < 2 {
		t.Fatalf("timeline steps = %#v", steps)
	}
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func countOccurrences(items []string, target string) int {
	count := 0
	for _, item := range items {
		if item == target {
			count++
		}
	}
	return count
}
