package api

import (
	"fmt"
	"kalita/internal/dsl"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// POST /api/:entity
// POST /api/:module/:entity
// POST /api/:entity  и /api/:module/:entity
func CreateHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawModule := c.Param("module")
		rawEntity := c.Param("entity")

		entity, ok := storage.NormalizeEntityName(rawModule, rawEntity)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{
				"errors": []FieldError{ferr(ErrTypeMismatch, "entity", "Entity not found")},
			})
			return
		}
		schema := storage.Schemas[entity]

		var obj map[string]interface{}
		if err := c.ShouldBindJSON(&obj); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}

		// 1) defaults
		applyDefaults(schema, obj)

		// 2) защита системных/readonly
		if ers := checkReadonlyAndSystem(schema, obj, true); len(ers) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"errors": ers})
			return
		}

		// 3) валидация (без write-lock)
		if errs := ValidateAgainstSchema(storage, schema, obj, "", entity); len(errs) > 0 {
			c.JSON(statusForErrors(errs), gin.H{"errors": errs})
			return
		}

		// 4) запись (под write-lock)
		storage.mu.Lock()
		defer storage.mu.Unlock()

		if storage.Data[entity] == nil {
			storage.Data[entity] = make(map[string]*Record)
		}

		id := storage.newID()
		now := time.Now().UTC()
		rec := &Record{
			ID:        id,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
			Data:      obj,
		}
		storage.Data[entity][id] = rec
		c.JSON(http.StatusCreated, flatten(rec))
	}
}

// GET /api/:module/:entity
func ListHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		schema := storage.Schemas[fqn]

		// читаем все «живые» записи
		storage.mu.RLock()
		recMap := storage.Data[fqn]
		all := make([]*Record, 0, len(recMap))
		for _, r := range recMap {
			if !r.Deleted {
				all = append(all, r)
			}
		}
		storage.mu.RUnlock()

		// единый query: удаляем служебное nulls, чтобы не попало в фильтры
		q := c.Request.URL.Query()
		q.Del("nulls")

		// 1) фильтры
		filtered := filterWithOps(all, schema, q)

		// 2) сортировка/пагинация
		lp := parseListParams(q) // понимает _limit/_offset/_sort и nulls
		if len(lp.Sort) > 0 {
			sortRecordsMultiNulls(filtered, lp.Sort, lp.Nulls)
		}

		start := lp.Offset
		if start < 0 {
			start = 0
		}
		end := start + lp.Limit
		if start > len(filtered) {
			start = len(filtered)
		}
		if end > len(filtered) {
			end = len(filtered)
		}
		page := filtered[start:end]

		out := make([]map[string]any, 0, len(page))
		for _, rec := range page {
			out = append(out, flatten(rec))
		}
		c.Header("X-Total-Count", strconv.Itoa(len(filtered)))
		c.JSON(http.StatusOK, out)
	}
}

// GET /api/:module/:entity/:id
func GetOneHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")
		id := c.Param("id")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}

		storage.mu.RLock()
		rec := storage.Data[fqn][id]
		storage.mu.RUnlock()
		if rec == nil || rec.Deleted {
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}

		// ⬇️ ВСТАВЬ ЭТО: If-None-Match поддержка
		inm := strings.TrimSpace(c.GetHeader("If-None-Match"))
		if inm != "" {
			// допускаем варианты: 3, "3", W/"3"
			if strings.HasPrefix(inm, "W/") {
				inm = strings.TrimPrefix(inm, "W/")
			}
			inm = strings.Trim(inm, `"'`)
			if v, err := strconv.ParseInt(inm, 10, 64); err == nil && v == rec.Version {
				// версию не меняем, отдадим только заголовок
				c.Header("ETag", fmt.Sprintf(`"%d"`, rec.Version))
				c.Status(http.StatusNotModified) // 304
				return
			}
		}

		c.Header("ETag", fmt.Sprintf(`"%d"`, rec.Version))
		c.JSON(http.StatusOK, flatten(rec)) // ← как было
	}
}

