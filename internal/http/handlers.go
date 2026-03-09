package http

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"kalita/internal/runtime"
	"kalita/internal/schema"
	"kalita/internal/validation"

	"github.com/gin-gonic/gin"
)

// POST /api/:entity
// POST /api/:module/:entity
// POST /api/:entity  и /api/:module/:entity
func CreateHandler(storage *runtime.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawModule := c.Param("module")
		rawEntity := c.Param("entity")

		entity, ok := storage.NormalizeEntityName(rawModule, rawEntity)
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{
				"errors": []validation.FieldError{validationFerr(validation.ErrTypeMismatch, "entity", "Entity not found")},
			})
			return
		}
		entitySchema := storage.Schemas[entity]

		var obj map[string]interface{}
		if err := c.ShouldBindJSON(&obj); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}

		// 1) defaults
		applyDefaults(entitySchema, obj)

		// 2) защита системных/readonly
		if ers := checkReadonlyAndSystem(entitySchema, obj, true); len(ers) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"errors": ers})
			return
		}

		// 3) валидация (без write-lock)
		if errs := validation.ValidateAgainstSchema(storage, entitySchema, obj, "", entity); len(errs) > 0 {
			c.JSON(statusForErrors(errs), gin.H{"errors": errs})
			return
		}

		// 4) запись (под write-lock)
		storage.Mu.Lock()
		defer storage.Mu.Unlock()

		if storage.Data[entity] == nil {
			storage.Data[entity] = make(map[string]*runtime.Record)
		}

		id := storage.NewID()
		now := time.Now().UTC()
		rec := &runtime.Record{
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
func ListHandler(storage *runtime.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		entitySchema := storage.Schemas[fqn]

		// читаем все «живые» записи
		storage.Mu.RLock()
		recMap := storage.Data[fqn]
		all := make([]*runtime.Record, 0, len(recMap))
		for _, r := range recMap {
			if !r.Deleted {
				all = append(all, r)
			}
		}
		storage.Mu.RUnlock()

		// единый query: удаляем служебное nulls, чтобы не попало в фильтры
		q := c.Request.URL.Query()
		q.Del("nulls")

		// 1) фильтры
		filtered := filterWithOps(all, entitySchema, q)

		// 2) сортировка/пагинация
		lp := runtime.ParseListParams(q) // понимает _limit/_offset/_sort и nulls
		if len(lp.Sort) > 0 {
			runtime.SortRecordsMultiNulls(filtered, lp.Sort, lp.Nulls)
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
func GetOneHandler(storage *runtime.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")
		id := c.Param("id")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}

		// [1.2] handlers.go:GetOneHandler — parse expand/depth/full (generic)
		full := c.DefaultQuery("full", "")
		expRaw := strings.TrimSpace(c.DefaultQuery("_expand", ""))
		depthStr := strings.TrimSpace(c.DefaultQuery("_depth", ""))

		const MaxExpandDepth = 5
		const MaxNestedItems = 10000

		depth := 0
		if full == "1" {
			expRaw = "*"
			depth = MaxExpandDepth
		}
		if depthStr != "" {
			if v, err := strconv.Atoi(depthStr); err == nil {
				if v < 0 {
					v = 0
				}
				if v > MaxExpandDepth {
					v = MaxExpandDepth
				}
				depth = v
			}
		}

		expandAll := (expRaw == "*" || strings.EqualFold(expRaw, "all"))
		expandSet := map[string]bool{}
		if !expandAll && expRaw != "" {
			for _, t := range strings.Split(expRaw, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					expandSet[t] = true
				}
			}
		}

		storage.Mu.RLock()
		rec := storage.Data[fqn][id]
		storage.Mu.RUnlock()
		if rec == nil || rec.Deleted {
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}

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
		// [A] handlers.go:GetOneHandler — универсальный expand
		base := flatten(rec)

		if depth == 0 || (!expandAll && len(expandSet) == 0) {
			// плоская запись, как раньше
			c.Header("ETag", fmt.Sprintf(`"%d"`, rec.Version))
			c.JSON(http.StatusOK, base)
			return
		}

		parentFQN := fqn
		schemas := storage.Schemas

		type node struct {
			FQN string
			IDs []string
		}
		type key struct{ FQN, FK string }

		resultChildren := map[string]map[string]map[string]any{} // FQN -> id -> obj
		nestedCount := 0
		truncated := false

		queue := []node{{FQN: parentFQN, IDs: []string{id}}}
		curDepth := 0

		visitedNode := make(map[string]struct{}) // ключ: FQN:id

		for len(queue) > 0 && curDepth < MaxExpandDepth && curDepth < depth && !truncated {
			nextQueue := []node{}
			need := make(map[key][]string)

			for _, n := range queue {
				children := discoverChildren(storage, schemas, n.FQN, expandAll, expandSet)
				for _, ch := range children {
					k := key{FQN: ch.ChildFQN, FK: ch.FK}
					need[k] = append(need[k], n.IDs...)
				}
			}

			for k, ids := range need {
				need[k] = uniqStrings(ids)
			}

			for k, parentIDs := range need {
				children := batchByFK(storage, k.FQN, k.FK, parentIDs)
				if len(children) == 0 {
					continue
				}

				// храним детей без дублей: FQN -> id -> объект
				if _, ok := resultChildren[k.FQN]; !ok {
					resultChildren[k.FQN] = make(map[string]map[string]any, len(children))
				}

				childIDs := make([]string, 0, len(children))
				for _, ch := range children {
					cid := fmt.Sprint(ch["id"])
					// сложим/перезапишем по id (убирает дубликаты из разных путей)
					resultChildren[k.FQN][cid] = ch

					// не добавляем в очередь один и тот же узел повторно
					nodeKey := k.FQN + ":" + cid
					if _, seen := visitedNode[nodeKey]; seen {
						continue
					}
					visitedNode[nodeKey] = struct{}{}
					childIDs = append(childIDs, cid)
				}
				if len(childIDs) > 0 {
					nextQueue = append(nextQueue, node{FQN: k.FQN, IDs: childIDs})
				}

				nestedCount += len(children)
				if nestedCount > MaxNestedItems {
					truncated = true
					break
				}
			}

			queue = nextQueue
			curDepth++
		}
		outChildren := make(map[string][]map[string]any, len(resultChildren))
		for fqn, byID := range resultChildren {
			arr := make([]map[string]any, 0, len(byID))
			for _, obj := range byID {
				arr = append(arr, obj)
			}
			outChildren[fqn] = arr
		}
		base["_children"] = outChildren

		base["_expand_depth"] = curDepth
		if truncated {
			base["_truncated"] = true
		}

		c.Header("ETag", fmt.Sprintf(`"%d"`, rec.Version))
		c.JSON(http.StatusOK, base)
	}
}

