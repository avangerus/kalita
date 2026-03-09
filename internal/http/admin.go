package http

import (
	"net/http"
	"strings"

	"kalita/internal/catalog"
	"kalita/internal/runtime"
	"kalita/internal/schema"

	"github.com/gin-gonic/gin"
)

type reloadReq struct {
	DSLRoot   string `json:"dsl_root"`   // директория с *.dsl
	EnumsRoot string `json:"enums_root"` // директория со справочниками enum
}

func AdminReloadHandler(storage *runtime.Storage) gin.HandlerFunc {
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
		newSchemas, err := schema.LoadAllEntities(dslRoot)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "DSL load error", "details": err.Error()})
			return
		}
		newEnums, err := catalog.LoadEnumCatalog(enumsRoot)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Enum load error", "details": err.Error()})
			return
		}

		// 2) прогоняем линтер на временной копии storage (без конструирования литерала)
		tmp := *storage
		tmp.Schemas = newSchemas
		tmp.Enums = newEnums
		if issues := schema.Lint(tmp.Schemas); len(issues) > 0 {
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
		storage.Mu.Lock()
		storage.Schemas = newSchemas
		storage.Enums = newEnums
		// storage.rebuildNameIndexLocked() // если у тебя есть такой кэш
		storage.Mu.Unlock()

		c.JSON(http.StatusOK, gin.H{
			"ok":         true,
			"dslRoot":    dslRoot,
			"enumsRoot":  enumsRoot,
			"entities":   len(newSchemas),
			"enumGroups": len(newEnums),
		})
	}
}
