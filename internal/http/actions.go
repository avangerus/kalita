package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"kalita/internal/runtime"
	"kalita/internal/validation"

	"github.com/gin-gonic/gin"
)

type actionRequest struct {
	Action        string `json:"action,omitempty"`
	RecordVersion int64  `json:"record_version"`
}

func ActionHandler(storage *runtime.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		fqn, action, req, ok := parseActionRequest(c, storage)
		if !ok {
			return
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
