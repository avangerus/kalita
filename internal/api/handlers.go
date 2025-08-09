package api

import (
	"encoding/json"
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

		// defaults
		applyDefaults(schema, obj)

		// защита системных/readonly
		if ers := checkReadonlyAndSystem(schema, obj, true); len(ers) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"errors": ers})
			return
		}

		// Валидация — БЕЗ write-lock
		if errs := ValidateAgainstSchema(storage, schema, obj, "", entity); len(errs) > 0 {
			c.JSON(statusForErrors(errs), gin.H{"errors": errs})
			return
		}

		// Запись — под write-lock
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

		// 1) фильтры с операторами
		filtered := filterWithOps(all, schema, c.Request.URL.Query())

		// 2) сортировка/пагинация
		lp := parseListParams(c.Request.URL.Query()) // limit/offset/sort/q
		if len(lp.Sort) > 0 {
			sortRecordsMulti(filtered, lp.Sort)
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

		// 3) ответ — «плоский» + total в заголовке
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
		c.Header("ETag", fmt.Sprintf(`"%d"`, rec.Version))
		c.JSON(http.StatusOK, flatten(rec)) // ← ключевое
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

		// читаем ожидаемую версию ДО того, как уберём version из payload
		expVer, okExp := readExpectedVersion(c, obj)

		// запрет системных/readonly (поле version будет удалено из obj внутри)
		if ers := checkReadonlyAndSystem(schema, obj, false); len(ers) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"errors": ers})
			return
		}

		// запрет системных/readonly
		if ers := checkReadonlyAndSystem(schema, obj, false); len(ers) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"errors": ers})
			return
		}

		// читаем текущую версию под RLock
		storage.mu.RLock()
		rec := storage.Data[fqn][id]
		curVer := int64(0)
		if rec != nil && !rec.Deleted {
			curVer = rec.Version
		}
		storage.mu.RUnlock()
		if rec == nil || rec.Deleted {
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}

		// ожидаемую версию берём из If-Match или body.version
		//expVer, okExp := readExpectedVersion(c, obj)
		if !okExp || expVer != curVer {
			c.JSON(http.StatusConflict, gin.H{
				"errors": []FieldError{ferr(ErrVersionConflict, "version",
					fmt.Sprintf("expected version %d", curVer))},
			})
			return
		}

		// применяем под write-lock с повторной проверкой версии (на случай гонки)
		now := time.Now().UTC()
		storage.mu.Lock()
		rec2 := storage.Data[fqn][id]
		if rec2 == nil || rec2.Deleted {
			storage.mu.Unlock()
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}
		if rec2.Version != curVer {
			storage.mu.Unlock()
			c.JSON(http.StatusConflict, gin.H{
				"errors": []FieldError{ferr(ErrVersionConflict, "version",
					fmt.Sprintf("expected version %d", rec2.Version))},
			})
			return
		}

		rec2.Data = obj
		rec2.Version++
		rec2.UpdatedAt = now
		storage.mu.Unlock()

		c.JSON(http.StatusOK, flatten(rec2))
	}
}

