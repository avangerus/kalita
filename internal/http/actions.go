package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"kalita/internal/caseruntime"
	"kalita/internal/command"
	"kalita/internal/eventcore"
	"kalita/internal/runtime"
	"kalita/internal/validation"

	"github.com/gin-gonic/gin"
)

type actionRequest struct {
	Action        string `json:"action,omitempty"`
	RecordVersion int64  `json:"record_version"`
}

func ActionHandler(storage *runtime.Storage) gin.HandlerFunc {
	return ActionHandlerWithServices(storage, nil, nil)
}

func ActionHandlerWithCommandBus(storage *runtime.Storage, commandBus command.CommandBus) gin.HandlerFunc {
	return ActionHandlerWithServices(storage, commandBus, nil)
}

type commandCaseResolver interface {
	ResolveCommand(ctx context.Context, cmd eventcore.Command) (caseruntime.ResolutionResult, error)
}

func ActionHandlerWithServices(storage *runtime.Storage, commandBus command.CommandBus, caseService commandCaseResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		fqn, action, req, ok := parseActionRequest(c, storage)
		if !ok {
			return
		}

		if commandBus != nil {
			cmd := buildWorkflowActionCommand(c, fqn, action, req)
			admitted, err := commandBus.Submit(c.Request.Context(), cmd)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"errors": []validation.FieldError{{
					Code:    validation.ErrTypeMismatch,
					Field:   "action",
					Message: err.Error(),
				}}})
				return
			}
			if caseService != nil {
				if _, err := caseService.ResolveCommand(c.Request.Context(), admitted); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"errors": []validation.FieldError{{
						Code:    validation.ErrTypeMismatch,
						Field:   "action",
						Message: err.Error(),
					}}})
					return
				}
			}
		}

		result, errs := runtime.ExecuteWorkflowAction(storage, fqn, c.Param("id"), action, req.RecordVersion)
		if len(errs) > 0 {
			verrs := toValidationErrors(errs)
			c.JSON(statusForErrors(verrs), gin.H{"errors": verrs})
			return
		}

		c.Header("ETag", fmt.Sprintf(`"%d"`, result.Version))
		c.JSON(http.StatusOK, gin.H{
			"id":           result.ID,
			"entity":       result.Entity,
			"action":       result.Action,
			"status_field": result.StatusField,
			"from":         result.From,
			"to":           result.To,
			"version":      result.Version,
			"committed":    result.Committed,
			"record":       result.Record,
		})
	}
}

func parseActionRequest(c *gin.Context, storage *runtime.Storage) (string, string, actionRequest, bool) {
	mod := c.Param("module")
	ent := c.Param("entity")
	action := c.Param("action")

	fqn, ok := storage.NormalizeEntityName(mod, ent)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
		return "", "", actionRequest{}, false
	}

	var req actionRequest
	dec := json.NewDecoder(bytes.NewReader(mustReadBody(c)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return "", "", actionRequest{}, false
	}
	if req.Action != "" && req.Action != action {
		c.JSON(http.StatusBadRequest, gin.H{"errors": []validation.FieldError{{
			Code:    validation.ErrTypeMismatch,
			Field:   "action",
			Message: fmt.Sprintf("body action %q does not match path action %q", req.Action, action),
		}}})
		return "", "", actionRequest{}, false
	}
	if req.RecordVersion <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"errors": []validation.FieldError{{
			Code:    validation.ErrRequired,
			Field:   "record_version",
			Message: "record_version is required for workflow actions",
		}}})
		return "", "", actionRequest{}, false
	}

	return fqn, action, req, true
}

func mustReadBody(c *gin.Context) []byte {
	if c.Request.Body == nil {
		return []byte("{}")
	}
	body, err := c.GetRawData()
	if err != nil || len(body) == 0 {
		return []byte("{}")
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return body
}

func buildWorkflowActionCommand(c *gin.Context, fqn, action string, req actionRequest) eventcore.Command {
	requestID := c.GetHeader("X-Request-Id")
	if requestID == "" {
		requestID = c.GetHeader("X-Request-ID")
	}

	return eventcore.Command{
		Type:           "workflow.action",
		CaseID:         c.Param("id"),
		TargetRef:      fmt.Sprintf("%s/%s", fqn, c.Param("id")),
		IdempotencyKey: fmt.Sprintf("%s:%s:%d", fqn, action, req.RecordVersion),
		Actor: eventcore.ActorContext{
			ActorType: eventcore.ActorHuman,
			RequestID: requestID,
		},
		Payload: map[string]any{
			"entity":         fqn,
			"record_id":      c.Param("id"),
			"action":         action,
			"record_version": req.RecordVersion,
		},
	}
}
