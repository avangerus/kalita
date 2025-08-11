package api

import (
	"fmt"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// POST /api/:module/:entity/:id/_file/:field
func UploadFileHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")
		id := c.Param("id")
		field := c.Param("field")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		if storage.Blob == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "blob store not configured"})
			return
		}

		schema := storage.Schemas[fqn]
		// проверим, что поле подходит
		ftype := ""
		isArray := false
		refTarget := ""
		for _, f := range schema.Fields {
			if f.Name != field {
				continue
			}
			if strings.EqualFold(f.Type, "ref") && f.RefTarget == "core.Attachment" {
				ftype = "ref"
				refTarget = f.RefTarget
				break
			}
			if strings.EqualFold(f.Type, "array") && strings.EqualFold(f.ElemType, "ref") && f.RefTarget == "core.Attachment" {
				ftype = "array_ref"
				isArray = true
				refTarget = f.RefTarget
				break
			}
		}
		if ftype == "" || refTarget == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Field is not ref[core.Attachment] or array[ref[core.Attachment]]"})
			return
		}

		// multipart
		file, hdr, err := c.Request.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "multipart file not found (field name 'file')"})
			return
		}
		defer file.Close()

		// сохраним в blob
		key, size, sum, err := storage.Blob.Put("", file) // key генерится автоматически
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "store error", "details": err.Error()})
			return
		}

		// создадим Attachment
		attSchema, ok := storage.Schemas["core.Attachment"]
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "core.Attachment schema not found"})
			return
		}
		att := map[string]any{
			"owner_entity": fqn,
			"owner_id":     id,
			"file_name":    safeName(hdr),
			"mime":         hdr.Header.Get("Content-Type"),
			"size":         float64(size),
			"storage":      "local",
			"storage_key":  key,
			"created_at":   time.Now().UTC().Format(time.RFC3339),
			"hash":         sum,
		}

		// валидация Attachment и запись
		if errs := ValidateAgainstSchema(storage, attSchema, att, "", "core.Attachment"); len(errs) > 0 {
			c.JSON(statusForErrors(errs), gin.H{"errors": errs})
			return
		}
		now := time.Now().UTC()
		storage.mu.Lock()
		if storage.Data["core.Attachment"] == nil {
			storage.Data["core.Attachment"] = map[string]*Record{}
		}
		attID := storage.newID()
		storage.Data["core.Attachment"][attID] = &Record{
			ID:        attID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			Data:      att,
		}
		// подставим в поле сущности
		rec := storage.Data[fqn][id]
		if rec == nil || rec.Deleted {
			storage.mu.Unlock()
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}
		if isArray {
			switch cur := rec.Data[field].(type) {
			case nil:
				rec.Data[field] = []string{attID}
			case []any:
				rec.Data[field] = append(cur, attID)
			case []string:
				rec.Data[field] = append(cur, attID)
			default:
				rec.Data[field] = []string{attID}
			}
		} else {
			rec.Data[field] = attID
		}
		rec.Version++
		rec.UpdatedAt = now
		storage.mu.Unlock()

		c.JSON(http.StatusOK, gin.H{
			"attachment_id": attID,
			"storage_key":   key,
		})
	}
}

func safeName(h *multipart.FileHeader) string {
	name := h.Filename
	name = filepath.Base(name)
	name = strings.TrimSpace(name)
	if name == "" {
		return "file"
	}
	return name
}

// GET /api/core/attachment/:id/download
func DownloadAttachmentHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")

		storage.mu.RLock()
		rec := storage.Data["core.Attachment"][id]
		storage.mu.RUnlock()

		if rec == nil || rec.Deleted {
			c.JSON(http.StatusNotFound, gin.H{"error": "Attachment not found"})
			return
		}

		key := toString(rec.Data["storage_key"])
		name := toString(rec.Data["file_name"])
		mime := toString(rec.Data["mime"])

		if storage.Blob == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "blob store not configured"})
			return
		}
		p, _ := storage.Blob.Path(key)

		// Явно проставляем заголовки, чтобы использовать сохранённый MIME
		if name == "" {
			name = "file"
		}
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
		if mime != "" {
			c.Header("Content-Type", mime)
		} else {
			c.Header("Content-Type", "application/octet-stream")
		}

		c.File(p)
	}
}