// PUT /api/:module/:entity/:id
func UpdateHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")
		id := c.Param("id")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		schema := storage.Schemas[fqn]

		var obj map[string]any
		if err := c.ShouldBindJSON(&obj); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}

		// 1) ожидаемая версия (If-Match или body.version)
		expVer, okExp := readExpectedVersion(c, obj)

		// 2) защита системных/readonly (на update — ругаемся)
		if ers := checkReadonlyAndSystem(schema, obj, false); len(ers) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"errors": ers})
			return
		}

		// 3) читаем текущую запись и версию (RLock)
		storage.mu.RLock()
		rec := storage.Data[fqn][id]
		storage.mu.RUnlock()
		if rec == nil || rec.Deleted {
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}

		// 4) оптимистическая блокировка, если версия ожидалась
		if okExp && expVer != rec.Version {
			c.JSON(http.StatusConflict, gin.H{"errors": []FieldError{
				ferr(ErrVersionConflict, "version", fmt.Sprintf("expected %d, got %d", expVer, rec.Version)),
			}})
			return
		}

		// 5) валидация (без write-lock); исключаем текущую запись из unique-поиска
		if errs := ValidateAgainstSchema(storage, schema, obj, id, fqn); len(errs) > 0 {
			c.JSON(statusForErrors(errs), gin.H{"errors": errs})
			return
		}

		// 6) запись (под write-lock) + финальная проверка версии от гонок
		storage.mu.Lock()
		defer storage.mu.Unlock()

		cur := storage.Data[fqn][id]
		if cur == nil || cur.Deleted {
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}
		if okExp && expVer != cur.Version {
			c.JSON(http.StatusConflict, gin.H{"errors": []FieldError{
				ferr(ErrVersionConflict, "version", fmt.Sprintf("expected %d, got %d", expVer, cur.Version)),
			}})
			return
		}

		cur.Data = obj
		cur.Version++
		cur.UpdatedAt = time.Now().UTC()

		c.Header("ETag", fmt.Sprintf(`"%d"`, cur.Version))
		c.JSON(http.StatusOK, flatten(cur))
	}
}

// PATCH /api/:module/:entity/:id
func PatchHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")
		id := c.Param("id")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		schema := storage.Schemas[fqn]

		var patch map[string]any
		if err := c.ShouldBindJSON(&patch); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}
		if len(patch) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Empty patch"})
			return
		}

		// ожидаемая версия (If-Match или body.version)
		expVer, okExp := readExpectedVersion(c, patch)

		// защита системных/readonly (на update ругаемся)
		if ers := checkReadonlyAndSystem(schema, patch, false); len(ers) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"errors": ers})
			return
		}

		// читаем текущую запись
		storage.mu.RLock()
		rec := storage.Data[fqn][id]
		storage.mu.RUnlock()
		if rec == nil || rec.Deleted {
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}

		// первая проверка версии (optimistic locking)
		if okExp && expVer != rec.Version {
			c.JSON(http.StatusConflict, gin.H{"errors": []FieldError{
				ferr(ErrVersionConflict, "version", fmt.Sprintf("expected %d, got %d", expVer, rec.Version)),
			}})
			return
		}

		// shallow merge: current ⊕ patch
		merged := make(map[string]any, len(rec.Data)+len(patch))
		for k, v := range rec.Data {
			merged[k] = v
		}
		for k, v := range patch {
			if v == nil {
				delete(merged, k) // null в PATCH — удалить поле; убери блок, если не нужна такая семантика
				continue
			}
			merged[k] = v
		}

		// валидация целиком merged; исключаем текущую запись из unique-проверок
		if errs := ValidateAgainstSchema(storage, schema, merged, id, fqn); len(errs) > 0 {
			c.JSON(statusForErrors(errs), gin.H{"errors": errs})
			return
		}

		// запись под write-lock + финальная сверка версии
		storage.mu.Lock()
		defer storage.mu.Unlock()

		cur := storage.Data[fqn][id]
		if cur == nil || cur.Deleted {
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}
		if okExp && expVer != cur.Version {
			c.JSON(http.StatusConflict, gin.H{"errors": []FieldError{
				ferr(ErrVersionConflict, "version", fmt.Sprintf("expected %d, got %d", expVer, cur.Version)),
			}})
			return
		}

		cur.Data = merged
		cur.Version++
		cur.UpdatedAt = time.Now().UTC()

		c.Header("ETag", fmt.Sprintf(`"%d"`, cur.Version))
		c.JSON(http.StatusOK, flatten(cur))
	}
}

