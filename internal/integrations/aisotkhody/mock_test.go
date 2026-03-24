package aisotkhody

import (
	"path/filepath"
	"testing"
	"time"
)

func TestMockFetcherLoadsFixtures(t *testing.T) {
	fetcher, err := NewMockFetcher("testdata")
	if err != nil {
		t.Fatalf("NewMockFetcher() error = %v", err)
	}

	events, err := fetcher.FetchMissedPickups(t.Context(), time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchMissedPickups() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}

	events[0].Payload["route_id"] = "CHANGED"
	reloaded, err := fetcher.FetchMissedPickups(t.Context(), time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchMissedPickups() second call error = %v", err)
	}
	if got := reloaded[0].Payload["route_id"]; got != "R-42" {
		t.Fatalf("reloaded payload route_id = %v", got)
	}
}

func TestDateKeyFromFixtureName(t *testing.T) {
	got, err := dateKeyFromFixtureName(filepath.Base("2026-03-20_missed-pickups.json"))
	if err != nil {
		t.Fatalf("dateKeyFromFixtureName() error = %v", err)
	}
	if got != "2026-03-20" {
		t.Fatalf("dateKey = %q", got)
	}
}