// PUT /api/:module/:entity/:id
func UpdateHandler(storage *runtime.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")
		id := c.Param("id")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		entitySchema := storage.Schemas[fqn]

		var obj map[string]any
		if err := c.ShouldBindJSON(&obj); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}

		// 1) ожидаемая версия (If-Match или body.version)
		expVer, okExp := readExpectedVersion(c, obj)

		// 2) защита системных/readonly (на update — ругаемся)
		if ers := checkReadonlyAndSystem(entitySchema, obj, false); len(ers) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"errors": ers})
			return
		}

		// 3) читаем текущую запись и версию (RLock)
		storage.Mu.RLock()
		rec := storage.Data[fqn][id]
		storage.Mu.RUnlock()
		if rec == nil || rec.Deleted {
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}

		// 4) оптимистическая блокировка, если версия ожидалась
		if okExp && expVer != rec.Version {
			c.JSON(http.StatusConflict, gin.H{"errors": []validation.FieldError{
				validationFerr(validation.ErrVersionConflict, "version", fmt.Sprintf("expected %d, got %d", expVer, rec.Version)),
			}})
			return
		}

		// 5) валидация (без write-lock); исключаем текущую запись из unique-поиска
		if errs := validation.ValidateAgainstSchema(storage, entitySchema, obj, id, fqn); len(errs) > 0 {
			c.JSON(statusForErrors(errs), gin.H{"errors": errs})
			return
		}

		// 6) запись (под write-lock) + финальная проверка версии от гонок
		storage.Mu.Lock()
		defer storage.Mu.Unlock()

		cur := storage.Data[fqn][id]
		if cur == nil || cur.Deleted {
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}
		if okExp && expVer != cur.Version {
			c.JSON(http.StatusConflict, gin.H{"errors": []validation.FieldError{
				validationFerr(validation.ErrVersionConflict, "version", fmt.Sprintf("expected %d, got %d", expVer, cur.Version)),
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
func PatchHandler(storage *runtime.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")
		id := c.Param("id")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		entitySchema := storage.Schemas[fqn]

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
		if ers := checkReadonlyAndSystem(entitySchema, patch, false); len(ers) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"errors": ers})
			return
		}

		// читаем текущую запись
		storage.Mu.RLock()
		rec := storage.Data[fqn][id]
		storage.Mu.RUnlock()
		if rec == nil || rec.Deleted {
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}

		// первая проверка версии (optimistic locking)
		if okExp && expVer != rec.Version {
			c.JSON(http.StatusConflict, gin.H{"errors": []validation.FieldError{
				validationFerr(validation.ErrVersionConflict, "version", fmt.Sprintf("expected %d, got %d", expVer, rec.Version)),
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
		if errs := validation.ValidateAgainstSchema(storage, entitySchema, merged, id, fqn); len(errs) > 0 {
			c.JSON(statusForErrors(errs), gin.H{"errors": errs})
			return
		}

		// запись под write-lock + финальная сверка версии
		storage.Mu.Lock()
		defer storage.Mu.Unlock()

		cur := storage.Data[fqn][id]
		if cur == nil || cur.Deleted {
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}
		if okExp && expVer != cur.Version {
			c.JSON(http.StatusConflict, gin.H{"errors": []validation.FieldError{
				validationFerr(validation.ErrVersionConflict, "version", fmt.Sprintf("expected %d, got %d", expVer, cur.Version)),
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
func DeleteHandler(storage *runtime.Storage) gin.HandlerFunc {
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
			field string // имя поля
		}
		var toNull []pendingNull

		storage.Mu.RLock()
		for childFQN, childSchema := range storage.Schemas {
			childData := storage.Data[childFQN]
			if childData == nil {
				continue
			}
			for _, f := range childSchema.Fields {
				if f.Type != "ref" || f.RefTarget == "" {
					continue
				}
				refMod := childSchema.Module
				refEnt := f.RefTarget
				if strings.Contains(refEnt, ".") {
					parts := strings.SplitN(refEnt, ".", 2)
					refMod, refEnt = parts[0], parts[1]
				}
				refFQN, ok := storage.NormalizeEntityName(refMod, refEnt)
				if !ok || refFQN != fqn {
					continue
				}
				policy := strings.ToLower(strings.TrimSpace(f.Options["on_delete"]))

				for childID, childRec := range childData {
					if childRec.Deleted {
						continue
					}
					if refVal, ok := childRec.Data[f.Name]; ok {
						if idStr, ok := refVal.(string); ok && idStr == id {
							switch policy {
							case "restrict":
								storage.Mu.RUnlock()
								c.JSON(http.StatusConflict, gin.H{"error": "Cannot delete: referenced by " + childFQN + "." + f.Name})
								return
							case "set_null":
								toNull = append(toNull, pendingNull{ent: childFQN, id: childID, field: f.Name})
							case "cascade":
								// TODO: cascade delete (рекурсивно)
							}
						}
					}
				}
			}
		}
		storage.Mu.RUnlock()

		// apply ON_DELETE SET NULL
		if len(toNull) > 0 {
			storage.Mu.Lock()
			for _, p := range toNull {
				rec := storage.Data[p.ent][p.id]
				if rec != nil && !rec.Deleted {
					rec.Data[p.field] = nil
					rec.Version++
					rec.UpdatedAt = time.Now().UTC()
				}
			}
			storage.Mu.Unlock()
		}

		// физическое удаление (soft delete)
		storage.Mu.Lock()
		rec := storage.Data[fqn][id]
		if rec == nil || rec.Deleted {
			storage.Mu.Unlock()
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}
		rec.Deleted = true
		rec.Version++
		rec.UpdatedAt = time.Now().UTC()
		storage.Mu.Unlock()

		c.JSON(http.StatusOK, gin.H{"ok": true, "deleted": id})
	}
}

// ==== BULK HANDLERS ====

func BulkCreateHandler(storage *runtime.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		entitySchema := storage.Schemas[fqn]

		var list []map[string]interface{}
		if err := c.ShouldBindJSON(&list); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}

		out := make([]map[string]any, 0, len(list))
		var errs []validation.FieldError

		storage.Mu.Lock()
		defer storage.Mu.Unlock()

		for i, obj := range list {
			// defaults
			applyDefaults(entitySchema, obj)

			// protection
			if ers := checkReadonlyAndSystem(entitySchema, obj, true); len(ers) > 0 {
				for j := range ers {
					ers[j].Field = fmt.Sprintf("[%d].%s", i, ers[j].Field)
				}
				errs = append(errs, ers...)
				continue
			}

			// validation
			if verrs := validation.ValidateAgainstSchema(storage, entitySchema, obj, "", fqn); len(verrs) > 0 {
				for j := range verrs {
					verrs[j].Field = fmt.Sprintf("[%d].%s", i, verrs[j].Field)
				}
				errs = append(errs, verrs...)
				continue
			}

			// create
			if storage.Data[fqn] == nil {
				storage.Data[fqn] = make(map[string]*runtime.Record)
			}
			id := storage.NewID()
			now := time.Now().UTC()
			rec := &runtime.Record{
				ID:        id,
				Version:   1,
				CreatedAt: now,
				UpdatedAt: now,
				Data:      obj,
			}
			storage.Data[fqn][id] = rec
			out = append(out, flatten(rec))
		}

		if len(errs) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"errors": errs})
			return
		}
		c.JSON(http.StatusCreated, out)
	}
}

func BulkPatchHandler(storage *runtime.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}
		entitySchema := storage.Schemas[fqn]

		type patchItem struct {
			ID      string                 `json:"id"`
			Version int64                  `json:"version,omitempty"`
			Data    map[string]interface{} `json:"data"`
		}
		var list []patchItem
		if err := c.ShouldBindJSON(&list); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}

		out := make([]map[string]any, 0, len(list))
		var errs []validation.FieldError

		storage.Mu.Lock()
		defer storage.Mu.Unlock()

		for i, item := range list {
			cur := storage.Data[fqn][item.ID]
			if cur == nil || cur.Deleted {
				errs = append(errs, validationFerr(validation.ErrNotFound, fmt.Sprintf("[%d].id", i), "Record not found"))
				continue
			}
			if item.Version > 0 && item.Version != cur.Version {
				errs = append(errs, validationFerr(validation.ErrVersionConflict, fmt.Sprintf("[%d].version", i), fmt.Sprintf("expected %d, got %d", item.Version, cur.Version)))
				continue
			}

			// shallow merge
			merged := make(map[string]any, len(cur.Data)+len(item.Data))
			for k, v := range cur.Data {
				merged[k] = v
			}
			for k, v := range item.Data {
				if v == nil {
					delete(merged, k)
					continue
				}
				merged[k] = v
			}

			if verrs := validation.ValidateAgainstSchema(storage, entitySchema, merged, item.ID, fqn); len(verrs) > 0 {
				for j := range verrs {
					verrs[j].Field = fmt.Sprintf("[%d].%s", i, verrs[j].Field)
				}
				errs = append(errs, verrs...)
				continue
			}

			cur.Data = merged
			cur.Version++
			cur.UpdatedAt = time.Now().UTC()
			out = append(out, flatten(cur))
		}

		if len(errs) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"errors": errs})
			return
		}
		c.JSON(http.StatusOK, out)
	}
}