// PATCH /api/:module/:entity/:id
func UpdatePartialHandler(storage *Storage) gin.HandlerFunc {
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

		// читаем ожидаемую версию ДО удаления поля version
		expVer, okExp := readExpectedVersion(c, patch)

		// Запрет системных и readonly полей (version удалится отсюда)
		if ers := checkReadonlyAndSystem(schema, patch, false); len(ers) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"errors": ers})
			return
		}

		// readonly/system защита
		if ers := checkReadonlyAndSystem(schema, patch, false); len(ers) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"errors": ers})
			return
		}

		// --- читаем текущую запись под RLock
		storage.mu.RLock()
		rec := storage.Data[fqn][id]
		if rec == nil || rec.Deleted {
			storage.mu.RUnlock()
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}
		curVer := rec.Version
		current := make(map[string]any, len(rec.Data))
		for k, v := range rec.Data {
			current[k] = v
		}
		storage.mu.RUnlock()

		// версия: If-Match или body.version должны совпасть
		//expVer, okExp := readExpectedVersion(c, patch)
		if !okExp || expVer != curVer {
			c.JSON(http.StatusConflict, gin.H{
				"errors": []FieldError{ferr(ErrVersionConflict, "version",
					fmt.Sprintf("expected version %d", curVer))},
			})
			return
		}

		// merge + validate без локов
		merged := make(map[string]any, len(current)+len(patch))
		for k, v := range current {
			merged[k] = v
		}
		for k, v := range patch {
			merged[k] = v
		}

		if errs := ValidateAgainstSchema(storage, schema, merged, id, fqn); len(errs) > 0 {
			c.JSON(statusForErrors(errs), gin.H{"errors": errs})
			return
		}

		// применяем под write-lock c повторной проверкой версии
		now := time.Now().UTC()
		storage.mu.Lock()
		rec2 := storage.Data[fqn][id]
		if rec2 == nil || rec2.Deleted {
			storage.mu.Unlock()
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}
		if rec2.Version != curVer {
			storage.mu.Unlock()
			c.JSON(http.StatusConflict, gin.H{
				"errors": []FieldError{ferr(ErrVersionConflict, "version",
					fmt.Sprintf("expected version %d", rec2.Version))},
			})
			return
		}
		for k, v := range patch {
			rec2.Data[k] = v
		}
		rec2.Version++
		rec2.UpdatedAt = now
		storage.mu.Unlock()

		c.JSON(http.StatusOK, flatten(rec2))
	}
}

// DELETE /api/:entity/:id  (soft delete)
// DELETE /api/:module/:entity/:id
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

		type FieldOut struct {
			Name            string            `json:"name"`
			Type            string            `json:"type"`
			Options         map[string]string `json:"options,omitempty"`
			Enum            []string          `json:"enum,omitempty"`
			Ref             string            `json:"ref,omitempty"`
			RefDisplayField string            `json:"refDisplayField,omitempty"`
			Elem            string            `json:"elem_type,omitempty"`

			// Расширенные метаданные (удобно фронту)
			Required bool   `json:"required,omitempty"`
			Unique   bool   `json:"unique,omitempty"`
			Readonly bool   `json:"readonly,omitempty"`
			Default  string `json:"default,omitempty"`  // как строка из DSL
			OnDelete string `json:"onDelete,omitempty"` // для ref: restrict/set_null/cascade
		}
		resp := struct {
			Module       string     `json:"module"`
			Name         string     `json:"name"`
			DisplayField string     `json:"displayField"`
			Fields       []FieldOut `json:"fields"`
			Constraints  struct {
				Unique [][]string `json:"unique,omitempty"`
			} `json:"constraints"`
		}{
			Module:       schema.Module,
			Name:         schema.Name,
			DisplayField: pickDisplayField(schema),
			Fields:       make([]FieldOut, 0, len(schema.Fields)),
		}
		resp.Constraints.Unique = schema.Constraints.Unique

		for _, f := range schema.Fields {
			fo := FieldOut{
				Name:    f.Name,
				Type:    f.Type,
				Options: f.Options,
			}

			// дополнительные удобные флаги
			if f.Options != nil {
				if strings.EqualFold(f.Options["required"], "true") {
					fo.Required = true
				}
				if strings.EqualFold(f.Options["unique"], "true") {
					fo.Unique = true
				}
				if strings.EqualFold(f.Options["readonly"], "true") {
					fo.Readonly = true
				}
				if def := f.Options["default"]; def != "" {
					fo.Default = def
				}
				if od := f.Options["on_delete"]; od != "" {
					fo.OnDelete = od
				}
			}

			// enum: если парсер не заполнил f.Enum, но Type вида "enum[A, B, C]" — распарсим
			if len(f.Enum) == 0 && strings.HasPrefix(f.Type, "enum[") {
				if i := strings.Index(f.Type, "["); i >= 0 {
					if j := strings.LastIndex(f.Type, "]"); j > i {
						raw := f.Type[i+1 : j]
						parts := strings.Split(raw, ",")
						vals := make([]string, 0, len(parts))
						for _, p := range parts {
							s := strings.TrimSpace(p)
							if s != "" {
								// убираем случайные кавычки, если они есть
								s = strings.Trim(s, `"'`)
								vals = append(vals, s)
							}
						}
						if len(vals) > 0 {
							fo.Type = "enum"
							fo.Enum = vals
						}
					}
				}
			} else if len(f.Enum) > 0 {
				fo.Enum = append(fo.Enum, f.Enum...)
				fo.Type = "enum"
			}

			// ref / array[ref]
			tgtFQN := ""
			if f.Type == "ref" && f.RefTarget != "" {
				tgtFQN = f.RefTarget
			}
			if f.Type == "array" && f.ElemType == "ref" && f.RefTarget != "" {
				fo.Elem = f.ElemType
				tgtFQN = f.RefTarget
			}
			if tgtFQN == "" && f.Options != nil && f.Options["ref"] != "" {
				tgtFQN = f.Options["ref"]
			}
			if tgtFQN != "" {
				// дополним модулем, если не указан
				if !strings.Contains(tgtFQN, ".") {
					tgtFQN = schema.Module + "." + tgtFQN
				}
				fo.Ref = tgtFQN
				// найдём целевую схему и её displayField
				if tgtSchema, ok := storage.Schemas[tgtFQN]; ok && tgtSchema != nil {
					fo.RefDisplayField = pickDisplayField(tgtSchema)
				}
			}

			resp.Fields = append(resp.Fields, fo)
		}

		c.JSON(http.StatusOK, resp)
	}
}

