// api/router.go
package api

import (
	"github.com/gin-gonic/gin"
)

func RunServer(addr string, storage *Storage) {
	r := gin.Default()

	apiGroup := r.Group("/api")
	{

		r.GET("/api/meta", MetaListHandler(storage))
		r.GET("/api/meta/:module/:entity", MetaEntityHandler(storage))
		// статические "служебные" маршруты — СНАЧАЛА
		apiGroup.GET("/:module/:entity/count", CountHandler(storage))  // новый алиас
		apiGroup.GET("/:module/:entity/_count", CountHandler(storage)) // твой текущий
		apiGroup.POST("/:module/:entity/_bulk", BulkCreateHandler(storage))
		apiGroup.PATCH("/:module/:entity/_bulk", BulkPatchHandler(storage))
		apiGroup.POST("/:module/:entity/:id/restore", RestoreHandler(storage))
		r.POST("/api/:module/:entity/_bulk_delete", BulkDeleteHandler(storage))
		r.POST("/api/:module/:entity/_bulk_restore", BulkRestoreHandler(storage))

		// обычные CRUD
		apiGroup.POST("/:module/:entity", CreateHandler(storage))
		apiGroup.GET("/:module/:entity", ListHandler(storage))
		apiGroup.GET("/:module/:entity/:id", GetOneHandler(storage))
		apiGroup.PUT("/:module/:entity/:id", UpdateHandler(storage))
		apiGroup.PATCH("/:module/:entity/:id", UpdatePartialHandler(storage))
		apiGroup.DELETE("/:module/:entity/:id", DeleteHandler(storage))

	}

	_ = r.Run(addr)
}