func BulkDeleteHandler(storage *runtime.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}

		var ids []string
		if err := c.ShouldBindJSON(&ids); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}

		storage.Mu.Lock()
		defer storage.Mu.Unlock()

		now := time.Now().UTC()
		deleted := make([]string, 0, len(ids))
		for _, id := range ids {
			rec := storage.Data[fqn][id]
			if rec != nil && !rec.Deleted {
				rec.Deleted = true
				rec.Version++
				rec.UpdatedAt = now
				deleted = append(deleted, id)
			}
		}
		c.JSON(http.StatusOK, gin.H{"deleted": deleted})
	}
}

func BulkRestoreHandler(storage *runtime.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}

		var ids []string
		if err := c.ShouldBindJSON(&ids); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
			return
		}

		storage.Mu.Lock()
		defer storage.Mu.Unlock()

		now := time.Now().UTC()
		restored := make([]string, 0, len(ids))
		for _, id := range ids {
			rec := storage.Data[fqn][id]
			if rec != nil && rec.Deleted {
				rec.Deleted = false
				rec.Version++
				rec.UpdatedAt = now
				restored = append(restored, id)
			}
		}
		c.JSON(http.StatusOK, gin.H{"restored": restored})
	}
}

func BatchGetHandler(storage *runtime.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}

		ids := c.QueryArray("ids")
		if len(ids) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ids required"})
			return
		}

		storage.Mu.RLock()
		defer storage.Mu.RUnlock()

		out := make([]map[string]any, 0, len(ids))
		for _, id := range ids {
			rec := storage.Data[fqn][id]
			if rec != nil && !rec.Deleted {
				out = append(out, flatten(rec))
			}
		}
		c.JSON(http.StatusOK, out)
	}
}

