package aisotkhody

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"kalita/internal/eventcore"
	"kalita/internal/integration"
)

type EventInjector interface {
	IngestExternalEvent(context.Context, integration.ExternalEvent) (integration.IngestResult, error)
}

type IngestionService interface {
	IngestDate(context.Context, time.Time) (IngestBatchResult, error)
	IngestNow(context.Context) (IngestBatchResult, error)
	Start(context.Context)
}

type IngestionConfig struct {
	ScheduleEnabled bool
	Interval        time.Duration
	LookbackDays    int
}

type IngestBatchResult struct {
	Date       string   `json:"date"`
	Fetched    int      `json:"fetched"`
	Ingested   int      `json:"ingested"`
	Duplicates int      `json:"duplicates"`
	Errors     []string `json:"errors,omitempty"`
}

type AisIngestionService struct {
	fetcher  DataFetcher
	mapper   AisEventMapper
	injector EventInjector
	clock    eventcore.Clock
	logger   *log.Logger
	config   IngestionConfig
}

func NewIngestionService(fetcher DataFetcher, injector EventInjector, clock eventcore.Clock, logger *log.Logger, cfg IngestionConfig) *AisIngestionService {
	if clock == nil {
		clock = eventcore.RealClock{}
	}
	if logger == nil {
		logger = log.Default()
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 15 * time.Minute
	}
	return &AisIngestionService{
		fetcher:  fetcher,
		mapper:   AisEventMapper{},
		injector: injector,
		clock:    clock,
		logger:   logger,
		config:   cfg,
	}
}

func (s *AisIngestionService) IngestNow(ctx context.Context) (IngestBatchResult, error) {
	return s.IngestDate(ctx, s.ingestDateForTime(s.clock.Now()))
}

func (s *AisIngestionService) IngestDate(ctx context.Context, date time.Time) (IngestBatchResult, error) {
	if s == nil {
		return IngestBatchResult{}, fmt.Errorf("ais ingestion service is nil")
	}
	if s.fetcher == nil {
		return IngestBatchResult{}, fmt.Errorf("ais fetcher is nil")
	}
	if s.injector == nil {
		return IngestBatchResult{}, fmt.Errorf("ais injector is nil")
	}

	targetDate := truncateDateUTC(date)
	result := IngestBatchResult{Date: targetDate.Format("2006-01-02")}

	events, err := s.fetcher.FetchMissedPickups(ctx, targetDate)
	if err != nil {
		return result, err
	}
	result.Fetched = len(events)

	for _, pickup := range events {
		mapped, err := s.mapper.MapPickupEvent(pickup)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", strings.TrimSpace(pickup.ExternalID), err))
			continue
		}
		ingestResult, err := s.injector.IngestExternalEvent(ctx, integration.ExternalEvent{
			ExternalID:     strings.TrimSpace(pickup.ExternalID),
			Source:         mapped.Source,
			EventType:      mapped.Type,
			OccurredAt:     mapped.OccurredAt.UTC(),
			CorrelationID:  mapped.CorrelationID,
			TargetRef:      aisTargetRef(pickup),
			EventPayload:   cloneMap(mapped.Payload),
			CommandPayload: aisCommandPayload(pickup),
			PlanInput:      aisPlanInput(pickup),
		})
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", strings.TrimSpace(pickup.ExternalID), err))
			continue
		}
		if ingestResult.Duplicate {
			result.Duplicates++
			continue
		}
		result.Ingested++
		s.logger.Printf("AIS event injected external_id=%s case_id=%s event_type=%s occurred_at=%s", strings.TrimSpace(pickup.ExternalID), ingestResult.Case.ID, mapped.Type, mapped.OccurredAt.UTC().Format(time.RFC3339))
	}

	return result, nil
}

func (s *AisIngestionService) Start(ctx context.Context) {
	if s == nil || !s.config.ScheduleEnabled {
		return
	}
	go func() {
		ticker := time.NewTicker(s.config.Interval)
		defer ticker.Stop()

		if _, err := s.IngestNow(ctx); err != nil {
			s.logger.Printf("AIS scheduled ingest failed: %v", err)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := s.IngestNow(ctx); err != nil {
					s.logger.Printf("AIS scheduled ingest failed: %v", err)
				}
			}
		}
	}()
}

func (s *AisIngestionService) ingestDateForTime(now time.Time) time.Time {
	return truncateDateUTC(now).AddDate(0, 0, -s.config.LookbackDays)
}

func truncateDateUTC(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func aisTargetRef(event PickupEvent) string {
	return fmt.Sprintf("integration/ais/%s/%s", strings.TrimSpace(event.RouteID), strings.TrimSpace(event.ContainerSite))
}

func aisCommandPayload(event PickupEvent) map[string]any {
	return map[string]any{
		"external_id":    strings.TrimSpace(event.ExternalID),
		"source":         AISSource,
		"route_id":       strings.TrimSpace(event.RouteID),
		"container_site": strings.TrimSpace(event.ContainerSite),
		"status":         strings.TrimSpace(event.Status),
		"reason":         strings.TrimSpace(event.Reason),
		"pickup_date":    event.PickupDate.UTC(),
		"missed_at":      event.MissedAt.UTC(),
		"payload":        cloneMap(event.Payload),
	}
}

func aisPlanInput(event PickupEvent) map[string]any {
	return map[string]any{
		"reason": fmt.Sprintf("AIS missed pickup %s for route %s", strings.TrimSpace(event.ExternalID), strings.TrimSpace(event.RouteID)),
		"actions": []any{
			map[string]any{
				"type": "external_incident_followup",
				"params": map[string]any{
					"external_id":    strings.TrimSpace(event.ExternalID),
					"source":         AISSource,
					"route_id":       strings.TrimSpace(event.RouteID),
					"container_site": strings.TrimSpace(event.ContainerSite),
					"status":         strings.TrimSpace(event.Status),
					"reason":         strings.TrimSpace(event.Reason),
				},
			},
		},
	}
}
