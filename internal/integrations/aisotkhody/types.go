package aisotkhody

import (
	"context"
	"time"
)

// DataFetcher isolates AIS data retrieval from mapping and ingestion logic.
type DataFetcher interface {
	FetchMissedPickups(ctx context.Context, date time.Time) ([]PickupEvent, error)
}

// PickupEvent is a normalized AIS missed-pickup record used by Kalita.
type PickupEvent struct {
	ExternalID    string         `json:"external_id"`
	RouteID       string         `json:"route_id"`
	ContainerSite string         `json:"container_site"`
	Status        string         `json:"status,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	PickupDate    time.Time      `json:"pickup_date"`
	MissedAt      time.Time      `json:"missed_at"`
	Payload       map[string]any `json:"payload,omitempty"`
}
