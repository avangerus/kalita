package aisotkhody

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type MockFetcher struct {
	eventsByDate map[string][]PickupEvent
}

func NewMockFetcher(fixturesDir string) (*MockFetcher, error) {
	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		return nil, fmt.Errorf("read AIS fixtures: %w", err)
	}

	fixtureNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		fixtureNames = append(fixtureNames, entry.Name())
	}
	sort.Strings(fixtureNames)

	eventsByDate := make(map[string][]PickupEvent, len(fixtureNames))
	for _, name := range fixtureNames {
		fullPath := filepath.Join(fixturesDir, name)
		raw, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("read AIS fixture %q: %w", fullPath, err)
		}
		events, err := ParsePickupEvents(raw)
		if err != nil {
			return nil, fmt.Errorf("parse AIS fixture %q: %w", fullPath, err)
		}
		dateKey, err := dateKeyFromFixtureName(name)
		if err != nil {
			return nil, err
		}
		eventsByDate[dateKey] = events
	}

	return &MockFetcher{eventsByDate: eventsByDate}, nil
}

func (m *MockFetcher) FetchMissedPickups(_ context.Context, date time.Time) ([]PickupEvent, error) {
	if m == nil {
		return nil, fmt.Errorf("mock fetcher is nil")
	}
	dateKey := date.UTC().Format("2006-01-02")
	events := m.eventsByDate[dateKey]
	out := make([]PickupEvent, 0, len(events))
	for _, event := range events {
		copied := event
		copied.Payload = cloneMap(event.Payload)
		out = append(out, copied)
	}
	return out, nil
}

func dateKeyFromFixtureName(name string) (string, error) {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	datePart := base
	if idx := strings.Index(base, "_"); idx >= 0 {
		datePart = base[:idx]
	}
	if idx := strings.Index(base, "-missed-pickups"); idx >= 0 {
		datePart = base[:idx]
	}
	if _, err := time.Parse("2006-01-02", datePart); err != nil {
		return "", fmt.Errorf("fixture %q must start with YYYY-MM-DD", name)
	}
	return datePart, nil
}
