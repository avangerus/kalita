package postgres

import (
	"encoding/json"
	"testing"
	"time"

	"kalita/internal/actionplan"
	"kalita/internal/workplan"
)

type fakeWorkItemScanner struct {
	values []any
	err    error
}

func (s fakeWorkItemScanner) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *string:
			*d = s.values[i].(string)
		case **string:
			if s.values[i] == nil {
				*d = nil
				continue
			}
			value := s.values[i].(string)
			*d = &value
		case **time.Time:
			if s.values[i] == nil {
				*d = nil
				continue
			}
			value := s.values[i].(time.Time)
			*d = &value
		case *[]byte:
			*d = append([]byte(nil), s.values[i].([]byte)...)
		case *time.Time:
			*d = s.values[i].(time.Time)
		default:
			panic("unexpected scan destination")
		}
	}
	return nil
}

func TestMarshalAndScanWorkItemRoundTrip(t *testing.T) {
	t.Parallel()

	dueAt := time.Date(2026, 3, 24, 12, 30, 0, 0, time.UTC)
	input := workplan.WorkItem{
		ID:                 "wi-1",
		CaseID:             "case-1",
		QueueID:            "queue-1",
		Type:               "workflow.action",
		Status:             "open",
		Priority:           "high",
		Reason:             "review",
		AssignedEmployeeID: "actor-1",
		PlanID:             "plan-1",
		DueAt:              &dueAt,
		ActionPlan: &actionplan.ActionPlan{
			ID:         "plan-1",
			WorkItemID: "wi-1",
			CaseID:     "case-1",
			Reason:     "auto",
			CreatedAt:  time.Date(2026, 3, 24, 11, 0, 0, 0, time.UTC),
			Actions: []actionplan.Action{
				{ID: "act-1", Type: actionplan.ActionType("call_customer"), Params: map[string]any{"attempt": float64(1)}, Reversibility: actionplan.ReversibilityCompensatable, Idempotency: actionplan.IdempotencyConditional},
			},
		},
		CreatedAt: time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 3, 24, 10, 5, 0, 0, time.UTC),
	}

	actionPlan, err := marshalWorkItemActionPlan(input.ActionPlan)
	if err != nil {
		t.Fatalf("marshalWorkItemActionPlan error = %v", err)
	}
	if !json.Valid(actionPlan) {
		t.Fatalf("actionPlan is not valid json: %s", string(actionPlan))
	}

	got, err := scanWorkItem(fakeWorkItemScanner{values: []any{
		input.ID,
		input.CaseID,
		input.QueueID,
		input.Type,
		input.Status,
		input.Priority,
		input.Reason,
		input.AssignedEmployeeID,
		input.PlanID,
		*input.DueAt,
		actionPlan,
		input.CreatedAt,
		input.UpdatedAt,
	}})
	if err != nil {
		t.Fatalf("scanWorkItem error = %v", err)
	}

	if got.ID != input.ID || got.CaseID != input.CaseID || got.AssignedEmployeeID != input.AssignedEmployeeID || got.PlanID != input.PlanID {
		t.Fatalf("scanWorkItem identity = %#v", got)
	}
	if got.DueAt == nil || !got.DueAt.Equal(*input.DueAt) {
		t.Fatalf("scanWorkItem dueAt = %#v", got.DueAt)
	}
	if got.ActionPlan == nil || got.ActionPlan.ID != input.ActionPlan.ID || len(got.ActionPlan.Actions) != 1 {
		t.Fatalf("scanWorkItem actionPlan = %#v", got.ActionPlan)
	}
	if got.ActionPlan.Actions[0].Params["attempt"] != float64(1) {
		t.Fatalf("scanWorkItem actionPlan params = %#v", got.ActionPlan.Actions[0].Params)
	}
}
