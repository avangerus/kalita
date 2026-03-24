package aisotkhody

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClientFetchMissedPickups(t *testing.T) {
	fixturePath := filepath.Join("testdata", "2026-03-20_missed-pickups.json")
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", fixturePath, err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if got := r.URL.Path; got != "/api/missed-pickups" {
			t.Fatalf("path = %s, want /api/missed-pickups", got)
		}
		if got := r.URL.Query().Get("date"); got != "2026-03-20" {
			t.Fatalf("date query = %q", got)
		}
		if got := r.Header.Get("X-API-Key"); got != "top-secret" {
			t.Fatalf("X-API-Key = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	defer server.Close()

	client := NewClient(Config{APIURL: server.URL + "/api", APIKey: "top-secret"})
	events, err := client.FetchMissedPickups(t.Context(), time.Date(2026, 3, 20, 13, 0, 0, 0, time.FixedZone("MSK", 3*60*60)))
	if err != nil {
		t.Fatalf("FetchMissedPickups() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].ExternalID != "ais-evt-1001" {
		t.Fatalf("events[0].ExternalID = %q", events[0].ExternalID)
	}
	if events[1].ContainerSite != "SITE-19" {
		t.Fatalf("events[1].ContainerSite = %q", events[1].ContainerSite)
	}
}

func TestParsePickupEventsSupportsAlternateEnvelope(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "2026-03-21_missed-pickups.json"))
	if err != nil {
		t.Fatalf("ReadFile fixture error = %v", err)
	}

	events, err := ParsePickupEvents(raw)
	if err != nil {
		t.Fatalf("ParsePickupEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].RouteID != "R-900" {
		t.Fatalf("RouteID = %q", events[0].RouteID)
	}
	if got := events[0].PickupDate.Format("2006-01-02"); got != "2026-03-21" {
		t.Fatalf("PickupDate = %q", got)
	}
}
