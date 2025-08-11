// api/router.go
package api

import (
	"log"

	"github.com/gin-gonic/gin"
)

func RunServer(addr string, storage *Storage) {
	// fail-fast, если есть критичные проблемы схемы
	if issues := storage.SchemaLint(); len(issues) > 0 {
		for _, it := range issues {
			log.Printf("[SCHEMA] %s.%s: %s (%s)\n", it.Entity, it.Field, it.Message, it.Code)
		}
		log.Fatal("schema has blocking issues; fix DSL and restart")
	}
	r := gin.Default()

	apiGroup := r.Group("/api")
	{

		//r.GET("/api/meta", MetaListHandler(storage))
		//r.GET("/api/meta/:module/:entity", MetaEntityHandler(storage))
		apiGroup.GET("/meta", MetaListHandler(storage))
		apiGroup.GET("/meta/:module/:entity", MetaEntityHandler(storage))
		apiGroup.GET("/meta/catalog/:name", MetaCatalogHandler(storage)) // если пользуешься catalog=

		apiGroup.POST("/:module/:entity/:id/_file/:field", UploadFileHandler(storage))
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

	_ = r.Run(addr)
}
