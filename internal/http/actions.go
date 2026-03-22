package http

import (
	"fmt"
	"net/http"

	"kalita/internal/runtime"
	"kalita/internal/validation"

	"github.com/gin-gonic/gin"
)

type actionRequest struct {
	RecordVersion int64                  `json:"record_version"`
	Payload       map[string]interface{} `json:"payload"`
}

func ActionHandler(storage *runtime.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")
		id := c.Param("id")
		action := c.Param("action")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}

		var req actionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}

		result, errs := runtime.ExecuteWorkflowAction(storage, fqn, id, action, req.RecordVersion, req.RecordVersion > 0)
		if len(errs) > 0 {
			verrs := make([]validation.FieldError, 0, len(errs))
			for _, err := range errs {
				verrs = append(verrs, validation.FieldError{
					Code:    err.Code,
					Field:   err.Field,
					Message: err.Message,
				})
			}
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
