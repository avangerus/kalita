package http

import (
	"net/http"
	"time"

	"kalita/internal/runtime"
	"kalita/internal/validation"

	"github.com/gin-gonic/gin"
)

func CreateActionRequestHandler(storage *runtime.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		fqn, action, req, ok := parseActionRequest(c, storage)
		if !ok {
			return
		}

		request, errs := runtime.CreateWorkflowActionRequest(storage, fqn, c.Param("id"), action, req.RecordVersion)
		if len(errs) > 0 {
			c.JSON(statusForErrors(toValidationErrors(errs)), gin.H{"errors": toValidationErrors(errs)})
			return
		}

		c.JSON(http.StatusCreated, workflowActionRequestResponse(request))
	}
}

func GetActionRequestHandler(storage *runtime.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		request, ok := runtime.GetWorkflowActionRequest(storage, c.Param("request_id"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Action request not found"})
			return
		}
		c.JSON(http.StatusOK, workflowActionRequestResponse(request))
	}
}

func toValidationErrors(errs []runtime.ActionError) []validation.FieldError {
	verrs := make([]validation.FieldError, 0, len(errs))
	for _, err := range errs {
		verrs = append(verrs, validation.FieldError{Code: err.Code, Field: err.Field, Message: err.Message})
	}
	return verrs
}

func workflowActionRequestResponse(request *runtime.WorkflowActionRequest) gin.H {
	return gin.H{
		"id":             request.ID,
		"entity":         request.Entity,
		"target_id":      request.TargetID,
		"record_version": request.RecordVersion,
		"action":         request.Action,
		"status_field":   request.StatusField,
		"from":           request.From,
		"to":             request.To,
		"state":          request.State,
		"created_at":     request.CreatedAt.Format(time.RFC3339),
		"updated_at":     request.UpdatedAt.Format(time.RFC3339),
	}
}