func RestoreHandler(storage *runtime.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")
		id := c.Param("id")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}

		storage.Mu.Lock()
		defer storage.Mu.Unlock()

		rec := storage.Data[fqn][id]
		if rec == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Record not found"})
			return
		}
		if !rec.Deleted {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Record is not deleted"})
			return
		}
		rec.Deleted = false
		rec.Version++
		rec.UpdatedAt = time.Now().UTC()
		c.JSON(http.StatusOK, flatten(rec))
	}
}

func CountHandler(storage *runtime.Storage) gin.HandlerFunc {
	return func(c *gin.Context) {
		mod := c.Param("module")
		ent := c.Param("entity")

		fqn, ok := storage.NormalizeEntityName(mod, ent)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
			return
		}

		storage.Mu.RLock()
		recMap := storage.Data[fqn]
		count := 0
		for _, r := range recMap {
			if !r.Deleted {
				count++
			}
		}
		storage.Mu.RUnlock()

		c.JSON(http.StatusOK, gin.H{"count": count})
	}
}

// ==== HELPER FUNCTIONS ====

func flatten(rec *runtime.Record) map[string]interface{} {
	out := map[string]interface{}{
		"id":         rec.ID,
		"version":    rec.Version,
		"created_at": rec.CreatedAt.Format(time.RFC3339),
		"updated_at": rec.UpdatedAt.Format(time.RFC3339),
	}
	for k, v := range rec.Data {
		// мета поля пользователя не даём перетирать служебные, если вдруг совпадут
		if _, clash := out[k]; clash {
			out["data."+k] = v
			continue
		}
		out[k] = v
	}
	return out
}