func MetaListHandler(storage *Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		type Row struct {
			Module string `json:"module"`
			Name   string `json:"name"`
			Fields int    `json:"fields"`
		}
		rows := make([]Row, 0, len(storage.Schemas))
		for _, e := range storage.Schemas {
			rows = append(rows, Row{
				Module: e.Module,
				Name:   e.Name,
				Fields: len(e.Fields), // ← используем Fields из твоей Entity
			})
		}
		c.JSON(http.StatusOK, rows)
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
			// 1) defaults
			applyDefaults(schema, obj)

			// 2) защита системных/readonly
			if ers := checkReadonlyAndSystem(schema, obj, true); len(ers) > 0 {
				results = append(results, bulkResult{Errors: ers})
				continue
			}

			applyDefaults(schema, obj)
			if ers := checkReadonlyAndSystem(schema, obj, true); len(ers) > 0 {
				results = append(results, gin.H{"errors": ers})
				continue
			}

			// 3) полная валидация (без write-lock)
			if errs := ValidateAgainstSchema(storage, schema, obj, "", fqn); len(errs) > 0 {
				results = append(results, bulkResult{Errors: errs})
				continue
			}

			// 4) запись — под write-lock (как в CreateHandler)
			now := time.Now().UTC()
			id := storage.newID()

			rec := &Record{
				ID:        id,
				Version:   1, // ← важно для оптимистической блокировки
				CreatedAt: now,
				UpdatedAt: now,
				Deleted:   false,
				Data:      obj, // мета не кладём внутрь Data
			}

			storage.mu.Lock()
			if storage.Data[fqn] == nil {
				storage.Data[fqn] = make(map[string]*Record)
			}
			storage.Data[fqn][id] = rec
			storage.mu.Unlock()

			// 5) в ответ — ПЛОСКИЙ формат как в CreateHandler
			results = append(results, flatten(rec))
		}

		// смешанные результаты — 207 Multi-Status
		c.JSON(http.StatusMultiStatus, results)
	}
}

