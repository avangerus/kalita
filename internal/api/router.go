package api

import (
	"github.com/gin-gonic/gin"
)

func RunServer(addr string, storage *Storage) {
	r := gin.Default()

	// Автоматически подключаем CRUD-роуты для всех сущностей
	r.GET("/api/:entity", ListHandler(storage))
	r.POST("/api/:entity", CreateHandler(storage))
	r.GET("/api/:entity/:id", GetHandler(storage))
	r.PUT("/api/:entity/:id", UpdateHandler(storage))
	r.DELETE("/api/:entity/:id", DeleteHandler(storage))

	// Можно добавить route /api/meta для схем, списков сущностей и пр.

	r.Run(addr)
}