// DELETE /api/:entity/:id  (soft delete)
// DELETE /api/:module/:entity/:id
func DeleteHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")
		id := c.Param("id")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}

		// --- ON_DELETE POLICIES ---
		// Сканируем все схемы и записи, чтобы понять, где на нас ссылаются.
		type pendingNull struct {
			ent   string // FQN дочерней сущности
			id    string // id записи-ребёнка
			field string // имя поля-ссылки
			isArr bool   // массив ссылок
		}
		var toNull []pendingNull

		storage.mu.RLock()
		for childFQN, childSchema := range storage.Schemas {
			// соберём поля, которые ссылаются на текущую сущность
			type rf struct {
				name   string
				kind   string // "ref" | "array_ref"
				policy string // restrict | set_null | cascade
			}
			var refFields []rf

			for _, cf := range childSchema.Fields {
				// одиночный ref
				if strings.EqualFold(cf.Type, "ref") && cf.RefTarget != "" {
					tgt := cf.RefTarget
					if !strings.Contains(tgt, ".") {
						tgt = childSchema.Module + "." + tgt
					}
					if tgt == fqn {
						pol := strings.ToLower(cf.Options["on_delete"])
						if pol == "" {
							pol = "restrict"
						}
						refFields = append(refFields, rf{cf.Name, "ref", pol})
					}
				}
				// массив ссылок: array[ref[...]]
				if strings.EqualFold(cf.Type, "array") && strings.EqualFold(cf.ElemType, "ref") && cf.RefTarget != "" {
					tgt := cf.RefTarget
					if !strings.Contains(tgt, ".") {
						tgt = childSchema.Module + "." + tgt
					}
					if tgt == fqn {
						pol := strings.ToLower(cf.Options["on_delete"])
						if pol == "" {
							pol = "restrict"
						}
						refFields = append(refFields, rf{cf.Name, "array_ref", pol})
					}
				}
			}

			if len(refFields) == 0 {
				continue
			}

			// пробежимся по живым записям дочерней сущности
			for childID, rec := range storage.Data[childFQN] {
				if rec == nil || rec.Deleted {
					continue
				}
				for _, rf := range refFields {
					v, ok := rec.Data[rf.name]
					if !ok {
						continue
					}

					switch rf.kind {
					case "ref":
						if s, _ := v.(string); s == id {
							if rf.policy == "restrict" {
								storage.mu.RUnlock()
								c.JSON(http.StatusConflict, gin.H{
									"errors": []FieldError{{
										Code:    "fk_in_use",
										Field:   rf.name,
										Message: fmt.Sprintf("record is referenced by %s.%s", childFQN, rf.name),
									}},
								})
								return
							}
							if rf.policy == "set_null" {
								toNull = append(toNull, pendingNull{ent: childFQN, id: childID, field: rf.name, isArr: false})
							}
						}

					case "array_ref":
						switch arr := v.(type) {
						case []any:
							found := false
							for _, it := range arr {
								if s, _ := it.(string); s == id {
									found = true
									break
								}
							}
							if found {
								if rf.policy == "restrict" {
									storage.mu.RUnlock()
									c.JSON(http.StatusConflict, gin.H{
										"errors": []FieldError{{
											Code:    "fk_in_use",
											Field:   rf.name,
											Message: fmt.Sprintf("record is referenced by %s.%s", childFQN, rf.name),
										}},
									})
									return
								}
								if rf.policy == "set_null" {
									toNull = append(toNull, pendingNull{ent: childFQN, id: childID, field: rf.name, isArr: true})
								}
							}

						case []string:
							found := false
							for _, s := range arr {
								if s == id {
									found = true
									break
								}
							}
							if found {
								if rf.policy == "restrict" {
									storage.mu.RUnlock()
									c.JSON(http.StatusConflict, gin.H{
										"errors": []FieldError{{
											Code:    "fk_in_use",
											Field:   rf.name,
											Message: fmt.Sprintf("record is referenced by %s.%s", childFQN, rf.name),
										}},
									})
									return
								}
								if rf.policy == "set_null" {
									toNull = append(toNull, pendingNull{ent: childFQN, id: childID, field: rf.name, isArr: true})
								}
							}
						}
					}
				}
			}
		}
		storage.mu.RUnlock()

		// если restrict не сработал — применяем set_null/удаление id из массивов под write-lock
		if len(toNull) > 0 {
			now := time.Now().UTC()
			storage.mu.Lock()
			for _, p := range toNull {
				rec := storage.Data[p.ent][p.id]
				if rec == nil || rec.Deleted {
					continue
				}
				if p.isArr {
					// массив ссылок: удалим удаляемый id
					switch arr := rec.Data[p.field].(type) {
					case []any:
						out := make([]any, 0, len(arr))
						for _, it := range arr {
							if s, _ := it.(string); s == id {
								continue
							}
							out = append(out, it)
						}
						rec.Data[p.field] = out
					case []string:
						out := make([]string, 0, len(arr))
						for _, s := range arr {
							if s == id {
								continue
							}
							out = append(out, s)
						}
						rec.Data[p.field] = out
					}
				} else {
					// одиночный ref
					rec.Data[p.field] = nil
				}
				rec.Version++
				rec.UpdatedAt = now
			}
			storage.mu.Unlock()
		}

		// --- помечаем текущую запись удалённой (soft delete) ---
		storage.mu.Lock()
		rec := storage.Data[fqn][id]
		if rec == nil || rec.Deleted {
			storage.mu.Unlock()
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}
		rec.Deleted = true
		rec.UpdatedAt = time.Now().UTC()
		rec.Version++
		storage.mu.Unlock()

		c.Status(http.StatusNoContent)
	}
}

