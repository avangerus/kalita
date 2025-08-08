package api

import (
	"fmt"
	"kalita/internal/dsl"
	"net/http"

	"github.com/gin-gonic/gin"
)

// List all objects for entity
func ListHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		entity := c.Param("entity")
		storage.mu.RLock()
		defer storage.mu.RUnlock()
		data, ok := storage.Data[entity]
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		c.JSON(http.StatusOK, data)
	}
}

// Create new object for entity
func CreateHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		entity := c.Param("entity")
		schema, ok := storage.Schemas[entity]
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		var obj map[string]interface{}
		if err := c.ShouldBindJSON(&obj); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}

		// --- Валидация! ---
		validationErrors := validateBySchema(obj, schema.Fields)
		if len(validationErrors) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"errors": validationErrors})
			return
		}

		storage.mu.Lock()
		storage.Data[entity] = append(storage.Data[entity], obj)
		storage.mu.Unlock()
		c.JSON(http.StatusCreated, obj)
	}
}

// Валидация объекта по схеме полей
func validateBySchema(obj map[string]interface{}, fields []dsl.Field) (errs []string) {
	for _, field := range fields {
		val, exists := obj[field.Name]

		// Проверка required
		if field.Options["required"] == "true" && !exists {
			errs = append(errs, fmt.Sprintf("Field '%s' is required", field.Name))
			continue
		}

		// Проверка на enum
		if field.Type == "enum" && exists && len(field.Enum) > 0 {
			strVal, ok := val.(string)
			if !ok {
				errs = append(errs, fmt.Sprintf("Field '%s' must be string (enum)", field.Name))
				continue
			}
			valid := false
			for _, enumVal := range field.Enum {
				if strVal == enumVal {
					valid = true
					break
				}
			}
			if !valid {
				errs = append(errs, fmt.Sprintf("Field '%s' value '%s' is not allowed", field.Name, strVal))
			}
		}

		// Можно добавить еще типы (int, date и т.д.)
	}
	return
}

// Get one object (по индексу)
func GetHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		entity := c.Param("entity")
		id := c.Param("id")
		storage.mu.RLock()
		defer storage.mu.RUnlock()
		data, ok := storage.Data[entity]
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		// Очень простая логика: id — это индекс массива (для MVP)
		// В будущем: можно искать по id-полю!
		for idx, obj := range data {
			if id == toString(idx) {
				c.JSON(http.StatusOK, obj)
				return
			}
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "Object not found"})
	}
}

// Update object (по индексу)
func UpdateHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		entity := c.Param("entity")
		id := c.Param("id")
		storage.mu.Lock()
		defer storage.mu.Unlock()
		data, ok := storage.Data[entity]
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		for idx := range data {
			if id == toString(idx) {
				var obj map[string]interface{}
				if err := c.ShouldBindJSON(&obj); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
					return
				}
				data[idx] = obj
				storage.Data[entity] = data
				c.JSON(http.StatusOK, obj)
				return
			}
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "Object not found"})
	}
}

// Delete object (по индексу)
func DeleteHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		entity := c.Param("entity")
		id := c.Param("id")
		storage.mu.Lock()
		defer storage.mu.Unlock()
		data, ok := storage.Data[entity]
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		for idx := range data {
			if id == toString(idx) {
				storage.Data[entity] = append(data[:idx], data[idx+1:]...)
				c.JSON(http.StatusOK, gin.H{"status": "deleted"})
				return
			}
		}
		c.JSON(http.StatusNotFound, gin.H{"error": "Object not found"})
	}
}

// Простой хелпер для конвертации int в string
func toString(idx int) string {
	return fmt.Sprintf("%d", idx)
}