func BulkPatchHandler(storage *Storage) gin.HandlerFunc {
	type itemReq struct {
		ID      string         `json:"id"`
		Patch   map[string]any `json:"patch"`
		Version *int64         `json:"version,omitempty"` // per-item version hint
	}
	type legacyReq struct {
		IDs   []string       `json:"ids"`
		Patch map[string]any `json:"patch"`
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

		// Разбираем либо массив itemReq, либо legacyReq
		var items []itemReq
		dec := json.NewDecoder(c.Request.Body)
		dec.UseNumber()

		// Попробуем сначала как массив
		tok, _ := dec.Token()
		switch tt := tok.(type) {
		case json.Delim:
			if tt == '[' {
				// читаем массив объектов
				for dec.More() {
					var it itemReq
					if err := dec.Decode(&it); err != nil {
						c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON array"})
						return
					}
					if it.ID == "" || it.Patch == nil {
						c.JSON(http.StatusBadRequest, gin.H{"error": "Each item must have id and patch"})
						return
					}
					items = append(items, it)
				}
				// съедаем закрывающую ']'
				if _, err := dec.Token(); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON array end"})
					return
				}
			} else if tt == '{' {
				// это объект — legacy формат
				var lr legacyReq
				if err := dec.Decode(&lr); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid legacy JSON"})
					return
				}
				if len(lr.IDs) == 0 || lr.Patch == nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid legacy JSON: expected {ids:[], patch:{}}"})
					return
				}
				for _, id := range lr.IDs {
					id = strings.TrimSpace(id)
					if id == "" {
						continue
					}
					// копию patch на каждый элемент
					p := make(map[string]any, len(lr.Patch))
					for k, v := range lr.Patch {
						p[k] = v
					}
					items = append(items, itemReq{ID: id, Patch: p})
				}
				// съедаем закрывающую '}'
				if _, err := dec.Token(); err != nil {
					// noop
				}
			} else {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON root"})
				return
			}
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON root"})
			return
		}

		results := make([]any, 0, len(items))
		now := time.Now().UTC()

		for _, it := range items {
			id := it.ID
			patch := it.Patch

			// Считаем ожидаемую версию ДО чистки readonly
			expVer, haveVer := readExpectedVersion(c, patch)
			// Если в itemReq.version передали — используем его приоритетно
			if it.Version != nil {
				expVer = *it.Version
				haveVer = true
			}

			// readonly/system защита (уберёт version из patch, чтобы не записать)
			if ers := checkReadonlyAndSystem(schema, patch, false); len(ers) > 0 {
				results = append(results, gin.H{"id": id, "errors": ers})
				continue
			}

			// --- читаем текущую запись под RLock
			storage.mu.RLock()
			rec := storage.Data[fqn][id]
			if rec == nil || rec.Deleted {
				storage.mu.RUnlock()
				results = append(results, gin.H{"id": id, "errors": []FieldError{ferr(ErrNotFound, "id", "Record not found")}})
				continue
			}
			curVer := rec.Version
			current := make(map[string]any, len(rec.Data))
			for k, v := range rec.Data {
				current[k] = v
			}
			storage.mu.RUnlock()

			// версия обязательна
			if !haveVer || expVer != curVer {
				results = append(results, gin.H{
					"id": id,
					"errors": []FieldError{ferr(ErrVersionConflict, "version",
						fmt.Sprintf("expected version %d", curVer))},
				})
				continue
			}

			// merge + validate без локов
			merged := make(map[string]any, len(current)+len(patch))
			for k, v := range current {
				merged[k] = v
			}
			for k, v := range patch {
				merged[k] = v
			}
			if errs := ValidateAgainstSchema(storage, schema, merged, id, fqn); len(errs) > 0 {
				results = append(results, gin.H{"id": id, "errors": errs})
				continue
			}

			// применяем под write-lock с повторной проверкой версии
			storage.mu.Lock()
			rec2 := storage.Data[fqn][id]
			if rec2 == nil || rec2.Deleted {
				storage.mu.Unlock()
				results = append(results, gin.H{"id": id, "errors": []FieldError{ferr(ErrNotFound, "id", "Record not found")}})
				continue
			}
			if rec2.Version != curVer {
				vv := rec2.Version
				storage.mu.Unlock()
				results = append(results, gin.H{"id": id, "errors": []FieldError{ferr(ErrVersionConflict, "version",
					fmt.Sprintf("expected version %d", vv))}})
				continue
			}
			for k, v := range patch {
				rec2.Data[k] = v
			}
			rec2.Version++
			rec2.UpdatedAt = now

			// отдаём свежие данные в «плоском» виде
			out := flatten(rec2)
			storage.mu.Unlock()

			results = append(results, out)
		}

		// 207 Multi-Status — для смешанных результатов
		c.JSON(http.StatusMultiStatus, results)
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
		if key == "q" || key == "offset" || key == "limit" || key == "sort" || key == "order" {
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
