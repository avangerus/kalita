package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ===== META HANDLERS =====

type metaEntityListItem struct {
	Module string `json:"module"`
	Entity string `json:"entity"`
}

func MetaListHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		out := make([]metaEntityListItem, 0, len(storage.Schemas))
		for fqn := range storage.Schemas {
			mod, ent := splitFQN(fqn)
			out = append(out, metaEntityListItem{Module: mod, Entity: ent})
		}
		c.JSON(http.StatusOK, out)
	}
}

type metaField struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	ElemType string            `json:"elemType,omitempty"`
	Ref      string            `json:"ref,omitempty"`
	RefFQN   string            `json:"refFQN,omitempty"`
	Enum     []string          `json:"enum,omitempty"`
	Options  map[string]string `json:"options,omitempty"`
}

type metaEntity struct {
	Module      string         `json:"module"`
	Entity      string         `json:"entity"`
	Fields      []metaField    `json:"fields"`
	Constraints map[string]any `json:"constraints,omitempty"` // {"unique":[["code"],["base","quote","date"]]}
}

func MetaEntityHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		schema := storage.Schemas[fqn]

		fields := make([]metaField, 0, len(schema.Fields))
		for _, f := range schema.Fields {
			opts := map[string]string{}
			if f.Options != nil {
				for k, v := range f.Options {
					opts[k] = v
				}
			}

			ref := ""
			refFQN := ""

			// одиночный ref
			if strings.EqualFold(f.Type, "ref") && f.RefTarget != "" {
				ref = f.RefTarget
			}
			// массив ссылок
			if strings.EqualFold(f.Type, "array") && strings.EqualFold(f.ElemType, "ref") && f.RefTarget != "" {
				ref = f.RefTarget
			}

			if ref != "" {
				refMod := schema.Module
				refEnt := ref
				if strings.Contains(ref, ".") {
					parts := strings.SplitN(ref, ".", 2)
					refMod, refEnt = parts[0], parts[1]
				}
				if full, ok := storage.NormalizeEntityName(refMod, refEnt); ok {
					refFQN = full
				}
			}

			fields = append(fields, metaField{
				Name:     f.Name,
				Type:     strings.ToLower(f.Type),
				ElemType: f.ElemType,
				Ref:      ref,
				RefFQN:   refFQN, // ← новое поле
				Enum:     append([]string(nil), f.Enum...),
				Options:  opts,
			})
		}

		var constraints map[string]any
		if len(schema.Constraints.Unique) > 0 {
			uniq := make([][]string, 0, len(schema.Constraints.Unique))
			for _, set := range schema.Constraints.Unique {
				uniq = append(uniq, append([]string(nil), set...))
			}
			constraints = map[string]any{"unique": uniq}
		}

		m, e := splitFQN(fqn)
		c.JSON(http.StatusOK, metaEntity{
			Module:      m,
			Entity:      e,
			Fields:      fields,
			Constraints: constraints,
		})
	}
}

func MetaCatalogHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("name")
		dir, ok := storage.Enums[name]
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Catalog not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"name":  name,
			"items": dir.Items,
		})
	}
}

// splitFQN("module.entity") -> ("module","entity")
func splitFQN(fqn string) (string, string) {
	i := strings.IndexByte(fqn, '.')
	if i <= 0 || i >= len(fqn)-1 {
		return "", fqn
	}
	return fqn[:i], fqn[i+1:]
}
