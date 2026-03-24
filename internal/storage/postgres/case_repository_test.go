package postgres

import (
	"encoding/json"
	"testing"
	"time"

	"kalita/internal/caseruntime"
)

type fakeScanner struct {
	values []any
	err    error
}

func (s fakeScanner) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *string:
			*d = s.values[i].(string)
		case *time.Time:
			*d = s.values[i].(time.Time)
		case *[]byte:
			*d = append([]byte(nil), s.values[i].([]byte)...)
		default:
			panic("unexpected scan destination")
		}
	}
	return nil
}

func TestMarshalAndScanCaseRoundTrip(t *testing.T) {
	t.Parallel()

	input := caseruntime.Case{
		ID:            "case-1",
		Kind:          "workflow.action",
		Status:        string(caseruntime.CaseOpen),
		Title:         "Case title",
		SubjectRef:    "test.WorkflowTask/rec-1",
		CorrelationID: "corr-1",
		OpenedAt:      time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 3, 24, 11, 0, 0, 0, time.UTC),
		OwnerQueueID:  "queue-1",
		CurrentPlanID: "plan-1",
		Attributes:    map[string]any{"priority": "high"},
	}

	metadata, err := marshalCaseMetadata(input)
	if err != nil {
		t.Fatalf("marshalCaseMetadata error = %v", err)
	}
	if !json.Valid(metadata) {
		t.Fatalf("metadata is not valid json: %s", string(metadata))
	}

	got, err := scanCase(fakeScanner{values: []any{
		input.ID,
		input.Status,
		input.OpenedAt,
		input.UpdatedAt,
		metadata,
	}})
	if err != nil {
		t.Fatalf("scanCase error = %v", err)
	}

	if got.ID != input.ID || got.Kind != input.Kind || got.SubjectRef != input.SubjectRef || got.CorrelationID != input.CorrelationID {
		t.Fatalf("scanCase identity = %#v", got)
	}
	if got.Attributes["priority"] != "high" {
		t.Fatalf("scanCase attributes = %#v", got.Attributes)
	}
}
