package http

import (
	"net/http"

	"kalita/internal/controlplane"

	"github.com/gin-gonic/gin"
)

func OperatorCaseDetailHandler(service *controlplane.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "operator control plane unavailable"})
			return
		}
		detail, ok, err := service.GetCaseDetail(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "case not found"})
			return
		}
		c.JSON(http.StatusOK, detail)
	}
}

func OperatorCaseTimelineHandler(service *controlplane.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "operator control plane unavailable"})
			return
		}
		entries, ok, err := service.GetCaseTimeline(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "case not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"entries": entries})
	}
}

func OperatorSummaryHandler(service *controlplane.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			c.JSON(http.StatusNotImplemented, gin.H{"error": "operator control plane unavailable"})
			return
		}
		summary, err := service.GetSummary(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, summary)
	}
}
