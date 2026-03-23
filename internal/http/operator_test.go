package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"kalita/internal/caseruntime"
	"kalita/internal/controlplane"
	"kalita/internal/employee"
	"kalita/internal/eventcore"
	"kalita/internal/executionruntime"
	"kalita/internal/policy"
	"kalita/internal/proposal"
	"kalita/internal/trust"
	"kalita/internal/workplan"

	"github.com/gin-gonic/gin"
)

func TestOperatorEndpointsReturnJSON(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	service := buildOperatorControlPlane(t)
	router := gin.New()
	router.GET("/api/operator/cases/:id", OperatorCaseDetailHandler(service))
	router.GET("/api/operator/cases/:id/timeline", OperatorCaseTimelineHandler(service))
	router.GET("/api/operator/summary", OperatorSummaryHandler(service))

	for _, tc := range []struct {
		path string
		code int
		key  string
	}{
		{path: "/api/operator/cases/case-1", code: http.StatusOK, key: "case"},
		{path: "/api/operator/cases/case-1/timeline", code: http.StatusOK, key: "entries"},
		{path: "/api/operator/summary", code: http.StatusOK, key: "active_cases"},
		{path: "/api/operator/cases/missing", code: http.StatusNotFound, key: "error"},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		router.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatalf("GET %s status=%d body=%s", tc.path, w.Code, w.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("GET %s json err=%v", tc.path, err)
		}
		if _, ok := payload[tc.key]; !ok {
			t.Fatalf("GET %s payload=%v missing %q", tc.path, payload, tc.key)
		}
	}
}

func buildOperatorControlPlane(t *testing.T) *controlplane.Service {
	t.Helper()
	ctx := context.Background()
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	workRepo := workplan.NewInMemoryQueueRepository()
	coordRepo := workplan.NewInMemoryCoordinationRepository()
	policyRepo := policy.NewInMemoryRepository()
	proposalRepo := proposal.NewInMemoryRepository()
	execRepo := executionruntime.NewInMemoryExecutionRepository()
	employees := employee.NewInMemoryDirectory()
	trustRepo := trust.NewInMemoryRepository()
	log := eventcore.NewInMemoryEventLog()
	base := time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC)

	mustHTTP(t, caseRepo.Save(ctx, caseruntime.Case{ID: "case-1", Kind: "workflow.action", Status: string(caseruntime.CaseOpen), CorrelationID: "corr-1", OpenedAt: base, UpdatedAt: base}))
	mustHTTP(t, workRepo.SaveWorkItem(ctx, workplan.WorkItem{ID: "work-1", CaseID: "case-1", QueueID: "q-1", Status: string(workplan.WorkItemOpen), CreatedAt: base.Add(time.Minute), UpdatedAt: base.Add(time.Minute)}))
	mustHTTP(t, coordRepo.SaveDecision(ctx, workplan.CoordinationDecision{ID: "coord-1", CaseID: "case-1", WorkItemID: "work-1", QueueID: "q-1", DecisionType: workplan.CoordinationDefer, Reason: "manager approval required", CreatedAt: base.Add(2 * time.Minute)}))
	mustHTTP(t, policyRepo.SaveDecision(ctx, policy.PolicyDecision{ID: "policy-1", CoordinationDecisionID: "coord-1", CaseID: "case-1", WorkItemID: "work-1", QueueID: "q-1", Outcome: policy.PolicyRequireApproval, Reason: "manager approval required", CreatedAt: base.Add(3 * time.Minute)}))
	mustHTTP(t, policyRepo.SaveApprovalRequest(ctx, policy.ApprovalRequest{ID: "approval-1", CoordinationDecisionID: "coord-1", PolicyDecisionID: "policy-1", CaseID: "case-1", WorkItemID: "work-1", QueueID: "q-1", Status: policy.ApprovalPending, CreatedAt: base.Add(4 * time.Minute)}))
	mustHTTP(t, proposalRepo.Save(ctx, proposal.Proposal{ID: "proposal-1", CaseID: "case-1", WorkItemID: "work-1", Status: proposal.ProposalDraft, CreatedAt: base.Add(5 * time.Minute), UpdatedAt: base.Add(5 * time.Minute)}))
	mustHTTP(t, execRepo.SaveSession(ctx, executionruntime.ExecutionSession{ID: "exec-1", CaseID: "case-1", WorkItemID: "work-1", Status: executionruntime.ExecutionSessionRunning, CreatedAt: base.Add(6 * time.Minute), UpdatedAt: base.Add(7 * time.Minute)}))
	mustHTTP(t, employees.SaveEmployee(ctx, employee.DigitalEmployee{ID: "actor-1", Enabled: true}))
	mustHTTP(t, trustRepo.Save(ctx, trust.TrustProfile{ActorID: "actor-1", TrustLevel: trust.TrustHigh}))
	mustHTTP(t, log.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{ID: "evt-1", CaseID: "case-1", CorrelationID: "corr-1", OccurredAt: base, Step: "case_resolution", Status: "opened_new", Payload: map[string]any{"command_type": "workflow.action"}}))

	return controlplane.NewService(caseRepo, workRepo, coordRepo, policyRepo, proposalRepo, execRepo, employees, trustRepo, log)
}

func mustHTTP(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
