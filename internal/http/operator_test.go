package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"kalita/internal/capability"
	"kalita/internal/caseruntime"
	"kalita/internal/controlplane"
	"kalita/internal/employee"
	"kalita/internal/executionruntime"
	"kalita/internal/policy"
	"kalita/internal/profile"
	"kalita/internal/proposal"
	"kalita/internal/trust"
	"kalita/internal/workplan"

	"github.com/gin-gonic/gin"
)

func TestOperatorEndpointsReturnAggregatedJSON(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	svc := controlplaneSeededService(t)
	r := gin.New()
	api := r.Group("/api")
	registerOperatorRoutes(api, svc)

	for _, tc := range []struct {
		path  string
		check func(*testing.T, *httptest.ResponseRecorder)
	}{
		{path: "/api/operator/cases", check: func(t *testing.T, rec *httptest.ResponseRecorder) {
			var payload []map[string]any
			decode(t, rec, &payload)
			if len(payload) != 1 || payload[0]["case_id"] != "case-1" {
				t.Fatalf("payload = %#v", payload)
			}
		}},
		{path: "/api/operator/work-items/work-1", check: func(t *testing.T, rec *httptest.ResponseRecorder) {
			var payload map[string]any
			decode(t, rec, &payload)
			if payload["work_item_id"] != "work-1" {
				t.Fatalf("payload = %#v", payload)
			}
			coord := payload["coordination"].(map[string]any)
			if coord["decision_type"] != "defer" {
				t.Fatalf("coordination = %#v", coord)
			}
		}},
		{path: "/api/operator/approvals", check: func(t *testing.T, rec *httptest.ResponseRecorder) {
			var payload []map[string]any
			decode(t, rec, &payload)
			if len(payload) != 1 || payload[0]["approval_request_id"] != "approval-1" {
				t.Fatalf("payload = %#v", payload)
			}
		}},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", tc.path, w.Code, w.Body.String())
		}
		tc.check(t, w)
	}
}

func TestOperatorEndpointReturnsNotFound(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	registerOperatorRoutes(api, controlplaneSeededService(t))

	req := httptest.NewRequest(http.MethodGet, "/api/operator/actors/missing", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func controlplaneSeededService(t *testing.T) controlplane.Service {
	t.Helper()
	ctx := context.Background()
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	queueRepo := workplan.NewInMemoryQueueRepository()
	coordRepo := workplan.NewInMemoryCoordinationRepository()
	policyRepo := policy.NewInMemoryRepository()
	proposalRepo := proposal.NewInMemoryRepository()
	directory := employee.NewInMemoryDirectory()
	trustRepo := trust.NewInMemoryRepository()
	profileRepo := profile.NewInMemoryRepository()
	capRepo := capability.NewInMemoryRepository()
	execRepo := executionruntime.NewInMemoryExecutionRepository()
	wal := executionruntime.NewInMemoryWAL()
	base := time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC)

	mustNoErr(t, caseRepo.Save(ctx, caseruntime.Case{ID: "case-1", Kind: "workflow.action", Status: "open", CorrelationID: "corr-1", SubjectRef: "subject-1", OpenedAt: base, UpdatedAt: base}))
	mustNoErr(t, queueRepo.SaveQueue(ctx, workplan.WorkQueue{ID: "queue-1", Name: "Ops"}))
	mustNoErr(t, queueRepo.SaveWorkItem(ctx, workplan.WorkItem{ID: "work-1", CaseID: "case-1", QueueID: "queue-1", Type: "workflow.action", Status: "open", PlanID: "plan-1", AssignedEmployeeID: "actor-1", CreatedAt: base, UpdatedAt: base}))
	mustNoErr(t, coordRepo.SaveDecision(ctx, workplan.CoordinationDecision{ID: "coord-1", WorkItemID: "work-1", CaseID: "case-1", QueueID: "queue-1", DecisionType: workplan.CoordinationDefer, Priority: 2, Reason: "awaiting approval", CreatedAt: base.Add(time.Minute)}))
	mustNoErr(t, policyRepo.SaveDecision(ctx, policy.PolicyDecision{ID: "policy-1", CoordinationDecisionID: "coord-1", CaseID: "case-1", WorkItemID: "work-1", QueueID: "queue-1", Outcome: policy.PolicyRequireApproval, Reason: "review", CreatedAt: base.Add(2 * time.Minute)}))
	mustNoErr(t, policyRepo.SaveApprovalRequest(ctx, policy.ApprovalRequest{ID: "approval-1", CoordinationDecisionID: "coord-1", PolicyDecisionID: "policy-1", CaseID: "case-1", WorkItemID: "work-1", QueueID: "queue-1", Status: policy.ApprovalPending, CreatedAt: base.Add(3 * time.Minute)}))
	mustNoErr(t, proposalRepo.Save(ctx, proposal.Proposal{ID: "proposal-1", Type: proposal.ProposalTypeActionIntent, Status: proposal.ProposalCompiled, ActorID: "actor-1", CaseID: "case-1", WorkItemID: "work-1", Justification: "ready", ActionPlanID: "plan-compiled", CreatedAt: base.Add(4 * time.Minute), UpdatedAt: base.Add(4 * time.Minute)}))
	mustNoErr(t, directory.SaveEmployee(ctx, employee.DigitalEmployee{ID: "actor-1", Role: "operator", Enabled: true, QueueMemberships: []string{"queue-1"}}))
	mustNoErr(t, trustRepo.Save(ctx, trust.TrustProfile{ActorID: "actor-1", TrustLevel: trust.TrustMedium, AutonomyTier: trust.AutonomySupervised, UpdatedAt: base.Add(5 * time.Minute)}))
	mustNoErr(t, profileRepo.SaveProfile(ctx, profile.CompetencyProfile{ID: "profile-1", ActorID: "actor-1", Name: "Operator", MaxComplexity: 3}))
	mustNoErr(t, capRepo.SaveCapability(ctx, capability.Capability{ID: "cap-1", Code: "workflow.execute", Level: 1}))
	mustNoErr(t, capRepo.AssignCapability(ctx, capability.ActorCapability{ActorID: "actor-1", CapabilityID: "cap-1", Level: 1}))
	mustNoErr(t, execRepo.SaveSession(ctx, executionruntime.ExecutionSession{ID: "exec-1", WorkItemID: "work-1", Status: executionruntime.ExecutionSessionFailed, CurrentStepIndex: 1, FailureReason: "waiting", CreatedAt: base.Add(6 * time.Minute), UpdatedAt: base.Add(6 * time.Minute)}))
	mustNoErr(t, wal.Append(ctx, executionruntime.WALRecord{ID: "wal-1", ExecutionSessionID: "exec-1", ActionID: "action-1", Type: executionruntime.WALStepResult, CreatedAt: base.Add(6 * time.Minute)}))

	return controlplane.NewService(caseRepo, queueRepo, coordRepo, policyRepo, proposalRepo, directory, trustRepo, profileRepo, capRepo, execRepo, wal)
}

func decode(t *testing.T, rec *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), target); err != nil {
		t.Fatalf("json.Unmarshal error = %v body=%s", err, rec.Body.String())
	}
}

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
