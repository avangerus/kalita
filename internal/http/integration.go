package http

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"kalita/internal/integration"
	"kalita/internal/integrations/aisotkhody"

	"github.com/gin-gonic/gin"
)

func registerIntegrationRoutes(group *gin.RouterGroup, svc integration.IncidentService, aisSvc aisotkhody.IngestionService) {
	if svc != nil {
		group.POST("/integration/incidents", func(c *gin.Context) {
			var incident integration.ExternalIncident
			if err := c.ShouldBindJSON(&incident); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
				return
			}
			result, err := svc.IngestIncident(c.Request.Context(), incident)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			if result.Duplicate {
				c.JSON(http.StatusOK, gin.H{"duplicate": true, "case_id": result.Case.ID})
				return
			}
			approvalID := ""
			if result.ApprovalRequest != nil {
				approvalID = result.ApprovalRequest.ID
			}
			executionID := ""
			if result.ExecutionSession != nil {
				executionID = result.ExecutionSession.ID
			}
			c.JSON(http.StatusAccepted, gin.H{"duplicate": false, "case_id": result.Case.ID, "work_item_id": result.WorkItem.ID, "coordination_decision_id": result.Coordination.ID, "policy_decision_id": result.PolicyDecision.ID, "approval_request_id": approvalID, "execution_session_id": executionID})
		})
	}
	if aisSvc != nil {
		group.POST("/integrations/ais/ingest", func(c *gin.Context) {
			date, err := parseAISIngestDate(c)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			result, err := aisSvc.IngestDate(c.Request.Context(), date)
			if err != nil {
				c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
				return
			}
			status := http.StatusOK
			if len(result.Errors) > 0 {
				status = http.StatusMultiStatus
			}
			c.JSON(status, result)
			return
		})
	}
}

func parseAISIngestDate(c *gin.Context) (time.Time, error) {
	var payload struct {
		Date string `json:"date"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&payload); err != nil {
			return time.Time{}, fmt.Errorf("invalid JSON")
		}
	}
	dateValue := strings.TrimSpace(payload.Date)
	if dateValue == "" {
		return time.Now().UTC(), nil
	}
	parsed, err := time.Parse("2006-01-02", dateValue)
	if err != nil {
		return time.Time{}, fmt.Errorf("date must be in YYYY-MM-DD format")
	}
	return parsed.UTC(), nil
}
