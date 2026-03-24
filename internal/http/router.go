package http

import (
	"context"
	"log"
	"net/http"
	"time"

	"kalita/internal/caseruntime"
	"kalita/internal/command"
	"kalita/internal/controlplane"
	"kalita/internal/employee"
	"kalita/internal/integration"
	"kalita/internal/integrations/aisotkhody"
	"kalita/internal/runtime"
	"kalita/internal/schema"

	"github.com/gin-gonic/gin"
)

func RunServer(addr string, storage *runtime.Storage) {
	RunServerWithServices(addr, storage, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func RunServerWithCommandBus(addr string, storage *runtime.Storage, commandBus command.CommandBus) {
	RunServerWithServices(addr, storage, commandBus, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func RunServerWithServices(addr string, storage *runtime.Storage, commandBus command.CommandBus, caseService *caseruntime.Service, workService workItemIntakeService, coordinator coordinator, policyService policyService, constraintsService constraintsService, actionPlanService actionPlanService, proposalService proposalService, employeeDirectory employee.Directory, operatorService controlplane.Service, integrationService integration.IncidentService, aisIngestionService aisotkhody.IngestionService, employeeServices ...employeeService) {
	RunServerWithServicesAndHealth(addr, storage, commandBus, caseService, workService, coordinator, policyService, constraintsService, actionPlanService, proposalService, employeeDirectory, operatorService, integrationService, aisIngestionService, "memory", nil, employeeServices...)
}

func RunServerWithServicesAndHealth(addr string, storage *runtime.Storage, commandBus command.CommandBus, caseService *caseruntime.Service, workService workItemIntakeService, coordinator coordinator, policyService policyService, constraintsService constraintsService, actionPlanService actionPlanService, proposalService proposalService, employeeDirectory employee.Directory, operatorService controlplane.Service, integrationService integration.IncidentService, aisIngestionService aisotkhody.IngestionService, dbBackend string, dbHealthCheck func(context.Context) error, employeeServices ...employeeService) {
	r := newRouterWithServices(storage, commandBus, caseService, workService, coordinator, policyService, constraintsService, actionPlanService, proposalService, employeeDirectory, operatorService, integrationService, aisIngestionService, dbBackend, dbHealthCheck, employeeServices...)
	_ = r.Run(addr)
}

func newRouterWithServices(storage *runtime.Storage, commandBus command.CommandBus, caseService *caseruntime.Service, workService workItemIntakeService, coordinator coordinator, policyService policyService, constraintsService constraintsService, actionPlanService actionPlanService, proposalService proposalService, employeeDirectory employee.Directory, operatorService controlplane.Service, integrationService integration.IncidentService, aisIngestionService aisotkhody.IngestionService, dbBackend string, dbHealthCheck func(context.Context) error, employeeServices ...employeeService) *gin.Engine {
	// fail-fast, если есть критичные проблемы схемы
	if issues := schema.Lint(storage.Schemas); len(issues) > 0 {
		for _, it := range issues {
			log.Printf("[SCHEMA] %s.%s: %s (%s)\n", it.Entity, it.Field, it.Message, it.Code)
		}
		log.Fatal("schema has blocking issues; fix DSL and restart")
	}
	r := gin.Default()
	registerHealthRoute(r, dbBackend, dbHealthCheck)

	apiGroup := r.Group("/api")
	{
		registerOperatorRoutes(apiGroup, operatorService)
		registerIntegrationRoutes(apiGroup, integrationService, aisIngestionService)
		registerDemoRoutes(r)

		//r.GET("/api/meta", MetaListHandler(storage))
		//r.GET("/api/meta/:module/:entity", MetaEntityHandler(storage))
		apiGroup.GET("/meta", MetaListHandler(storage))
		apiGroup.GET("/meta/:module/:entity", MetaEntityHandler(storage))
		apiGroup.GET("/meta/catalog/:name", MetaCatalogHandler(storage)) // если пользуешься catalog=

		apiGroup.POST("/:module/:entity/:id/_file/:field", UploadFileHandler(storage))
		apiGroup.POST("/:module/:entity/:id/_actions/:action", ActionHandlerWithServices(storage, commandBus, caseService, workService, coordinator, policyService, constraintsService, actionPlanService, proposalService, employeeDirectory, employeeServices...))
		apiGroup.POST("/:module/:entity/:id/_actions/:action/requests", CreateActionRequestHandler(storage))
		apiGroup.GET("/_action_requests/:request_id", GetActionRequestHandler(storage))
		r.GET("/api/core/attachment/:id/download", DownloadAttachmentHandler(storage))

		r.POST("/api/admin/reload", AdminReloadHandler(storage))
		// статические "служебные" маршруты — СНАЧАЛА
		apiGroup.GET("/:module/:entity/count", CountHandler(storage))  // новый алиас
		apiGroup.GET("/:module/:entity/_count", CountHandler(storage)) // твой текущий
		apiGroup.POST("/:module/:entity/_bulk", BulkCreateHandler(storage))
		apiGroup.PATCH("/:module/:entity/_bulk", BulkPatchHandler(storage))
		apiGroup.POST("/:module/:entity/:id/restore", RestoreHandler(storage))
		r.POST("/api/:module/:entity/_bulk_delete", BulkDeleteHandler(storage))
		r.POST("/api/:module/:entity/_bulk_restore", BulkRestoreHandler(storage))
		apiGroup.POST("/:module/:entity/_batch_get", BatchGetHandler(storage))

		//r.GET("/api/meta/catalogs", MetaCatalogsHandler(storage))
		//r.GET("/api/meta/catalog/:name", MetaCatalogHandler(storage))

		// обычные CRUD
		apiGroup.POST("/:module/:entity", CreateHandler(storage))
		apiGroup.GET("/:module/:entity", ListHandler(storage))
		apiGroup.GET("/:module/:entity/:id", GetOneHandler(storage))
		apiGroup.PUT("/:module/:entity/:id", UpdateHandler(storage))
		apiGroup.PATCH("/:module/:entity/:id", PatchHandler(storage))

		apiGroup.DELETE("/:module/:entity/:id", DeleteHandler(storage))

	}
	return r
}

func registerHealthRoute(r *gin.Engine, dbBackend string, dbHealthCheck func(context.Context) error) {
	backend := dbBackend
	if backend == "" {
		backend = "memory"
	}
	r.GET("/health", func(c *gin.Context) {
		if dbHealthCheck != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
			defer cancel()
			if err := dbHealthCheck(ctx); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"status": "degraded",
					"db":     backend,
					"error":  err.Error(),
				})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"db":     backend,
		})
	})
}
