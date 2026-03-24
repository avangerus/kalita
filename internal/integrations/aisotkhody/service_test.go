package aisotkhody_test

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kalita/internal/app"
	"kalita/internal/eventcore"
	"kalita/internal/integration"
	"kalita/internal/integrations/aisotkhody"
)

func TestAisIngestionServiceIngestsFixturesIdempotentlyAndLogsInjectedEvents(t *testing.T) {
	result := bootstrapAISTestApp(t)
	fetcher, err := aisotkhody.NewMockFetcher("testdata")
	if err != nil {
		t.Fatalf("NewMockFetcher() error = %v", err)
	}
	injector, ok := result.IntegrationService.(*integration.Service)
	if !ok {
		t.Fatalf("integration service has unexpected type %T", result.IntegrationService)
	}
	var logs bytes.Buffer
	service := aisotkhody.NewIngestionService(fetcher, injector, eventcore.RealClock{}, log.New(&logs, "", 0), aisotkhody.IngestionConfig{})

	first, err := service.IngestDate(t.Context(), time.Date(2026, 3, 20, 15, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("first IngestDate() error = %v", err)
	}
	if first.Date != "2026-03-20" || first.Fetched != 2 || first.Ingested != 2 || first.Duplicates != 0 {
		t.Fatalf("first result = %#v", first)
	}
	if len(first.Errors) != 0 {
		t.Fatalf("first errors = %#v", first.Errors)
	}

	second, err := service.IngestDate(t.Context(), time.Date(2026, 3, 20, 18, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("second IngestDate() error = %v", err)
	}
	if second.Fetched != 2 || second.Ingested != 0 || second.Duplicates != 2 {
		t.Fatalf("second result = %#v", second)
	}

	cases, err := result.ControlPlane.ListCases(t.Context())
	if err != nil {
		t.Fatalf("ListCases() error = %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("len(cases) = %d, want 2", len(cases))
	}
	for _, item := range cases {
		if item.Kind != aisotkhody.AISPickupEventType {
			t.Fatalf("case kind = %q, want %q", item.Kind, aisotkhody.AISPickupEventType)
		}
	}
	logOutput := logs.String()
	if !strings.Contains(logOutput, "ais-evt-1001") || !strings.Contains(logOutput, "ais-evt-1002") {
		t.Fatalf("log output = %q", logOutput)
	}
}

func bootstrapAISTestApp(t *testing.T) *app.BootstrapResult {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("filepath.Abs repoRoot error = %v", err)
	}
	cfg := map[string]any{
		"port":               "8080",
		"dslDir":             filepath.Join(repoRoot, "dsl"),
		"enumsDir":           filepath.Join(repoRoot, "reference", "enums"),
		"dbUrl":              "",
		"autoMigrate":        false,
		"blobDriver":         "local",
		"filesRoot":          filepath.Join(t.TempDir(), "uploads"),
		"demoMode":           false,
		"persistenceEnabled": false,
	}
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