func validationFerr(code, field, msg string) validation.FieldError {
	return validation.FieldError{Code: code, Field: field, Message: msg}
}

func statusForErrors(errs []validation.FieldError) int {
	for _, e := range errs {
		if e.Code == validation.ErrVersionConflict {
			return 409
		}
	}
	return 400
}

func readExpectedVersion(c *gin.Context, obj map[string]any) (int64, bool) {
	// 1) If-Match header
	if inm := c.GetHeader("If-Match"); inm != "" {
		inm = strings.Trim(inm, `"`)
		if v, err := strconv.ParseInt(inm, 10, 64); err == nil {
			return v, true
		}
	}
	// 2) body.version
	if v, ok := obj["version"].(float64); ok {
		return int64(v), true
	}
	return 0, false
}

func checkReadonlyAndSystem(schema *schema.Entity, obj map[string]interface{}, isCreate bool) []validation.FieldError {
	var errs []validation.FieldError
	systemFields := map[string]bool{
		"id":         true,
		"version":    true,
		"created_at": true,
		"updated_at": true,
	}
	for f := range obj {
		if systemFields[f] {
			errs = append(errs, validationFerr(validation.ErrReadOnly, f, "System field cannot be set"))
		}
	}
	for _, f := range schema.Fields {
		if f.Options != nil && strings.EqualFold(f.Options["readonly"], "true") {
			if _, ok := obj[f.Name]; ok && !isCreate {
				errs = append(errs, validationFerr(validation.ErrReadOnly, f.Name, "Field is read-only"))
			}
		}
	}
	return errs
}

func applyDefaults(schema *schema.Entity, obj map[string]interface{}) {
	for _, f := range schema.Fields {
		if f.Options == nil {
			continue
		}
		if _, exists := obj[f.Name]; exists {
			continue
		}
		if def, ok := f.Options["default"]; ok {
			obj[f.Name] = def
		}
	}
}

