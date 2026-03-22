package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"kalita/internal/caseruntime"
	"kalita/internal/command"
	"kalita/internal/eventcore"
	"kalita/internal/executioncontrol"
	"kalita/internal/policy"
	"kalita/internal/runtime"
	"kalita/internal/schema"
	"kalita/internal/workplan"

	"github.com/gin-gonic/gin"
)

func TestActionHandlerReturnsProposalAndMetaWorkflow(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandler(storage))
	router.GET("/api/meta/:module/:entity", MetaEntityHandler(storage))

	body := map[string]any{
		"action":         "submit",
		"record_version": 3,
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("POST action status = %d body=%s", w.Code, w.Body.String())
	}
	var actionResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &actionResp); err != nil {
		t.Fatalf("json.Unmarshal(action) error = %v", err)
	}
	if got := actionResp["to"]; got != "InApproval" {
		t.Fatalf("to = %v", got)
	}
	if got := actionResp["version"]; got.(float64) != 3 {
		t.Fatalf("version = %v", got)
	}
	if got := actionResp["committed"]; got != false {
		t.Fatalf("committed = %v", got)
	}
	if got := storage.Data["test.WorkflowTask"][rec.ID].Data["status"]; got != "Draft" {
		t.Fatalf("status mutated to %v", got)
	}
	proposal := actionResp["record"].(map[string]any)
	if got := proposal["status"]; got != "InApproval" {
		t.Fatalf("proposal status = %v", got)
	}

	metaReq := httptest.NewRequest(http.MethodGet, "/api/meta/test/WorkflowTask", nil)
	metaW := httptest.NewRecorder()
	router.ServeHTTP(metaW, metaReq)
	if metaW.Code != http.StatusOK {
		t.Fatalf("meta status = %d body=%s", metaW.Code, metaW.Body.String())
	}
	var metaResp map[string]any
	if err := json.Unmarshal(metaW.Body.Bytes(), &metaResp); err != nil {
		t.Fatalf("json.Unmarshal(meta) error = %v", err)
	}
	workflow := metaResp["workflow"].(map[string]any)
	if workflow["status_field"] != "status" {
		t.Fatalf("meta workflow status_field = %v", workflow["status_field"])
	}
}

func TestActionHandlerReturnsConflictOnStaleVersion(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandler(storage))

	body := map[string]any{"record_version": 2}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestActionHandlerRejectsMissingRecordVersion(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandler(storage))

	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if got := storage.Data["test.WorkflowTask"][rec.ID].Data["status"]; got != "Draft" {
		t.Fatalf("status mutated to %v", got)
	}
}