// /api/meta/lookup/:module/:entity?field=name&q=iva&limit=10
func LookupHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")
		field := c.DefaultQuery("field", "name")
		q := strings.TrimSpace(c.DefaultQuery("q", ""))
		limitStr := c.DefaultQuery("limit", "10")
		limit, _ := strconv.Atoi(limitStr)
		if limit <= 0 || limit > 100 {
			limit = 10
		}

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{
				"errors": []FieldError{ferr(ErrTypeMismatch, "fieldName", "human message")},
			})
			return
		}

		storage.mu.RLock()
		defer storage.mu.RUnlock()

		recMap := storage.Data[fqn]
		type Row struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		}
		out := make([]Row, 0, limit)

		ql := strings.ToLower(q)
		for id, r := range recMap {
			if r.Deleted {
				continue
			}
			val := toString(r.Data[field])
			if ql == "" || strings.Contains(strings.ToLower(val), ql) {
				out = append(out, Row{ID: id, Label: val})
				if len(out) >= limit {
					break
				}
			}
		}
		c.JSON(http.StatusOK, out)
	}
}

func statusForErrors(errs []FieldError) int {
	// 409, если есть конфликтные ошибки (unique/ref)
	for _, e := range errs {
		if e.Code == ErrUniqueViolation || e.Code == ErrRefNotFound {
			return http.StatusConflict
		}
	}
	return http.StatusBadRequest
}

func CountHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")
		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		schema := storage.Schemas[fqn]

		storage.mu.RLock()
		recMap := storage.Data[fqn]
		all := make([]*Record, 0, len(recMap))
		for _, r := range recMap {
			if !r.Deleted {
				all = append(all, r)
			}
		}
		storage.mu.RUnlock()

		filtered := filterWithOps(all, schema, c.Request.URL.Query())
		c.JSON(http.StatusOK, gin.H{"total": len(filtered)})
	}
}

func RestoreHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")
		id := c.Param("id")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}

		storage.mu.Lock()
		defer storage.mu.Unlock()

		recMap := storage.Data[fqn]
		rec, ok := recMap[id]
		if !ok || rec == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}

		if rec.Deleted {
			rec.Deleted = false
			rec.UpdatedAt = time.Now().UTC()
			rec.Version++
		}
		c.JSON(http.StatusOK, rec.Data)
	}
}

