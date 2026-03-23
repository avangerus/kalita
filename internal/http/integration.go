package http

import (
	"net/http"

	"kalita/internal/integration"

	"github.com/gin-gonic/gin"
)

func registerIntegrationRoutes(group *gin.RouterGroup, svc integration.IncidentService) {
	if svc == nil {
		return
	}
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
