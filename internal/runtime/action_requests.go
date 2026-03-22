package runtime

import (
	"fmt"
	"time"
)

type WorkflowActionRequest struct {
	ID            string    `json:"id"`
	Entity        string    `json:"entity"`
	TargetID      string    `json:"target_id"`
	RecordVersion int64     `json:"record_version"`
	Action        string    `json:"action"`
	StatusField   string    `json:"status_field"`
	From          string    `json:"from"`
	To            string    `json:"to"`
	State         string    `json:"state"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func CreateWorkflowActionRequest(storage *Storage, entityFQN, id, actionName string, expectedVersion int64) (*WorkflowActionRequest, []ActionError) {
	result, errs := ExecuteWorkflowAction(storage, entityFQN, id, actionName, expectedVersion)
	if len(errs) > 0 {
		return nil, errs
	}

	now := time.Now().UTC()
	key := workflowActionRequestKey(result.Entity, result.ID, result.Action, result.Version)

	storage.Mu.Lock()
	defer storage.Mu.Unlock()
	if storage.ActionRequests == nil {
		storage.ActionRequests = make(map[string]*WorkflowActionRequest)
	}
	if storage.ActionRequestKeys == nil {
		storage.ActionRequestKeys = make(map[string]string)
	}
	if existingID, ok := storage.ActionRequestKeys[key]; ok {
		if existing := storage.ActionRequests[existingID]; existing != nil {
			copy := *existing
			return &copy, nil
		}
	}
	request := &WorkflowActionRequest{
		ID:            storage.NewID(),
		Entity:        result.Entity,
		TargetID:      result.ID,
		RecordVersion: result.Version,
		Action:        result.Action,
		StatusField:   result.StatusField,
		From:          result.From,
		To:            result.To,
		State:         "pending",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	storage.ActionRequests[request.ID] = request
	storage.ActionRequestKeys[key] = request.ID

	return request, nil
}

func workflowActionRequestKey(entityFQN, id, actionName string, recordVersion int64) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d", entityFQN, id, actionName, recordVersion)
}

func GetWorkflowActionRequest(storage *Storage, id string) (*WorkflowActionRequest, bool) {
	storage.Mu.RLock()
	defer storage.Mu.RUnlock()
	request := storage.ActionRequests[id]
	if request == nil {
		return nil, false
	}
	copy := *request
	return &copy, true
}