func filterRecords(all []*Record, lp ListParams) []*Record {
	out := make([]*Record, 0, len(all))
	q := strings.ToLower(lp.Q)
	for _, r := range all {
		// равенства по фильтрам
		match := true
		for k, vals := range lp.Filters {
			got := toString(r.Data[k])
			okv := false
			for _, want := range vals {
				if got == want {
					okv = true
					break
				}
			}
			if !okv {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		// поиск q по строковым полям
		if q != "" {
			found := false
			for _, v := range r.Data {
				if s, ok := v.(string); ok {
					if strings.Contains(strings.ToLower(s), q) {
						found = true
						break
					}
				}
			}
			if !found {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

// выбираем поле для отображения сущности (для таблиц/ссылок)
func pickDisplayField(s *dsl.Entity) string {
	// 1) самые частые
	candidates := []string{"name", "title", "email", "code"}
	// 2) string-поля из схемы (первых пару)
	stringFields := []string{}
	for _, f := range s.Fields {
		if f.Type == "string" {
			stringFields = append(stringFields, f.Name)
		}
	}
	for _, c := range candidates {
		for _, f := range s.Fields {
			if f.Name == c {
				return c
			}
		}
	}
	if len(stringFields) > 0 {
		return stringFields[0]
	}
	// 3) fallback — id всегда есть в Data
	return "id"
}

func BulkCreateHandler(storage *Storage) gin.HandlerFunc {
	type bulkResult struct {
		Data   map[string]any `json:"data,omitempty"`
		Errors []FieldError   `json:"errors,omitempty"`
	}

	// системные поля, которые нельзя присылать на create
	sys := map[string]struct{}{
		"id": {}, "created_at": {}, "updated_at": {}, "version": {},
	}

	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		schema := storage.Schemas[fqn]

		var items []map[string]any
		if err := c.ShouldBindJSON(&items); err != nil || len(items) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON array"})
			return
		}

		results := make([]any, 0, len(items))

		for _, obj := range items {
			// 0) явная проверка системных полей (в т.ч. version)
			hasSys := false
			for k := range obj {
				if _, bad := sys[k]; bad {
					results = append(results, bulkResult{
						Errors: []FieldError{ferr(ErrReadOnly, k, "field is readonly")},
					})
					hasSys = true
					break
				}
			}
			if hasSys {
				continue
			}

			// 1) defaults
			applyDefaults(schema, obj)

			// 2) защита системных/readonly из схемы (дополнительно к явной проверке выше)
			if ers := checkReadonlyAndSystem(schema, obj, true); len(ers) > 0 {
				results = append(results, bulkResult{Errors: ers})
				continue
			}

			// 3) полная валидация (без write-lock)
			if errs := ValidateAgainstSchema(storage, schema, obj, "", fqn); len(errs) > 0 {
				results = append(results, bulkResult{Errors: errs})
				continue
			}

			// 4) запись — под write-lock
			now := time.Now().UTC()
			id := storage.newID()
			rec := &Record{
				ID:        id,
				Version:   1,
				CreatedAt: now,
				UpdatedAt: now,
				Deleted:   false,
				Data:      obj,
			}

			storage.mu.Lock()
			if storage.Data[fqn] == nil {
				storage.Data[fqn] = make(map[string]*Record)
			}
			storage.Data[fqn][id] = rec
			storage.mu.Unlock()

			// 5) в ответ — ПЛОСКИЙ формат
			results = append(results, flatten(rec))
		}

		c.JSON(http.StatusMultiStatus, results) // 207
	}
}

// POST /api/:module/:entity/_bulk/patch
// Body: { "items": [ { "id":"...", "patch":{...}, "if_match":"3" }, ... ] }
func BulkPatchHandler(storage *Storage) gin.HandlerFunc {
	type itemReq struct {
		ID      string         `json:"id"`
		Patch   map[string]any `json:"patch"`
		IfMatch string         `json:"if_match"` // опционально; версия без кавычек
	}
	type itemRes struct {
		ID     string         `json:"id"`
		Status int            `json:"status"`
		Data   map[string]any `json:"data,omitempty"`
		Errors []FieldError   `json:"errors,omitempty"`
	}
	type resp struct {
		Results []itemRes `json:"results"`
	}

	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		schema := storage.Schemas[fqn]

		var body struct {
			Items []itemReq `json:"items"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || len(body.Items) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON or empty items"})
			return
		}

		out := resp{Results: make([]itemRes, 0, len(body.Items))}
		allOK := true
		allFail := true

		for _, it := range body.Items {
			res := itemRes{ID: it.ID}

			// 1) базовые проверки
			if it.ID == "" || len(it.Patch) == 0 {
				res.Status = http.StatusBadRequest
				res.Errors = []FieldError{ferr(ErrTypeMismatch, "id", "missing id or empty patch")}
				out.Results = append(out.Results, res)
				allOK = false
				continue
			}

			// 2) защита системных/readonly
			if ers := checkReadonlyAndSystem(schema, it.Patch, false); len(ers) > 0 {
				res.Status = http.StatusBadRequest
				res.Errors = ers
				out.Results = append(out.Results, res)
				allOK = false
				continue
			}

			// 3) читаем текущую запись (RLock)
			storage.mu.RLock()
			rec := storage.Data[fqn][it.ID]
			storage.mu.RUnlock()
			if rec == nil || rec.Deleted {
				res.Status = http.StatusNotFound
				res.Errors = []FieldError{ferr(ErrNotFound, "id", "Record not found")}
				out.Results = append(out.Results, res)
				allOK = false
				continue
			}

			// 4) If-Match (первая проверка)
			if it.IfMatch != "" {
				// принимаем как число (без кавычек)
				exp, err := strconv.ParseInt(strings.Trim(it.IfMatch, `"'`), 10, 64)
				if err == nil && exp != rec.Version {
					res.Status = http.StatusConflict
					res.Errors = []FieldError{
						ferr(ErrVersionConflict, "version", fmt.Sprintf("expected %d, got %d", exp, rec.Version)),
					}
					out.Results = append(out.Results, res)
					allOK = false
					continue
				}
			}

			// 5) merge current ⊕ patch  (semantics: null -> удалить поле)
			merged := make(map[string]any, len(rec.Data)+len(it.Patch))
			for k, v := range rec.Data {
				merged[k] = v
			}
			for k, v := range it.Patch {
				if v == nil {
					delete(merged, k)
					continue
				}
				merged[k] = v
			}

			// 6) валидация merged; исключаем текущую запись из unique-поиска
			if errs := ValidateAgainstSchema(storage, schema, merged, it.ID, fqn); len(errs) > 0 {
				res.Status = statusForErrors(errs)
				res.Errors = errs
				out.Results = append(out.Results, res)
				allOK = false
				continue
			}

			// 7) запись под write-lock + финальная проверка версии
			storage.mu.Lock()
			cur := storage.Data[fqn][it.ID]
			if cur == nil || cur.Deleted {
				storage.mu.Unlock()
				res.Status = http.StatusNotFound
				res.Errors = []FieldError{ferr(ErrNotFound, "id", "Record not found")}
				out.Results = append(out.Results, res)
				allOK = false
				continue
			}
			if it.IfMatch != "" {
				if exp, err := strconv.ParseInt(strings.Trim(it.IfMatch, `"'`), 10, 64); err == nil && exp != cur.Version {
					storage.mu.Unlock()
					res.Status = http.StatusConflict
					res.Errors = []FieldError{
						ferr(ErrVersionConflict, "version", fmt.Sprintf("expected %d, got %d", exp, cur.Version)),
					}
					out.Results = append(out.Results, res)
					allOK = false
					continue
				}
			}
			cur.Data = merged
			cur.Version++
			cur.UpdatedAt = time.Now().UTC()
			flat := flatten(cur)
			storage.mu.Unlock()

			res.Status = http.StatusOK
			res.Data = flat
			out.Results = append(out.Results, res)
			allFail = false
		}

		// HTTP-статус на весь ответ
		if allOK {
			c.JSON(http.StatusOK, out)
			return
		}
		// есть и успехи, и ошибки — 207 Multi-Status
		if !allFail {
			c.JSON(http.StatusMultiStatus, out)
			return
		}
		// все упали — 400
		c.JSON(http.StatusBadRequest, out)
	}
}

type filterCond struct {
	field string
	op    string // eq, in, gt, gte, lt, lte
	vals  []string
}

// parse list conditions from query, like:
//
//	status__in=Draft,Booked
//	amount__gte=1000
//	date__lte=2025-01-31
func buildConds(q url.Values) []filterCond {
	var out []filterCond
	for key, vals := range q {
		switch key {
		case "q", "offset", "limit", "sort", "order",
			"_offset", "_limit", "_sort", "_order",
			"nulls":
			continue
		}
		if len(vals) == 0 {
			continue
		}
		// key can be: field or field__op
		field := key
		op := "eq"
		if i := strings.LastIndex(key, "__"); i > 0 {
			field = key[:i]
			op = key[i+2:]
		}
		v := vals[0]
		if strings.HasPrefix(v, "in:") {
			op = "in"
			v = strings.TrimPrefix(v, "in:")
		} else if op == "in" {
			// keep
		}
		var parts []string
		if op == "in" {
			for _, p := range strings.Split(v, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					parts = append(parts, p)
				}
			}
		} else {
			parts = []string{v}
		}
		if field != "" && len(parts) > 0 {
			out = append(out, filterCond{field: field, op: op, vals: parts})
		}
	}
	return out
}

func fieldTypeOf(schema *dsl.Entity, name string) string {
	for _, f := range schema.Fields {
		if f.Name == name {
			// нормализуем enum к "enum"
			if strings.HasPrefix(f.Type, "enum") || len(f.Enum) > 0 {
				return "enum"
			}
			return f.Type
		}
	}
	return "" // неизвестное поле
}

func compareByType(ft string, got any, op string, want string) bool {
	// равенство/IN для всего — сравниваем строковые представления
	toS := func(v any) string {
		switch t := v.(type) {
		case string:
			return t
		default:
			return fmt.Sprint(t)
		}
	}

	switch op {
	case "eq":
		return strings.EqualFold(toS(got), want)
	case "in":
		gs := toS(got)
		for _, w := range strings.Split(want, ",") {
			if strings.EqualFold(gs, strings.TrimSpace(w)) {
				return true
			}
		}
		return false
	}

	// сравнения — только для чисел и дат
	switch ft {
	case "int", "float", "money":
		// пытаться приводить к числу
		parse := func(s string) (float64, bool) {
			f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			return f, err == nil
		}
		var gv float64
		switch x := got.(type) {
		case float64:
			gv = x
		case int, int32, int64:
			gv = float64(reflect.ValueOf(x).Int())
		case string:
			if f, ok := parse(x); ok {
				gv = f
			} else {
				return false
			}
		default:
			return false
		}
		wv, ok := parse(want)
		if !ok {
			return false
		}
		switch op {
		case "gt":
			return gv > wv
		case "gte":
			return gv >= wv
		case "lt":
			return gv < wv
		case "lte":
			return gv <= wv
		default:
			return false
		}

	case "date":
		wd, err1 := time.Parse("2006-01-02", strings.TrimSpace(want))
		var gd time.Time
		switch x := got.(type) {
		case string:
			d, err := time.Parse("2006-01-02", x)
			if err != nil {
				return false
			}
			gd = d
		default:
			return false
		}
		if err1 != nil {
			return false
		}
		switch op {
		case "gt":
			return gd.After(wd)
		case "gte":
			return !gd.Before(wd)
		case "lt":
			return gd.Before(wd)
		case "lte":
			return !gd.After(wd)
		case "eq":
			return gd.Equal(wd)
		default:
			return false
		}

	case "datetime":
		wd, err1 := time.Parse(time.RFC3339, strings.TrimSpace(want))
		var gd time.Time
		switch x := got.(type) {
		case string:
			d, err := time.Parse(time.RFC3339, x)
			if err != nil {
				return false
			}
			gd = d
		default:
			return false
		}
		if err1 != nil {
			return false
		}
		switch op {
		case "gt":
			return gd.After(wd)
		case "gte":
			return !gd.Before(wd)
		case "lt":
			return gd.Before(wd)
		case "lte":
			return !gd.After(wd)
		case "eq":
			return gd.Equal(wd)
		default:
			return false
		}
	}

	// неизвестный тип/оператор — не совпало
	return false
}

func filterWithOps(all []*Record, schema *dsl.Entity, q url.Values) []*Record {
	conds := buildConds(q)
	if len(conds) == 0 && q.Get("q") == "" {
		return all
	}
	out := make([]*Record, 0, len(all))
	needle := strings.ToLower(strings.TrimSpace(q.Get("q")))

loopRecs:
	for _, r := range all {
		// 1) операторы по полям
		for _, cnd := range conds {
			ft := fieldTypeOf(schema, cnd.field)
			if ft == "" {
				// неизвестное поле — считаем, что не матчится
				continue loopRecs
			}
			got := r.Data[cnd.field]
			switch cnd.op {
			case "eq":
				if !compareByType(ft, got, "eq", cnd.vals[0]) {
					continue loopRecs
				}
			case "in":
				if !compareByType(ft, got, "in", strings.Join(cnd.vals, ",")) {
					continue loopRecs
				}
			case "gt", "gte", "lt", "lte":
				if !compareByType(ft, got, cnd.op, cnd.vals[0]) {
					continue loopRecs
				}
			default:
				continue loopRecs
			}
		}
		// 2) полнотекстовый q по строковым полям
		if needle != "" {
			found := false
			for _, v := range r.Data {
				if s, ok := v.(string); ok && strings.Contains(strings.ToLower(s), needle) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		out = append(out, r)
	}
	return out
}

func getClientVersion(c *gin.Context, body map[string]any) (int64, bool) {
	// приоритет — заголовок
	if h := strings.TrimSpace(c.GetHeader("If-Match")); h != "" {
		if v, err := strconv.ParseInt(h, 10, 64); err == nil {
			return v, true
		}
	}
	if body != nil {
		if f, ok := body["version"]; ok {
			switch t := f.(type) {
			case float64:
				return int64(t), true
			case string:
				if v, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
					return v, true
				}
			}
		}
	}
	return 0, false
}

func BulkDeleteHandler(storage *Storage) gin.HandlerFunc {
	type req struct {
		IDs []string `json:"ids"`
	}
	type res struct {
		ID     string       `json:"id,omitempty"`
		Errors []FieldError `json:"errors,omitempty"`
	}
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")
		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}

		var body req
		if err := c.ShouldBindJSON(&body); err != nil || len(body.IDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON: expected {ids:[]}"})
			return
		}

		results := make([]any, 0, len(body.IDs))
		now := time.Now().UTC()

		for _, id := range body.IDs {
			// FK-protect: запрет удаления, если на запись есть ссылки
			if refEnt, refField, inUse := storage.FindIncomingRefs(fqn, id); inUse {
				results = append(results, res{
					ID: id,
					Errors: []FieldError{{
						Code:    "fk_in_use",
						Field:   refField,
						Message: fmt.Sprintf("record is referenced by %s.%s", refEnt, refField),
					}},
				})
				continue
			}

			storage.mu.Lock()
			rec := storage.Data[fqn][id]
			if rec == nil || rec.Deleted {
				storage.mu.Unlock()
				results = append(results, res{
					ID:     id,
					Errors: []FieldError{ferr(ErrNotFound, "id", "Record not found")},
				})
				continue
			}
			rec.Deleted = true
			rec.UpdatedAt = now
			rec.Version++
			storage.mu.Unlock()

			results = append(results, gin.H{"id": id})
		}

		c.JSON(http.StatusMultiStatus, results) // 207, как в bulk create
	}
}
func BulkRestoreHandler(storage *Storage) gin.HandlerFunc {
	type req struct {
		IDs []string `json:"ids"`
	}
	type res struct {
		ID     string       `json:"id,omitempty"`
		Errors []FieldError `json:"errors,omitempty"`
	}
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")
		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}

		var body req
		if err := c.ShouldBindJSON(&body); err != nil || len(body.IDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON: expected {ids:[]}"})
			return
		}

		results := make([]any, 0, len(body.IDs))
		now := time.Now().UTC()

		for _, id := range body.IDs {
			storage.mu.Lock()
			rec := storage.Data[fqn][id]
			if rec == nil {
				storage.mu.Unlock()
				results = append(results, res{
					ID:     id,
					Errors: []FieldError{ferr(ErrNotFound, "id", "Record not found")},
				})
				continue
			}
			if rec.Deleted {
				rec.Deleted = false
				rec.UpdatedAt = now
				rec.Version++
			}
			storage.mu.Unlock()

			results = append(results, gin.H{"id": id})
		}

		c.JSON(http.StatusMultiStatus, results)
	}
}

