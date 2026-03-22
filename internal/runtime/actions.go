package runtime

import (
	"fmt"
	"strings"
	"time"

	"kalita/internal/schema"
)

type ActionResult struct {
	Entity       string                 `json:"entity"`
	ID           string                 `json:"id"`
	Action       string                 `json:"action"`
	StatusField  string                 `json:"status_field"`
	From         string                 `json:"from"`
	To           string                 `json:"to"`
	Version      int64                  `json:"version"`
	Record       map[string]interface{} `json:"record"`
	Committed    bool                   `json:"committed"`
	UpdatedAtUTC time.Time              `json:"-"`
}

func ExecuteWorkflowAction(storage *Storage, entityFQN, id, actionName string, expectedVersion int64) (*ActionResult, []ActionError) {
	entitySchema := storage.Schemas[entityFQN]
	if entitySchema == nil {
		return nil, []ActionError{{Code: "not_found", Field: "entity", Message: "Entity not found"}}
	}

	workflow := entitySchema.Workflow
	if workflow == nil {
		return nil, []ActionError{{Code: "not_found", Field: "action", Message: "Workflow is not declared for entity"}}
	}

	action, ok := workflow.Actions[actionName]
	if !ok {
		return nil, []ActionError{{Code: "not_found", Field: "action", Message: "Action not declared for entity"}}
	}

	storage.Mu.RLock()
	rec := storage.Data[entityFQN][id]
	storage.Mu.RUnlock()
	if rec == nil || rec.Deleted {
		return nil, []ActionError{{Code: "not_found", Field: "id", Message: "Record not found"}}
	}
	if rec.Version != expectedVersion {
		return nil, []ActionError{{
			Code:    "version_conflict",
			Field:   "record_version",
			Message: fmt.Sprintf("expected %d, got %d", expectedVersion, rec.Version),
		}}
	}

	currentStatus := fmt.Sprintf("%v", rec.Data[workflow.StatusField])
	if !contains(action.From, currentStatus) {
		return nil, []ActionError{{
			Code:    "enum_invalid",
			Field:   workflow.StatusField,
			Message: fmt.Sprintf("Action %q is not allowed from status %q", actionName, currentStatus),
		}}
	}

	proposal := flattenRecord(rec)
	proposal[workflow.StatusField] = action.To
	proposal["version"] = rec.Version
	proposal["updated_at"] = rec.UpdatedAt.Format(time.RFC3339)

	return &ActionResult{
		Entity:       entityFQN,
		ID:           rec.ID,
		Action:       actionName,
		StatusField:  workflow.StatusField,
		From:         currentStatus,
		To:           action.To,
		Version:      rec.Version,
		Record:       proposal,
		Committed:    false,
		UpdatedAtUTC: rec.UpdatedAt,
	}, nil
}

type ActionError struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

func flattenRecord(rec *Record) map[string]interface{} {
	out := map[string]interface{}{
		"id":         rec.ID,
		"version":    rec.Version,
		"created_at": rec.CreatedAt.Format(time.RFC3339),
		"updated_at": rec.UpdatedAt.Format(time.RFC3339),
	}
	for k, v := range rec.Data {
		if _, clash := out[k]; clash {
			out["data."+k] = v
			continue
		}
		out[k] = v
	}
	return out
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if strings.EqualFold(item, want) || item == want {
			return true
		}
	}
	return false
}

func WorkflowActions(entity *schema.Entity) map[string]schema.WorkflowAction {
	if entity == nil || entity.Workflow == nil {
		return nil
	}
	out := make(map[string]schema.WorkflowAction, len(entity.Workflow.Actions))
	for k, v := range entity.Workflow.Actions {
		out[k] = schema.WorkflowAction{
			From: append([]string(nil), v.From...),
			To:   v.To,
		}
	}
	return out
}
