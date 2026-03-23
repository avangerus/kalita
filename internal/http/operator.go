package http

import (
	"net/http"
	"strings"

	"kalita/internal/controlplane"

	"github.com/gin-gonic/gin"
)

func registerOperatorRoutes(group *gin.RouterGroup, svc controlplane.Service) {
	if svc == nil {
		return
	}

	operator := group.Group("/operator")
	operator.GET("/cases", func(c *gin.Context) {
		payload, err := svc.ListCases(c.Request.Context())
		respondOperator(c, payload, err)
	})
	operator.GET("/summary", func(c *gin.Context) {
		payload, err := svc.GetSummary(c.Request.Context())
		respondOperator(c, payload, err)
	})
	operator.GET("/cases/:id", func(c *gin.Context) {
		payload, err := svc.GetCaseOverview(c.Request.Context(), c.Param("id"))
		respondOperator(c, payload, err)
	})
	operator.GET("/cases/:id/timeline", func(c *gin.Context) {
		payload, err := svc.GetCaseTimeline(c.Request.Context(), c.Param("id"))
		respondOperator(c, payload, err)
	})
	operator.POST("/cases/:id/acknowledge", func(c *gin.Context) {
		payload, err := svc.AcknowledgeCase(c.Request.Context(), c.Param("id"))
		respondOperator(c, payload, err)
	})
	operator.POST("/cases/:id/notes", func(c *gin.Context) {
		var req struct {
			Text string `json:"text"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			respondOperator(c, gin.H{}, err)
			return
		}
		payload, err := svc.AddCaseNote(c.Request.Context(), c.Param("id"), req.Text)
		respondOperator(c, payload, err)
	})
	operator.POST("/cases/:id/recoordinate", func(c *gin.Context) {
		payload, err := svc.RequestCaseRecoordination(c.Request.Context(), c.Param("id"))
		respondOperator(c, payload, err)
	})
	operator.POST("/cases/:id/external-input", func(c *gin.Context) {
		var req struct {
			Source string `json:"source"`
			Text   string `json:"text"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			respondOperator(c, gin.H{}, err)
			return
		}
		payload, err := svc.RecordExternalInput(c.Request.Context(), c.Param("id"), req.Source, req.Text)
		respondOperator(c, payload, err)
	})
	operator.GET("/work-items", func(c *gin.Context) {
		payload, err := svc.ListWorkItems(c.Request.Context())
		respondOperator(c, payload, err)
	})
	operator.GET("/work-items/:id", func(c *gin.Context) {
		payload, err := svc.GetWorkItemOverview(c.Request.Context(), c.Param("id"))
		respondOperator(c, payload, err)
	})
	operator.GET("/actors", func(c *gin.Context) {
		payload, err := svc.ListActors(c.Request.Context())
		respondOperator(c, payload, err)
	})
	operator.GET("/actors/:id", func(c *gin.Context) {
		payload, err := svc.GetActorOverview(c.Request.Context(), c.Param("id"))
		respondOperator(c, payload, err)
	})
	operator.GET("/approvals", func(c *gin.Context) {
		payload, err := svc.GetApprovalInbox(c.Request.Context())
		respondOperator(c, payload, err)
	})
	operator.POST("/approvals/:id/approve", func(c *gin.Context) {
		payload, err := svc.ApproveApprovalRequest(c.Request.Context(), c.Param("id"))
		respondOperator(c, payload, err)
	})
	operator.POST("/approvals/:id/reject", func(c *gin.Context) {
		payload, err := svc.RejectApprovalRequest(c.Request.Context(), c.Param("id"))
		respondOperator(c, payload, err)
	})
	operator.GET("/blocked-work", func(c *gin.Context) {
		payload, err := svc.GetBlockedOrDeferredWork(c.Request.Context())
		respondOperator(c, payload, err)
	})
}

func respondOperator[T any](c *gin.Context, payload T, err error) {
	if err != nil {
		c.JSON(statusForOperatorError(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, payload)
}

func statusForOperatorError(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if strings.HasSuffix(err.Error(), " not found") {
		return http.StatusNotFound
	}
	if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "must not") || strings.Contains(err.Error(), "JSON") {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