// readExpectedVersion читает ожидаемую версию из If-Match либо из payload["version"] (число).
func readExpectedVersion(c *gin.Context, payload map[string]any) (int64, bool) {
	// 1) If-Match: допускаем просто число (например, "3")
	ifMatch := strings.TrimSpace(c.GetHeader("If-Match"))
	if ifMatch != "" {
		// уберём кавычки/weak-префикс вида W/"3"
		if strings.HasPrefix(ifMatch, "W/") {
			ifMatch = strings.TrimPrefix(ifMatch, "W/")
		}
		ifMatch = strings.Trim(ifMatch, `"'`)
		if v, err := strconv.ParseInt(ifMatch, 10, 64); err == nil {
			return v, true
		}
	}
	// 2) из тела: "version": <int>
	if payload != nil {
		if raw, ok := payload["version"]; ok {
			switch t := raw.(type) {
			case float64:
				// JSON number → float64
				return int64(t), true
			case string:
				if v, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
					return v, true
				}
			}
		}
	}
	return 0, false
}

// mergeForPatch выполняет merge current+patch с учетом политики nulls.
// Если nullsDelete=true — ключи с null в patch удаляются из результата.
func mergeForPatch(current, patch map[string]any, nullsDelete bool) map[string]any {
	out := make(map[string]any, len(current)+len(patch))
	for k, v := range current {
		out[k] = v
	}
	for k, v := range patch {
		if v == nil && nullsDelete {
			delete(out, k)
		} else {
			out[k] = v
		}
	}
	return out
}

// GET /api/meta/catalogs
func MetaCatalogsHandler(storage *Storage) gin.HandlerFunc {
	type Row struct {
		Name  string   `json:"name"`
		Codes []string `json:"codes"`
	}
	return func(c *gin.Context) {
		rows := make([]Row, 0, len(storage.Enums))
		for name, dir := range storage.Enums {
			codes := make([]string, 0, len(dir.Items))
			for _, it := range dir.Items {
				codes = append(codes, it.Code)
			}
			rows = append(rows, Row{Name: name, Codes: codes})
		}
		c.JSON(http.StatusOK, rows)
	}
}
