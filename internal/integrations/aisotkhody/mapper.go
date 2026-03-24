package aisotkhody

import (
	"fmt"
	"strings"

	"kalita/internal/eventcore"
)

const (
	AISSource              = "ais_otkhody"
	AISPickupEventType     = "missed_container_pickup_review"
	aisIncidentCorrelation = "incident:"
)

// AisEventMapper converts normalized AIS pickup records into domain events.
type AisEventMapper struct{}

func (AisEventMapper) MapPickupEvent(event PickupEvent) (eventcore.Event, error) {
	if err := validatePickupEvent(event); err != nil {
		return eventcore.Event{}, err
	}

	payload := cloneMap(event.Payload)
	if payload == nil {
		payload = make(map[string]any, 8)
	}
	payload["external_id"] = strings.TrimSpace(event.ExternalID)
	payload["route_id"] = strings.TrimSpace(event.RouteID)
	payload["container_site"] = strings.TrimSpace(event.ContainerSite)
	payload["container_site_id"] = strings.TrimSpace(event.ContainerSite)
	payload["status"] = strings.TrimSpace(event.Status)
	payload["reason"] = strings.TrimSpace(event.Reason)
	payload["incident_reason"] = strings.TrimSpace(event.Reason)
	payload["pickup_date"] = event.PickupDate.UTC()
	payload["missed_at"] = event.MissedAt.UTC()
	payload["source"] = AISSource
	payload["incident_source"] = AISSource

	return eventcore.Event{
		Type:          AISPickupEventType,
		OccurredAt:    event.MissedAt.UTC(),
		Source:        AISSource,
		CorrelationID: aisIncidentCorrelation + strings.TrimSpace(event.ExternalID),
		Payload:       payload,
	}, nil
}

func validatePickupEvent(event PickupEvent) error {
	switch {
	case strings.TrimSpace(event.ExternalID) == "":
		return fmt.Errorf("external_id is required")
	case strings.TrimSpace(event.RouteID) == "":
		return fmt.Errorf("route_id is required")
	case strings.TrimSpace(event.ContainerSite) == "":
		return fmt.Errorf("container_site is required")
	case event.PickupDate.IsZero():
		return fmt.Errorf("pickup_date is required")
	case event.MissedAt.IsZero():
		return fmt.Errorf("missed_at is required")
	default:
		return nil
	}
}
