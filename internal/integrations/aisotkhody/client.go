package aisotkhody

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const defaultTimeout = 15 * time.Second

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewClient(cfg Config) *Client {
	return NewClientWithHTTPClient(cfg, &http.Client{Timeout: defaultTimeout})
}

func NewClientWithHTTPClient(cfg Config, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(cfg.APIURL), "/"),
		apiKey:     strings.TrimSpace(cfg.APIKey),
		httpClient: httpClient,
	}
}

func NewClientFromEnv() (*Client, error) {
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return NewClient(cfg), nil
}

func (c *Client) FetchMissedPickups(ctx context.Context, date time.Time) ([]PickupEvent, error) {
	if strings.TrimSpace(c.baseURL) == "" {
		return nil, fmt.Errorf("ais api url is empty")
	}
	if strings.TrimSpace(c.apiKey) == "" {
		return nil, fmt.Errorf("ais api key is empty")
	}

	reqURL, err := buildMissedPickupsURL(c.baseURL, date)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build AIS request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request AIS missed pickups: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read AIS response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("AIS API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	events, err := ParsePickupEvents(body)
	if err != nil {
		return nil, fmt.Errorf("decode AIS response: %w", err)
	}
	return events, nil
}

func buildMissedPickupsURL(baseURL string, date time.Time) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse AIS API url: %w", err)
	}
	parsed.Path = path.Join(parsed.Path, "missed-pickups")
	query := parsed.Query()
	query.Set("date", date.Format("2006-01-02"))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func ParsePickupEvents(raw []byte) ([]PickupEvent, error) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}

	items, ok := unwrapPickupItems(payload)
	if !ok {
		return nil, fmt.Errorf("AIS response does not contain a pickup list")
	}

	events := make([]PickupEvent, 0, len(items))
	for idx, item := range items {
		event, err := normalizePickupEvent(item)
		if err != nil {
			return nil, fmt.Errorf("pickup[%d]: %w", idx, err)
		}
		events = append(events, event)
	}
	return events, nil
}

func unwrapPickupItems(payload any) ([]map[string]any, bool) {
	switch root := payload.(type) {
	case []any:
		return toObjectSlice(root)
	case map[string]any:
		for _, key := range []string{"data", "items", "results", "events", "pickups"} {
			if nested, ok := root[key]; ok {
				if out, ok := unwrapPickupItems(nested); ok {
					return out, true
				}
			}
		}
		return nil, false
	default:
		return nil, false
	}
}

func toObjectSlice(items []any) ([]map[string]any, bool) {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		out = append(out, obj)
	}
	return out, true
}

func normalizePickupEvent(item map[string]any) (PickupEvent, error) {
	externalID := firstString(item, "external_id", "id", "event_id", "pickup_id", "uuid")
	if externalID == "" {
		return PickupEvent{}, fmt.Errorf("missing external id")
	}

	pickupDate, err := firstTime(item, "pickup_date", "planned_date", "date")
	if err != nil {
		return PickupEvent{}, fmt.Errorf("pickup date: %w", err)
	}
	missedAt, err := firstTime(item, "missed_at", "missed_pickup_at", "timestamp", "created_at", "event_time")
	if err != nil {
		return PickupEvent{}, fmt.Errorf("missed at: %w", err)
	}

	return PickupEvent{
		ExternalID:    externalID,
		RouteID:       firstString(item, "route_id", "route", "route_code"),
		ContainerSite: firstString(item, "container_site", "container_site_id", "site_id", "point_id", "location"),
		Status:        firstString(item, "status", "state"),
		Reason:        firstString(item, "reason", "comment", "description"),
		PickupDate:    pickupDate.UTC(),
		MissedAt:      missedAt.UTC(),
		Payload:       cloneMap(item),
	}, nil
}

func firstString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := item[key]
		if !ok {
			continue
		}
		switch value := raw.(type) {
		case string:
			value = strings.TrimSpace(value)
			if value != "" {
				return value
			}
		case float64:
			return strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.0f", value), ".0"), "."))
		}
	}
	return ""
}

func firstTime(item map[string]any, keys ...string) (time.Time, error) {
	for _, key := range keys {
		raw, ok := item[key]
		if !ok {
			continue
		}
		ts, err := parseTimeValue(raw)
		if err == nil {
			return ts, nil
		}
	}
	return time.Time{}, fmt.Errorf("missing supported time value in %v", keys)
}

func parseTimeValue(raw any) (time.Time, error) {
	switch value := raw.(type) {
	case string:
		value = strings.TrimSpace(value)
		for _, layout := range []string{
			time.RFC3339,
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
			"2006-01-02",
		} {
			if ts, err := time.Parse(layout, value); err == nil {
				return ts, nil
			}
		}
	case float64:
		return time.Unix(int64(value), 0), nil
	}
	return time.Time{}, fmt.Errorf("unsupported time value %T", raw)
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}
