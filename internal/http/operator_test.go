package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kalita/internal/capability"
	"kalita/internal/caseruntime"
	"kalita/internal/controlplane"
	"kalita/internal/employee"
	"kalita/internal/eventcore"
	"kalita/internal/executionruntime"
	"kalita/internal/policy"
	"kalita/internal/profile"
	"kalita/internal/proposal"
	"kalita/internal/trust"
	"kalita/internal/workplan"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestControlPlaneSummaryIncludesQueuePressure(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	api := r.Group("/api")
	registerOperatorRoutes(api, controlplaneSeededService(t))

	req := httptest.NewRequest(http.MethodGet, "/api/controlplane/summary", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var payload map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	queuePressure, ok := payload["queue_pressure"].([]any)
	require.True(t, ok, "queue_pressure missing from payload: %#v", payload)
	require.Len(t, queuePressure, 1)

	first, ok := queuePressure[0].(map[string]any)
	require.True(t, ok, "unexpected queue_pressure item: %#v", queuePressure[0])
	assert.Equal(t, "ops", first["department_id"])
	assert.Equal(t, 1.0, first["work_items_count"])
	assert.Contains(t, first, "pressure_score")
}

func TestOperatorApprovalEndpointsResolveRequestsIdempotently(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	api := r.Group("/api")
	registerOperatorRoutes(api, controlplaneSeededService(t))

	for _, path := range []string{
		"/api/operator/approvals/approval-1/approve",
		"/api/operator/approvals/approval-1/approve",
	} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("POST %s status=%d body=%s", path, w.Code, w.Body.String())
		}
		var payload map[string]any
		decode(t, w, &payload)
		if payload["status"] != "approved" {
			t.Fatalf("payload = %#v", payload)
		}
	}
}

func TestOperatorCaseInputEndpointsRecordGovernedEvents(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	api := r.Group("/api")
	registerOperatorRoutes(api, controlplaneSeededService(t))

	for _, reqSpec := range []struct {
		name string
		path string
		body string
	}{
		{name: "acknowledge", path: "/api/operator/cases/case-1/acknowledge"},
		{name: "note", path: "/api/operator/cases/case-1/notes", body: `{"text":"Carrier confirmed missed pickup due to blocked access"}`},
		{name: "external input", path: "/api/operator/cases/case-1/external-input", body: `{"source":"carrier_report","text":"Access restored, retry allowed"}`},
		{name: "recoordinate", path: "/api/operator/cases/case-1/recoordinate"},
	} {
		req := httptest.NewRequest(http.MethodPost, reqSpec.path, strings.NewReader(reqSpec.body))
		if reqSpec.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", reqSpec.name, w.Code, w.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/operator/cases/case-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET case overview status=%d body=%s", w.Code, w.Body.String())
	}
	var overview map[string]any
	decode(t, w, &overview)
	if overview["acknowledged"] != true {
		t.Fatalf("overview = %#v", overview)
	}
	if notes := overview["operator_notes"].([]any); len(notes) != 1 {
		t.Fatalf("operator_notes = %#v", overview["operator_notes"])
	}
	if inputs := overview["external_inputs"].([]any); len(inputs) != 1 {
		t.Fatalf("external_inputs = %#v", overview["external_inputs"])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/operator/cases/case-1/timeline", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET timeline status=%d body=%s", w.Code, w.Body.String())
	}
	var timeline []map[string]any
	decode(t, w, &timeline)
	steps := map[string]bool{}
	for _, entry := range timeline {
		steps[entry["step"].(string)] = true
	}
	for _, want := range []string{"operator_case_acknowledged", "operator_note_added", "external_input_received", "operator_recoordination_requested", "coordination_decided"} {
		if !steps[want] {
			t.Fatalf("timeline missing %s in %#v", want, timeline)
		}
	}
}

func TestOperatorAcknowledgeIsIdempotent(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	api := r.Group("/api")
	registerOperatorRoutes(api, controlplaneSeededService(t))

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/operator/cases/case-1/acknowledge", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("POST acknowledge #%d status=%d body=%s", i+1, w.Code, w.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/operator/cases/case-1/timeline", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var timeline []map[string]any
	decode(t, w, &timeline)
	count := 0
	for _, entry := range timeline {
		if entry["step"] == "operator_case_acknowledged" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("ack count = %d timeline=%#v", count, timeline)
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
	eventLog := eventcore.NewInMemoryEventLog()
	base := time.Date(2026, 3, 23, 10, 0, 0, 0, time.UTC)

	mustNoErr(t, caseRepo.Save(ctx, caseruntime.Case{ID: "case-1", Kind: "workflow.action", Status: "open", CorrelationID: "corr-1", SubjectRef: "subject-1", OpenedAt: base, UpdatedAt: base}))
	mustNoErr(t, queueRepo.SaveQueue(ctx, workplan.WorkQueue{ID: "queue-1", Name: "Ops", Department: "ops"}))
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

	return controlplane.NewService(caseRepo, queueRepo, coordRepo, policyRepo, proposalRepo, directory, trustRepo, profileRepo, capRepo, execRepo, wal, eventLog, workplan.NewCoordinator(coordRepo, eventLog, nil, nil))
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
