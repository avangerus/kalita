package api

import (
	"net/http"
	"strings"

	"kalita/internal/dsl"
	"kalita/internal/reference"

	"github.com/gin-gonic/gin"
)

type reloadReq struct {
	DSLRoot   string `json:"dsl_root"`   // директория с *.dsl
	EnumsRoot string `json:"enums_root"` // директория со справочниками enum
}

func AdminReloadHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req reloadReq
		if err := c.ShouldBindJSON(&req); err != nil && err != http.ErrBodyNotAllowed {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}

		dslRoot := strings.TrimSpace(req.DSLRoot)
		if dslRoot == "" {
			dslRoot = "dsl"
		}
		enumsRoot := strings.TrimSpace(req.EnumsRoot)
		if enumsRoot == "" {
			enumsRoot = "reference/enums"
		}

		// 1) читаем новые схемы и справочники
		newSchemas, err := dsl.LoadAllEntities(dslRoot)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "DSL load error", "details": err.Error()})
			return
		}
		newEnums, err := reference.LoadEnumCatalog(enumsRoot)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Enum load error", "details": err.Error()})
			return
		}

		// 2) прогоняем линтер на временной копии storage (без конструирования литерала)
		tmp := *storage
		tmp.Schemas = newSchemas
		tmp.Enums = newEnums
		if issues := tmp.SchemaLint(); len(issues) > 0 {
			out := make([]gin.H, 0, len(issues))
			for _, it := range issues {
				out = append(out, gin.H{
					"entity":  it.Entity,
					"field":   it.Field,
					"message": it.Message,
					"code":    it.Code,
				})
			}
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "schema has blocking issues",
				"issues":  out,
				"hint":    "fix DSL and retry",
				"dslRoot": dslRoot, "enumsRoot": enumsRoot,
			})
			return
		}

		// 3) атомарная замена под write-lock
		storage.mu.Lock()
		storage.Schemas = newSchemas
		storage.Enums = newEnums
		// storage.rebuildNameIndexLocked() // если у тебя есть такой кэш
		storage.mu.Unlock()

		c.JSON(http.StatusOK, gin.H{
			"ok":         true,
			"dslRoot":    dslRoot,
			"enumsRoot":  enumsRoot,
			"entities":   len(newSchemas),
			"enumGroups": len(newEnums),
		})
	}
}
