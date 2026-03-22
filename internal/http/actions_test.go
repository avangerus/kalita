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

	"kalita/internal/eventcore"
	"kalita/internal/runtime"
	"kalita/internal/schema"

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

type denyCommandBus struct{}

func (denyCommandBus) Submit(_ context.Context, _ eventcore.Command) (eventcore.Command, error) {
	return eventcore.Command{}, errors.New("command denied")
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
