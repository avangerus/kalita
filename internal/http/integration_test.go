package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kalita/internal/app"
	"kalita/internal/demo"
	"kalita/internal/integrations/aisotkhody"

	"github.com/gin-gonic/gin"
)

func TestIntegrationIncidentEndpointCreatesCaseAndTimeline(t *testing.T) {
	result := bootstrapIntegrationApp(t)
	r := gin.New()
	api := r.Group("/api")
	registerIntegrationRoutes(api, result.IntegrationService, nil)
	registerOperatorRoutes(api, result.ControlPlane)

	body := `{"external_id":"ext-incident-1","source":"gps","route_id":"R-42","container_site":"SITE-9","timestamp":"2026-03-23T12:34:00Z","payload":{"severity":"high"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/integration/incidents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("POST /api/integration/incidents status=%d body=%s", w.Code, w.Body.String())
	}

	var ingest map[string]any
	decode(t, w, &ingest)
	caseID := ingest["case_id"].(string)
	if caseID == "" {
		t.Fatalf("ingest payload = %#v", ingest)
	}

	caseReq := httptest.NewRequest(http.MethodGet, "/api/operator/cases/"+caseID, nil)
	caseW := httptest.NewRecorder()
	r.ServeHTTP(caseW, caseReq)
	if caseW.Code != http.StatusOK {
		t.Fatalf("GET case status=%d body=%s", caseW.Code, caseW.Body.String())
	}
	var overview map[string]any
	decode(t, caseW, &overview)
	if overview["kind"] != "container_incident_detected" {
		t.Fatalf("overview = %#v", overview)
	}

	timelineReq := httptest.NewRequest(http.MethodGet, "/api/operator/cases/"+caseID+"/timeline", nil)
	timelineW := httptest.NewRecorder()
	r.ServeHTTP(timelineW, timelineReq)
	if timelineW.Code != http.StatusOK {
		t.Fatalf("GET timeline status=%d body=%s", timelineW.Code, timelineW.Body.String())
	}
	var timeline []map[string]any
	decode(t, timelineW, &timeline)
	if !timelineHasStep(timeline, "incident_detected") {
		t.Fatalf("timeline = %#v", timeline)
	}
	if !timelineHasStep(timeline, "work_item_created") {
		t.Fatalf("timeline = %#v", timeline)
	}
}

func TestIntegrationIncidentEndpointIsIdempotentAndDemoCompatible(t *testing.T) {
	gin.SetMode(gin.TestMode)
	demoResult, err := demo.RunAISOtkhodyDemoScenario(t.Context())
	if err != nil {
		t.Fatalf("RunAISOtkhodyDemoScenario error = %v", err)
	}
	r := gin.New()
	api := r.Group("/api")
	registerIntegrationRoutes(api, demoResult.IntegrationService, nil)
	registerOperatorRoutes(api, demoResult.ControlPlane)

	body := `{"external_id":"ext-incident-demo-1","source":"photo","route_id":"R-777","container_site":"SITE-77","timestamp":"2026-03-23T13:00:00Z","payload":{"photo_id":"ph-1"}}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/integration/incidents", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if i == 0 && w.Code != http.StatusAccepted {
			t.Fatalf("first POST status=%d body=%s", w.Code, w.Body.String())
		}
		if i == 1 && w.Code != http.StatusOK {
			t.Fatalf("second POST status=%d body=%s", w.Code, w.Body.String())
		}
	}

	casesReq := httptest.NewRequest(http.MethodGet, "/api/operator/cases", nil)
	casesW := httptest.NewRecorder()
	r.ServeHTTP(casesW, casesReq)
	if casesW.Code != http.StatusOK {
		t.Fatalf("GET cases status=%d body=%s", casesW.Code, casesW.Body.String())
	}
	var cases []map[string]any
	decode(t, casesW, &cases)
	count := 0
	for _, item := range cases {
		if strings.Contains(item["subject_ref"].(string), "R-777") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("cases = %#v", cases)
	}
}

func bootstrapIntegrationApp(t *testing.T) *app.BootstrapResult {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("filepath.Abs repoRoot error = %v", err)
	}
	cfg := map[string]any{"port": "8080", "dslDir": filepath.Join(repoRoot, "dsl"), "enumsDir": filepath.Join(repoRoot, "reference", "enums"), "dbUrl": "", "autoMigrate": false, "blobDriver": "local", "filesRoot": filepath.Join(t.TempDir(), "uploads"), "demoMode": false, "persistenceEnabled": false}
	payload, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal config error = %v", err)
	}
	if err := os.WriteFile(cfgPath, payload, 0o644); err != nil {
		t.Fatalf("WriteFile config error = %v", err)
	}
	result, err := app.Bootstrap(cfgPath)
	if err != nil {
		t.Fatalf("Bootstrap error = %v", err)
	}
	return result
}

func timelineHasStep(items []map[string]any, want string) bool {
	for _, item := range items {
		if item["step"] == want {
			return true
		}
	}
	return false
}

func TestIntegrationIncidentEndpointRejectsMalformedInput(t *testing.T) {
	result := bootstrapIntegrationApp(t)
	r := gin.New()
	api := r.Group("/api")
	registerIntegrationRoutes(api, result.IntegrationService, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/integration/incidents", strings.NewReader(`{"external_id":"","timestamp":"`+time.Now().UTC().Format(time.RFC3339)+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAISIngestEndpointReturnsBatchResult(t *testing.T) {
	r := gin.New()
	api := r.Group("/api")
	registerIntegrationRoutes(api, nil, stubAISIngestionService{
		result: aisotkhody.IngestBatchResult{Date: "2026-03-20", Fetched: 2, Ingested: 1, Duplicates: 1},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/integrations/ais/ingest", strings.NewReader(`{"date":"2026-03-20"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var payload map[string]any
	decode(t, w, &payload)
	if payload["date"] != "2026-03-20" || payload["duplicates"].(float64) != 1 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestAISIngestEndpointRejectsInvalidDate(t *testing.T) {
	r := gin.New()
	api := r.Group("/api")
	registerIntegrationRoutes(api, nil, stubAISIngestionService{})

	req := httptest.NewRequest(http.MethodPost, "/api/integrations/ais/ingest", strings.NewReader(`{"date":"20-03-2026"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

type stubAISIngestionService struct {
	result aisotkhody.IngestBatchResult
	err    error
}

func (s stubAISIngestionService) IngestDate(_ context.Context, _ time.Time) (aisotkhody.IngestBatchResult, error) {
	return s.result, s.err
}

func (s stubAISIngestionService) IngestNow(context.Context) (aisotkhody.IngestBatchResult, error) {
	return s.result, s.err
}

func (stubAISIngestionService) Start(context.Context) {}
