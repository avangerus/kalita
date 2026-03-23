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

	caseOverview, err := result.ControlPlane.GetCaseOverview(context.Background(), DemoCaseID)
	if err != nil {
		t.Fatalf("GetCaseOverview error = %v", err)
	}
	if caseOverview.Kind != "container_incident_detected" || caseOverview.CorrelationID != DemoCorrelationID {
		t.Fatalf("caseOverview = %#v", caseOverview)
	}

	workItems, err := result.ControlPlane.ListWorkItems(context.Background())
	if err != nil {
		t.Fatalf("ListWorkItems error = %v", err)
	}
	if len(workItems) != 1 {
		t.Fatalf("workItems = %#v", workItems)
	}
	if workItems[0].Coordination.DecisionType != string(workplan.CoordinationDefer) {
		t.Fatalf("coordination = %#v", workItems[0].Coordination)
	}
	if workItems[0].PolicyApproval.Outcome != string(policy.PolicyRequireApproval) || workItems[0].PolicyApproval.ApprovalRequestID != DemoApprovalRequestID {
		t.Fatalf("policy approval = %#v", workItems[0].PolicyApproval)
	}
	if workItems[0].Execution.SessionID != "" {
		t.Fatalf("expected no execution session, got %#v", workItems[0].Execution)
	}

	approvals, err := result.ControlPlane.GetApprovalInbox(context.Background())
	if err != nil {
		t.Fatalf("GetApprovalInbox error = %v", err)
	}
	if len(approvals) != 1 || approvals[0].ApprovalRequestID != DemoApprovalRequestID {
		t.Fatalf("approvals = %#v", approvals)
	}

	timeline, err := result.ControlPlane.GetCaseTimeline(context.Background(), DemoCaseID)
	if err != nil {
		t.Fatalf("GetCaseTimeline error = %v", err)
	}
	steps := make([]string, 0, len(timeline))
	for _, entry := range timeline {
		steps = append(steps, entry.Step)
	}
	want := []string{"case_created", "work_item_created", "coordination_decided", "policy_decided", "approval_requested"}
	for _, step := range want {
		if !contains(steps, step) {
			t.Fatalf("timeline steps = %#v, missing %q", steps, step)
		}
	}
	if contains(steps, "execution_started") {
		t.Fatalf("timeline unexpectedly contains execution_started: %#v", steps)
	}
	for i := 1; i < len(timeline); i++ {
		if timeline[i].OccurredAt.Before(timeline[i-1].OccurredAt) {
			t.Fatalf("timeline not ordered: %#v", timeline)
		}
	}
	if got := workItems[0].Coordination.Reason; got != "only low-trust actors available: actor-low-1,actor-low-2; defer until stronger trust or supervised release" {
		t.Fatalf("coordination reason = %q", got)
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
