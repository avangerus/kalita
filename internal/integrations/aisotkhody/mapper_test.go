package aisotkhody

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAisEventMapperMapsPickupEventFromPrimaryFixture(t *testing.T) {
	events := mustParseFixtureEvents(t, "2026-03-20_missed-pickups.json")

	mapper := AisEventMapper{}
	got, err := mapper.MapPickupEvent(events[0])
	if err != nil {
		t.Fatalf("MapPickupEvent() error = %v", err)
	}

	if got.Type != AISPickupEventType {
		t.Fatalf("Type = %q, want %q", got.Type, AISPickupEventType)
	}
	if got.Source != AISSource {
		t.Fatalf("Source = %q, want %q", got.Source, AISSource)
	}
	if got.CorrelationID != "incident:ais-evt-1001" {
		t.Fatalf("CorrelationID = %q", got.CorrelationID)
	}
	if got.OccurredAt.Format(time.RFC3339) != "2026-03-20T06:35:00Z" {
		t.Fatalf("OccurredAt = %s", got.OccurredAt.Format(time.RFC3339))
	}
	if value, _ := got.Payload["route_id"].(string); value != "R-42" {
		t.Fatalf("payload route_id = %q", value)
	}
	if value, _ := got.Payload["container_site_id"].(string); value != "SITE-17" {
		t.Fatalf("payload container_site_id = %q", value)
	}
	if value, _ := got.Payload["incident_reason"].(string); value != "blocked_access" {
		t.Fatalf("payload incident_reason = %q", value)
	}
	if value, _ := got.Payload["incident_source"].(string); value != AISSource {
		t.Fatalf("payload incident_source = %q", value)
	}
	if value, ok := got.Payload["pickup_date"].(time.Time); !ok || value.Format("2006-01-02") != "2026-03-20" {
		t.Fatalf("payload pickup_date = %#v", got.Payload["pickup_date"])
	}
	if value, ok := got.Payload["missed_at"].(time.Time); !ok || value.Format(time.RFC3339) != "2026-03-20T06:35:00Z" {
		t.Fatalf("payload missed_at = %#v", got.Payload["missed_at"])
	}
}

func TestAisEventMapperMapsPickupEventFromAlternateFixture(t *testing.T) {
	events := mustParseFixtureEvents(t, "2026-03-21_missed-pickups.json")

	mapper := AisEventMapper{}
	got, err := mapper.MapPickupEvent(events[0])
	if err != nil {
		t.Fatalf("MapPickupEvent() error = %v", err)
	}

	if got.CorrelationID != "incident:ais-evt-1003" {
		t.Fatalf("CorrelationID = %q", got.CorrelationID)
	}
	if got.OccurredAt.Format(time.RFC3339) != "2026-03-21T08:05:00Z" {
		t.Fatalf("OccurredAt = %s", got.OccurredAt.Format(time.RFC3339))
	}
	if value, _ := got.Payload["route_id"].(string); value != "R-900" {
		t.Fatalf("payload route_id = %q", value)
	}
	if value, _ := got.Payload["container_site"].(string); value != "SITE-33" {
		t.Fatalf("payload container_site = %q", value)
	}
	if value, _ := got.Payload["incident_reason"].(string); value != "snow_cleanup_delay" {
		t.Fatalf("payload incident_reason = %q", value)
	}
	if value, _ := got.Payload["external_id"].(string); value != "ais-evt-1003" {
		t.Fatalf("payload external_id = %q", value)
	}
}

func TestAisEventMapperClonesInputPayload(t *testing.T) {
	mapper := AisEventMapper{}
	input := PickupEvent{
		ExternalID:    "ais-evt-2001",
		RouteID:       "R-501",
		ContainerSite: "SITE-501",
		Status:        "missed",
		Reason:        "blocked_access",
		PickupDate:    time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC),
		MissedAt:      time.Date(2026, 3, 24, 9, 15, 0, 0, time.UTC),
		Payload:       map[string]any{"route_id": "R-501"},
	}

	got, err := mapper.MapPickupEvent(input)
	if err != nil {
		t.Fatalf("MapPickupEvent() error = %v", err)
	}

	got.Payload["route_id"] = "CHANGED"
	if input.Payload["route_id"] != "R-501" {
		t.Fatalf("input payload mutated = %v", input.Payload["route_id"])
	}
}

func TestAisEventMapperRejectsIncompletePickupEvent(t *testing.T) {
	mapper := AisEventMapper{}
	_, err := mapper.MapPickupEvent(PickupEvent{
		ExternalID: "ais-evt-3001",
		PickupDate: time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC),
		MissedAt:   time.Date(2026, 3, 24, 9, 15, 0, 0, time.UTC),
	})
	if err == nil || err.Error() != "route_id is required" {
		t.Fatalf("error = %v, want route_id is required", err)
	}
}

func mustParseFixtureEvents(t *testing.T, name string) []PickupEvent {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", name, err)
	}
	events, err := ParsePickupEvents(raw)
	if err != nil {
		t.Fatalf("ParsePickupEvents(%q) error = %v", name, err)
	}
	return events
}