func TestActionHandlerRejectsIgnoredPayloadFields(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandler(storage))

	body := map[string]any{
		"record_version": 3,
		"payload":        map[string]any{"comment": "ignored before"},
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestExistingPatchCompatibilityStillWorks(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	router := gin.New()
	router.PATCH("/api/:module/:entity/:id", PatchHandler(storage))

	body := map[string]any{"title": "Updated title"}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPatch, "/api/test/WorkflowTask/"+rec.ID, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"3"`)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d body=%s", w.Code, w.Body.String())
	}
	if got := storage.Data["test.WorkflowTask"][rec.ID].Data["title"]; got != "Updated title" {
		t.Fatalf("title = %v", got)
	}
}

type fakeClock struct{ now time.Time }

func (f fakeClock) Now() time.Time { return f.now }

type fakeIDGenerator struct {
	ids []string
	i   int
}

func (f *fakeIDGenerator) NewID() string {
	if f.i >= len(f.ids) {
		return ""
	}
	id := f.ids[f.i]
	f.i++
	return id
}

func testHTTPWorkflowStorage() (*runtime.Storage, *runtime.Record) {
	entity := &schema.Entity{
		Name:   "WorkflowTask",
		Module: "test",
		Fields: []schema.Field{
			{Name: "title", Type: "string", Options: map[string]string{}},
			{Name: "status", Type: "enum", Enum: []string{"Draft", "InApproval", "Approved"}, Options: map[string]string{}},
		},
		Workflow: &schema.Workflow{
			StatusField: "status",
			Actions: map[string]schema.WorkflowAction{
				"submit": {From: []string{"Draft"}, To: "InApproval"},
			},
		},
	}
	st := runtime.NewStorage([]*schema.Entity{entity}, nil)
	now := time.Now().UTC().Add(-time.Minute)
	rec := &runtime.Record{
		ID:        "rec-1",
		Version:   3,
		CreatedAt: now,
		UpdatedAt: now,
		Data: map[string]interface{}{
			"title":  "Original",
			"status": "Draft",
		},
	}
	st.Data["test.WorkflowTask"] = map[string]*runtime.Record{rec.ID: rec}
	return st, rec
}

type staticCommandBus struct {
	cmd eventcore.Command
}

func (b staticCommandBus) Submit(_ context.Context, _ eventcore.Command) (eventcore.Command, error) {
	return b.cmd, nil
}

type failingCaseService struct {
	err error
}

func (s failingCaseService) ResolveCommand(context.Context, eventcore.Command) (caseruntime.ResolutionResult, error) {
	return caseruntime.ResolutionResult{}, s.err
}

type denyCommandBus struct{}

func (denyCommandBus) Submit(_ context.Context, _ eventcore.Command) (eventcore.Command, error) {
	return eventcore.Command{}, errors.New("command denied")
}

type failingWorkService struct {
	err error
}

func (s failingWorkService) IntakeCommand(context.Context, caseruntime.ResolutionResult) (workplan.IntakeResult, error) {
	return workplan.IntakeResult{}, s.err
}

type failingPlanner struct {
	err error
}

func (p failingPlanner) EnsurePlanForWorkItem(context.Context, workplan.WorkQueue, workplan.WorkItem, string) (workplan.DailyPlan, bool, error) {
	return workplan.DailyPlan{}, false, p.err
}

type failingCoordinator struct{ err error }

func (f failingCoordinator) CoordinateWorkItem(context.Context, workplan.WorkItem) (workplan.CoordinationDecision, error) {
	return workplan.CoordinationDecision{}, f.err
}

type staticPolicyService struct {
	decision policy.PolicyDecision
	approval *policy.ApprovalRequest
	err      error
}

func (s staticPolicyService) EvaluateAndRecord(context.Context, workplan.CoordinationDecision) (policy.PolicyDecision, *policy.ApprovalRequest, error) {
	return s.decision, s.approval, s.err
}

type staticConstraintsService struct {
	constraints executioncontrol.ExecutionConstraints
	err         error
	calls       int
}

func (s *staticConstraintsService) CreateAndRecord(context.Context, workplan.CoordinationDecision, policy.PolicyDecision) (executioncontrol.ExecutionConstraints, error) {
	s.calls++
	return s.constraints, s.err
}

func TestActionHandlerResolvesCaseBeforeLegacyFlow(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	eventLog := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 14, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"cmd-1", "corr-1", "exec-1", "admission-event-1", "case-1", "case-event-1", "work-1", "work-event-1", "plan-1", "plan-event-1", "followup-event-1"}}
	commandBus := command.NewService(eventLog, command.PassThroughAdmissionPolicy{}, clock, ids)
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	caseService := caseruntime.NewService(caseruntime.NewResolver(caseRepo, clock, ids), eventLog, clock, ids)
	queueRepo := workplan.NewInMemoryQueueRepository()
	if err := queueRepo.SaveQueue(context.Background(), workplan.WorkQueue{ID: "default-intake", AllowedCaseKinds: []string{"workflow.action"}}); err != nil {
		t.Fatalf("SaveQueue error = %v", err)
	}
	planRepo := workplan.NewInMemoryPlanRepository()
	planner := workplan.NewPlanner(planRepo, eventLog, clock, ids)
	coordinationRepo := workplan.NewInMemoryCoordinationRepository()
	coordinator := workplan.NewCoordinator(coordinationRepo, eventLog, clock, ids)
	workService := workplan.NewService(queueRepo, workplan.NewRouter(queueRepo, "default-intake"), planner, coordinator, eventLog, clock, ids)

	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandlerWithServices(storage, commandBus, caseService, workService, nil, nil))

	body := map[string]any{"record_version": 3}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}

	storedCase, err := caseService.ResolveCommand(context.Background(), eventcore.Command{
		ID: "cmd-followup", CorrelationID: "corr-1", ExecutionID: "exec-followup", Type: "workflow.action", TargetRef: "test.WorkflowTask/" + rec.ID,
	})
	if err != nil {
		t.Fatalf("followup ResolveCommand error = %v", err)
	}
	if !storedCase.Existed || storedCase.Case.ID != "case-1" {
		t.Fatalf("followup result = %#v", storedCase)
	}
	caseByID, ok, err := caseRepo.GetByID(context.Background(), "case-1")
	if err != nil {
		t.Fatalf("GetByID error = %v", err)
	}
	if !ok || caseByID.SubjectRef != "test.WorkflowTask/"+rec.ID {
		t.Fatalf("stored case = %#v ok=%v", caseByID, ok)
	}
	_, executionEvents, err := eventLog.ListByCorrelation(context.Background(), "corr-1")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(executionEvents) < 5 {
		t.Fatalf("execution events len = %d, want at least 5", len(executionEvents))
	}
	if executionEvents[0].Step != "command_admission" || executionEvents[0].Status != "admitted" {
		t.Fatalf("first execution event = %#v", executionEvents[0])
	}
	if executionEvents[1].Step != "case_resolution" || executionEvents[1].Status != "opened_new" {
		t.Fatalf("second execution event = %#v", executionEvents[1])
	}
	if executionEvents[2].Step != "work_item_intake" || executionEvents[2].Status != "created" {
		t.Fatalf("third execution event = %#v", executionEvents[2])
	}
	if executionEvents[3].Step != "daily_plan_intake" || executionEvents[3].Status != "attached" {
		t.Fatalf("fourth execution event = %#v", executionEvents[3])
	}
	if executionEvents[4].Step != "coordination_decision" || executionEvents[4].Status != "selected" {
		t.Fatalf("fifth execution event = %#v", executionEvents[4])
	}
	workItems, err := queueRepo.ListWorkItemsByCase(context.Background(), "case-1")
	if err != nil {
		t.Fatalf("ListWorkItemsByCase error = %v", err)
	}
	if len(workItems) != 1 || workItems[0].QueueID != "default-intake" || workItems[0].PlanID != "plan-1" {
		t.Fatalf("workItems = %#v", workItems)
	}
	plan, ok, err := planRepo.GetPlan(context.Background(), "plan-1")
	if err != nil {
		t.Fatalf("GetPlan error = %v", err)
	}
	if !ok || len(plan.WorkItemIDs) != 1 || plan.WorkItemIDs[0] != "work-1" {
		t.Fatalf("plan = %#v ok=%v", plan, ok)
	}
	decisions, err := coordinationRepo.ListByWorkItem(context.Background(), "work-1")
	if err != nil {
		t.Fatalf("ListByWorkItem error = %v", err)
	}
	if len(decisions) != 1 || decisions[0].Outcome != workplan.CoordinationSelected {
		t.Fatalf("coordination decisions = %#v", decisions)
	}
	if got := storage.Data["test.WorkflowTask"][rec.ID].Data["status"]; got != "Draft" {
		t.Fatalf("legacy flow mutated status to %v", got)
	}
}

func TestActionHandlerReturnsValidationErrorWhenCaseResolutionFails(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandlerWithServices(storage, staticCommandBus{cmd: eventcore.Command{ID: "cmd-1", CorrelationID: "corr-1", ExecutionID: "exec-1", Type: "workflow.action", TargetRef: "test.WorkflowTask/" + rec.ID}}, failingCaseService{err: errors.New("case resolution failed")}, nil, nil, nil))

	body := map[string]any{"record_version": 3}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if got := storage.Data["test.WorkflowTask"][rec.ID].Data["status"]; got != "Draft" {
		t.Fatalf("status mutated to %v", got)
	}
}

func TestActionHandlerReturnsValidationErrorWhenWorkItemIntakeFails(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	eventLog := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 14, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"cmd-1", "corr-1", "exec-1", "admission-event-1", "case-1", "case-event-1"}}
	commandBus := command.NewService(eventLog, command.PassThroughAdmissionPolicy{}, clock, ids)
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	caseService := caseruntime.NewService(caseruntime.NewResolver(caseRepo, clock, ids), eventLog, clock, ids)
	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandlerWithServices(storage, commandBus, caseService, failingWorkService{err: errors.New("work intake failed")}, nil, nil))

	body := map[string]any{"record_version": 3}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if got := storage.Data["test.WorkflowTask"][rec.ID].Data["status"]; got != "Draft" {
		t.Fatalf("status mutated to %v", got)
	}
}

func TestActionHandlerReturnsValidationErrorWhenCoordinationFails(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	eventLog := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 14, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"cmd-1", "corr-1", "exec-1", "admission-event-1", "case-1", "case-event-1", "work-1", "work-event-1", "plan-1", "plan-event-1"}}
	commandBus := command.NewService(eventLog, command.PassThroughAdmissionPolicy{}, clock, ids)
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	caseService := caseruntime.NewService(caseruntime.NewResolver(caseRepo, clock, ids), eventLog, clock, ids)
	queueRepo := workplan.NewInMemoryQueueRepository()
	if err := queueRepo.SaveQueue(context.Background(), workplan.WorkQueue{ID: "default-intake", AllowedCaseKinds: []string{"workflow.action"}}); err != nil {
		t.Fatalf("SaveQueue error = %v", err)
	}
	planner := workplan.NewPlanner(workplan.NewInMemoryPlanRepository(), eventLog, clock, ids)
	workService := workplan.NewService(queueRepo, workplan.NewRouter(queueRepo, "default-intake"), planner, failingCoordinator{err: errors.New("coordination failed")}, eventLog, clock, ids)

	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandlerWithServices(storage, commandBus, caseService, workService, nil, nil))

	body := map[string]any{"record_version": 3}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if got := storage.Data["test.WorkflowTask"][rec.ID].Data["status"]; got != "Draft" {
		t.Fatalf("status mutated to %v", got)
	}
}

func TestActionHandlerPolicyAllowContinuesLegacyFlow(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	eventLog := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 14, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"cmd-1", "corr-1", "exec-1", "admission-event-1", "case-1", "case-event-1", "work-1", "work-event-1", "plan-1", "plan-event-1", "coord-1", "coord-event-1"}}
	commandBus := command.NewService(eventLog, command.PassThroughAdmissionPolicy{}, clock, ids)
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	caseService := caseruntime.NewService(caseruntime.NewResolver(caseRepo, clock, ids), eventLog, clock, ids)
	queueRepo := workplan.NewInMemoryQueueRepository()
	if err := queueRepo.SaveQueue(context.Background(), workplan.WorkQueue{ID: "default-intake", AllowedCaseKinds: []string{"workflow.action"}}); err != nil {
		t.Fatalf("SaveQueue error = %v", err)
	}
	planner := workplan.NewPlanner(workplan.NewInMemoryPlanRepository(), eventLog, clock, ids)
	workService := workplan.NewService(queueRepo, workplan.NewRouter(queueRepo, "default-intake"), planner, workplan.NewCoordinator(workplan.NewInMemoryCoordinationRepository(), eventLog, clock, ids), eventLog, clock, ids)
	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandlerWithServices(storage, commandBus, caseService, workService, staticPolicyService{
		decision: policy.PolicyDecision{ID: "pol-1", Outcome: policy.PolicyAllow, Reason: "allowed"},
	}, nil))

	body := map[string]any{"record_version": 3}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestActionHandlerPolicyRequireApprovalStopsBeforeLegacyFlow(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	eventLog := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 14, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"cmd-1", "corr-1", "exec-1", "admission-event-1", "case-1", "case-event-1", "work-1", "work-event-1", "plan-1", "plan-event-1", "coord-1", "coord-event-1"}}
	commandBus := command.NewService(eventLog, command.PassThroughAdmissionPolicy{}, clock, ids)
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	caseService := caseruntime.NewService(caseruntime.NewResolver(caseRepo, clock, ids), eventLog, clock, ids)
	queueRepo := workplan.NewInMemoryQueueRepository()
	if err := queueRepo.SaveQueue(context.Background(), workplan.WorkQueue{ID: "default-intake", AllowedCaseKinds: []string{"workflow.action"}}); err != nil {
		t.Fatalf("SaveQueue error = %v", err)
	}
	planner := workplan.NewPlanner(workplan.NewInMemoryPlanRepository(), eventLog, clock, ids)
	workService := workplan.NewService(queueRepo, workplan.NewRouter(queueRepo, "default-intake"), planner, workplan.NewCoordinator(workplan.NewInMemoryCoordinationRepository(), eventLog, clock, ids), eventLog, clock, ids)

	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandlerWithServices(storage, commandBus, caseService, workService, staticPolicyService{
		decision: policy.PolicyDecision{ID: "policy-1", Outcome: policy.PolicyRequireApproval, Reason: "manager approval required"},
		approval: &policy.ApprovalRequest{ID: "approval-1", Status: policy.ApprovalPending},
	}, nil))

	body := map[string]any{"record_version": 3}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if got := storage.Data["test.WorkflowTask"][rec.ID].Data["status"]; got != "Draft" {
		t.Fatalf("status mutated to %v", got)
	}
}

func TestActionHandlerPolicyDenyStopsBeforeLegacyFlow(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	eventLog := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 14, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"cmd-1", "corr-1", "exec-1", "admission-event-1", "case-1", "case-event-1", "work-1", "work-event-1", "plan-1", "plan-event-1", "coord-1", "coord-event-1"}}
	commandBus := command.NewService(eventLog, command.PassThroughAdmissionPolicy{}, clock, ids)
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	caseService := caseruntime.NewService(caseruntime.NewResolver(caseRepo, clock, ids), eventLog, clock, ids)
	queueRepo := workplan.NewInMemoryQueueRepository()
	if err := queueRepo.SaveQueue(context.Background(), workplan.WorkQueue{ID: "default-intake", AllowedCaseKinds: []string{"workflow.action"}}); err != nil {
		t.Fatalf("SaveQueue error = %v", err)
	}
	planner := workplan.NewPlanner(workplan.NewInMemoryPlanRepository(), eventLog, clock, ids)
	workService := workplan.NewService(queueRepo, workplan.NewRouter(queueRepo, "default-intake"), planner, workplan.NewCoordinator(workplan.NewInMemoryCoordinationRepository(), eventLog, clock, ids), eventLog, clock, ids)

	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandlerWithServices(storage, commandBus, caseService, workService, staticPolicyService{decision: policy.PolicyDecision{ID: "pol-2", Outcome: policy.PolicyDeny, Reason: "blocked by policy"}}, nil))

	body := map[string]any{"record_version": 3}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if got := storage.Data["test.WorkflowTask"][rec.ID].Data["status"]; got != "Draft" {
		t.Fatalf("status mutated to %v", got)
	}
}

func TestActionHandlerReturnsValidationErrorWhenCommandAdmissionFails(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandlerWithCommandBus(storage, denyCommandBus{}))

	body := map[string]any{"record_version": 3}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if got := storage.Data["test.WorkflowTask"][rec.ID].Data["status"]; got != "Draft" {
		t.Fatalf("status mutated to %v", got)
	}
}

func TestCreateActionRequestAndGetByID(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandler(storage))
	router.POST("/api/:module/:entity/:id/_actions/:action/requests", CreateActionRequestHandler(storage))
	router.GET("/api/_action_requests/:request_id", GetActionRequestHandler(storage))

	body := map[string]any{"record_version": 3}
	raw, _ := json.Marshal(body)
	createReq := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit/requests", bytes.NewReader(raw))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	router.ServeHTTP(createW, createReq)

	if createW.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createW.Code, createW.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createW.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal(create) error = %v", err)
	}
	requestID, _ := created["id"].(string)
	if requestID == "" {
		t.Fatalf("request id missing: %#v", created)
	}
	if created["entity"] != "test.WorkflowTask" || created["target_id"] != rec.ID {
		t.Fatalf("unexpected request target = %#v", created)
	}
	if created["state"] != "pending" || created["from"] != "Draft" || created["to"] != "InApproval" {
		t.Fatalf("unexpected request transition = %#v", created)
	}
	if got := storage.Data["test.WorkflowTask"][rec.ID].Data["status"]; got != "Draft" {
		t.Fatalf("status mutated to %v", got)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/_action_requests/"+requestID, nil)
	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", getW.Code, getW.Body.String())
	}
	if getW.Body.String() != createW.Body.String() {
		t.Fatalf("get body = %s, want %s", getW.Body.String(), createW.Body.String())
	}
}

func TestCreateActionRequestReusesValidationAndProposalEndpointStaysCompatible(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandler(storage))
	router.POST("/api/:module/:entity/:id/_actions/:action/requests", CreateActionRequestHandler(storage))

	invalidBody, _ := json.Marshal(map[string]any{"record_version": 2})
	invalidReq := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit/requests", bytes.NewReader(invalidBody))
	invalidReq.Header.Set("Content-Type", "application/json")
	invalidW := httptest.NewRecorder()
	router.ServeHTTP(invalidW, invalidReq)
	if invalidW.Code != http.StatusConflict {
		t.Fatalf("invalid create status = %d body=%s", invalidW.Code, invalidW.Body.String())
	}
	if len(storage.ActionRequests) != 0 {
		t.Fatalf("request store mutated after invalid create: %#v", storage.ActionRequests)
	}

	proposalBody, _ := json.Marshal(map[string]any{"record_version": 3})
	proposalReq := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(proposalBody))
	proposalReq.Header.Set("Content-Type", "application/json")
	proposalW := httptest.NewRecorder()
	router.ServeHTTP(proposalW, proposalReq)
	if proposalW.Code != http.StatusOK {
		t.Fatalf("proposal status = %d body=%s", proposalW.Code, proposalW.Body.String())
	}
	var proposalResp map[string]any
	if err := json.Unmarshal(proposalW.Body.Bytes(), &proposalResp); err != nil {
		t.Fatalf("json.Unmarshal(proposal) error = %v", err)
	}
	if proposalResp["committed"] != false {
		t.Fatalf("committed = %v", proposalResp["committed"])
	}
	if len(storage.ActionRequests) != 0 {
		t.Fatalf("proposal endpoint created request unexpectedly: %#v", storage.ActionRequests)
	}
}

func TestActionHandlerReturnsValidationErrorWhenDailyPlanAttachmentFails(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	eventLog := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 14, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"cmd-1", "corr-1", "exec-1", "admission-event-1", "case-1", "case-event-1", "work-1", "work-event-1"}}
	commandBus := command.NewService(eventLog, command.PassThroughAdmissionPolicy{}, clock, ids)
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	caseService := caseruntime.NewService(caseruntime.NewResolver(caseRepo, clock, ids), eventLog, clock, ids)
	queueRepo := workplan.NewInMemoryQueueRepository()
	if err := queueRepo.SaveQueue(context.Background(), workplan.WorkQueue{ID: "default-intake", AllowedCaseKinds: []string{"workflow.action"}}); err != nil {
		t.Fatalf("SaveQueue error = %v", err)
	}
	workService := workplan.NewService(queueRepo, workplan.NewRouter(queueRepo, "default-intake"), failingPlanner{err: errors.New("daily plan failed")}, workplan.NewCoordinator(workplan.NewInMemoryCoordinationRepository(), eventLog, clock, ids), eventLog, clock, ids)

	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandlerWithServices(storage, commandBus, caseService, workService, nil, nil))

	body := map[string]any{"record_version": 3}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if got := storage.Data["test.WorkflowTask"][rec.ID].Data["status"]; got != "Draft" {
		t.Fatalf("status mutated to %v", got)
	}
}

func TestActionHandlerPolicyAllowCreatesConstraintsBeforeLegacyFlow(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	eventLog := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 14, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"cmd-1", "corr-1", "exec-1", "admission-event-1", "case-1", "case-event-1", "work-1", "work-event-1", "plan-1", "plan-event-1", "coord-1", "coord-event-1"}}
	commandBus := command.NewService(eventLog, command.PassThroughAdmissionPolicy{}, clock, ids)
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	caseService := caseruntime.NewService(caseruntime.NewResolver(caseRepo, clock, ids), eventLog, clock, ids)
	queueRepo := workplan.NewInMemoryQueueRepository()
	if err := queueRepo.SaveQueue(context.Background(), workplan.WorkQueue{ID: "default-intake", AllowedCaseKinds: []string{"workflow.action"}}); err != nil {
		t.Fatalf("SaveQueue error = %v", err)
	}
	planner := workplan.NewPlanner(workplan.NewInMemoryPlanRepository(), eventLog, clock, ids)
	workService := workplan.NewService(queueRepo, workplan.NewRouter(queueRepo, "default-intake"), planner, workplan.NewCoordinator(workplan.NewInMemoryCoordinationRepository(), eventLog, clock, ids), eventLog, clock, ids)
	constraintsSvc := &staticConstraintsService{constraints: executioncontrol.ExecutionConstraints{ID: "constraints-1"}}

	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandlerWithServices(storage, commandBus, caseService, workService, staticPolicyService{decision: policy.PolicyDecision{ID: "pol-1", Outcome: policy.PolicyAllow, Reason: "allowed"}}, constraintsSvc))

	body := map[string]any{"record_version": 3}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if constraintsSvc.calls != 1 {
		t.Fatalf("constraints calls = %d", constraintsSvc.calls)
	}
}

func TestActionHandlerPolicyRequireApprovalRecordsConstraintsAndStopsBeforeLegacyFlow(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	eventLog := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 14, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"cmd-1", "corr-1", "exec-1", "admission-event-1", "case-1", "case-event-1", "work-1", "work-event-1", "plan-1", "plan-event-1", "coord-1", "coord-event-1"}}
	commandBus := command.NewService(eventLog, command.PassThroughAdmissionPolicy{}, clock, ids)
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	caseService := caseruntime.NewService(caseruntime.NewResolver(caseRepo, clock, ids), eventLog, clock, ids)
	queueRepo := workplan.NewInMemoryQueueRepository()
	if err := queueRepo.SaveQueue(context.Background(), workplan.WorkQueue{ID: "default-intake", AllowedCaseKinds: []string{"workflow.action"}}); err != nil {
		t.Fatalf("SaveQueue error = %v", err)
	}
	planner := workplan.NewPlanner(workplan.NewInMemoryPlanRepository(), eventLog, clock, ids)
	workService := workplan.NewService(queueRepo, workplan.NewRouter(queueRepo, "default-intake"), planner, workplan.NewCoordinator(workplan.NewInMemoryCoordinationRepository(), eventLog, clock, ids), eventLog, clock, ids)
	constraintsSvc := &staticConstraintsService{constraints: executioncontrol.ExecutionConstraints{ID: "constraints-1"}}

	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandlerWithServices(storage, commandBus, caseService, workService, staticPolicyService{decision: policy.PolicyDecision{ID: "policy-1", Outcome: policy.PolicyRequireApproval, Reason: "manager approval required"}, approval: &policy.ApprovalRequest{ID: "approval-1", Status: policy.ApprovalPending}}, constraintsSvc))

	body := map[string]any{"record_version": 3}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if constraintsSvc.calls != 1 {
		t.Fatalf("constraints calls = %d", constraintsSvc.calls)
	}
	if got := storage.Data["test.WorkflowTask"][rec.ID].Data["status"]; got != "Draft" {
		t.Fatalf("status mutated to %v", got)
	}
}

func TestActionHandlerPolicyDenyDoesNotCreateConstraintsAndStopsBeforeLegacyFlow(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	eventLog := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 14, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"cmd-1", "corr-1", "exec-1", "admission-event-1", "case-1", "case-event-1", "work-1", "work-event-1", "plan-1", "plan-event-1", "coord-1", "coord-event-1"}}
	commandBus := command.NewService(eventLog, command.PassThroughAdmissionPolicy{}, clock, ids)
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	caseService := caseruntime.NewService(caseruntime.NewResolver(caseRepo, clock, ids), eventLog, clock, ids)
	queueRepo := workplan.NewInMemoryQueueRepository()
	if err := queueRepo.SaveQueue(context.Background(), workplan.WorkQueue{ID: "default-intake", AllowedCaseKinds: []string{"workflow.action"}}); err != nil {
		t.Fatalf("SaveQueue error = %v", err)
	}
	planner := workplan.NewPlanner(workplan.NewInMemoryPlanRepository(), eventLog, clock, ids)
	workService := workplan.NewService(queueRepo, workplan.NewRouter(queueRepo, "default-intake"), planner, workplan.NewCoordinator(workplan.NewInMemoryCoordinationRepository(), eventLog, clock, ids), eventLog, clock, ids)
	constraintsSvc := &staticConstraintsService{constraints: executioncontrol.ExecutionConstraints{ID: "constraints-1"}}
	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandlerWithServices(storage, commandBus, caseService, workService, staticPolicyService{decision: policy.PolicyDecision{ID: "pol-2", Outcome: policy.PolicyDeny, Reason: "blocked by policy"}}, constraintsSvc))

	body := map[string]any{"record_version": 3}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if constraintsSvc.calls != 0 {
		t.Fatalf("constraints calls = %d", constraintsSvc.calls)
	}
}

func TestActionHandlerConstraintCreationFailureReturnsValidationError(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	storage, rec := testHTTPWorkflowStorage()
	eventLog := eventcore.NewInMemoryEventLog()
	clock := fakeClock{now: time.Date(2026, 3, 22, 14, 0, 0, 0, time.UTC)}
	ids := &fakeIDGenerator{ids: []string{"cmd-1", "corr-1", "exec-1", "admission-event-1", "case-1", "case-event-1", "work-1", "work-event-1", "plan-1", "plan-event-1", "coord-1", "coord-event-1"}}
	commandBus := command.NewService(eventLog, command.PassThroughAdmissionPolicy{}, clock, ids)
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	caseService := caseruntime.NewService(caseruntime.NewResolver(caseRepo, clock, ids), eventLog, clock, ids)
	queueRepo := workplan.NewInMemoryQueueRepository()
	if err := queueRepo.SaveQueue(context.Background(), workplan.WorkQueue{ID: "default-intake", AllowedCaseKinds: []string{"workflow.action"}}); err != nil {
		t.Fatalf("SaveQueue error = %v", err)
	}
	planner := workplan.NewPlanner(workplan.NewInMemoryPlanRepository(), eventLog, clock, ids)
	workService := workplan.NewService(queueRepo, workplan.NewRouter(queueRepo, "default-intake"), planner, workplan.NewCoordinator(workplan.NewInMemoryCoordinationRepository(), eventLog, clock, ids), eventLog, clock, ids)
	constraintsSvc := &staticConstraintsService{err: errors.New("constraints failed")}

	router := gin.New()
	router.POST("/api/:module/:entity/:id/_actions/:action", ActionHandlerWithServices(storage, commandBus, caseService, workService, staticPolicyService{decision: policy.PolicyDecision{ID: "pol-1", Outcome: policy.PolicyAllow, Reason: "allowed"}}, constraintsSvc))

	body := map[string]any{"record_version": 3}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/test/WorkflowTask/"+rec.ID+"/_actions/submit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if got := storage.Data["test.WorkflowTask"][rec.ID].Data["status"]; got != "Draft" {
		t.Fatalf("status mutated to %v", got)
	}
}