// filterWithOps applies filters with operators like _eq, _ne, _gt, etc.
func filterWithOps(records []*runtime.Record, schema *schema.Entity, q url.Values) []*runtime.Record {
	if len(q) == 0 {
		return records
	}
	result := records
	for key, vals := range q {
		var op string
		realKey := key
		if idx := strings.Index(key, "_"); idx > 0 {
			op = key[idx:]
			realKey = key[:idx]
		}
		if op == "" {
			op = "_eq"
		}
		fieldIdx := -1
		for i, f := range schema.Fields {
			if f.Name == realKey {
				fieldIdx = i
				break
			}
		}
		if fieldIdx < 0 {
			continue
		}
		f := schema.Fields[fieldIdx]
		var filtered []*runtime.Record
		for _, rec := range result {
			recVal := rec.Data[realKey]
			for _, v := range vals {
				match := false
				switch op {
				case "_eq":
					match = fmt.Sprintf("%v", recVal) == v
				case "_ne":
					match = fmt.Sprintf("%v", recVal) != v
				case "_gt":
					match = runtime.ToString(recVal) > v
				case "_gte":
					match = runtime.ToString(recVal) >= v
				case "_lt":
					match = runtime.ToString(recVal) < v
				case "_lte":
					match = runtime.ToString(recVal) <= v
				case "_like":
					match = strings.Contains(strings.ToLower(runtime.ToString(recVal)), strings.ToLower(v))
				case "_in":
					parts := strings.Split(v, ",")
					for _, p := range parts {
						if strings.TrimSpace(runtime.ToString(recVal)) == strings.TrimSpace(p) {
							match = true
							break
						}
					}
				}
				if f.Type == "array" {
					if arr, ok := recVal.([]interface{}); ok {
						for _, it := range arr {
							if strings.EqualFold(runtime.ToString(it), v) {
								match = true
								break
							}
						}
					}
				}
				if match {
					filtered = append(filtered, rec)
					break
				}
			}
		}
		result = filtered
	}
	return result
}

// ==== CHILD DISCOVERY AND BATCH OPERATIONS ====

type childRelation struct {
	ChildFQN string
	FK       string
}

func discoverChildren(storage *runtime.Storage, schemas map[string]*schema.Entity, parentFQN string, expandAll bool, expandSet map[string]bool) []childRelation {
	var result []childRelation
	_, ok := schemas[parentFQN]
	if !ok {
		return result
	}
	for fqn, schema := range schemas {
		for _, f := range schema.Fields {
			if f.Type != "ref" || f.RefTarget == "" {
				continue
			}
			refMod := schema.Module
			refEnt := f.RefTarget
			if strings.Contains(refEnt, ".") {
				parts := strings.SplitN(refEnt, ".", 2)
				refMod, refEnt = parts[0], parts[1]
			}
			refFQN, ok := storage.NormalizeEntityName(refMod, refEnt)
			if !ok || refFQN != parentFQN {
				continue
			}
			if !expandAll && !expandSet[fqn] {
				continue
			}
			result = append(result, childRelation{ChildFQN: fqn, FK: f.Name})
		}
	}
	return result
}

func uniqStrings(ss []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func batchByFK(storage *runtime.Storage, childFQN, fk string, parentIDs []string) []map[string]any {
	if len(parentIDs) == 0 {
		return nil
	}
	storage.Mu.RLock()
	defer storage.Mu.RUnlock()

	recs := storage.Data[childFQN]
	if recs == nil {
		return nil
	}
	idSet := map[string]bool{}
	for _, pid := range parentIDs {
		idSet[pid] = true
	}
	var out []map[string]any
	for _, rec := range recs {
		if rec.Deleted {
			continue
		}
		if fkVal, ok := rec.Data[fk]; ok {
			if id, ok := fkVal.(string); ok && idSet[id] {
				out = append(out, flatten(rec))
			}
		}
	}
	return out
}

// splitFQN("module.entity") -> ("module","entity")
func splitFQN(fqn string) (string, string) {
	i := strings.IndexByte(fqn, '.')
	if i <= 0 || i >= len(fqn)-1 {
		return "", fqn
	}
	return fqn[:i], fqn[i+1:]
}
