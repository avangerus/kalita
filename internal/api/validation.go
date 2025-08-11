package api

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kalita/internal/dsl"
)

type FieldError struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Коды ошибок, которыми будем пользоваться
const (
	ErrRequired        = "required"
	ErrTypeMismatch    = "type_mismatch"
	ErrEnumInvalid     = "enum_invalid"
	ErrUniqueViolation = "unique_violation"
	ErrRefNotFound     = "ref_not_found"
	ErrNotFound        = "not_found"
	ErrReadOnly        = "readonly_field"
	ErrVersionConflict = "version_conflict"
)

// ValidateAgainstSchema валидирует и НОРМАЛИЗУЕТ obj под схему.
// ValidateAgainstSchema валидирует и НОРМАЛИЗУЕТ obj под схему.
func ValidateAgainstSchema(
	storage *Storage,
	schema *dsl.Entity,
	obj map[string]interface{},
	idForUniqueExclusion string, // id текущей записи при обновлении (исключаем из unique-поиска)
	entityKey string, // FQN сущности: "<module>.<name>"
) []FieldError {
	var errs []FieldError

	// --- утилиты
	lc := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	isTrue := func(opts map[string]string, key string) bool {
		if opts == nil {
			return false
		}
		v, ok := opts[key]
		if !ok {
			return false
		}
		v = strings.ToLower(strings.TrimSpace(v))
		return v == "" || v == "true" || v == "1" || v == "yes"
	}

	// чтобы не дублировать ошибки unique из поля и из constraints
	reported := make(map[string]bool) // ключи вида "unique:code" или "unique:base,quote,date"

	// быстрый доступ к описанию поля
	fieldByName := make(map[string]dsl.Field, len(schema.Fields))
	for _, f := range schema.Fields {
		fieldByName[f.Name] = f
	}

	// 1) required
	for _, f := range schema.Fields {
		if isTrue(f.Options, "required") {
			_, present := obj[f.Name]
			if !present || obj[f.Name] == nil {
				errs = append(errs, ferr(ErrRequired, f.Name, "Field '"+f.Name+"' is required"))
			}
		}
	}

	// 2) строгая проверка типов и нормализация (коэрсинг)
	for name, val := range obj {
		f, ok := fieldByName[name]
		if !ok {
			// неизвестное поле — игнорируем (можно сделать warning, но не ошибка)
			continue
		}
		// null допускаем для не-required
		if val == nil {
			if isTrue(f.Options, "required") {
				errs = append(errs, ferr(ErrRequired, name, "Field '"+name+"' is required"))
			}
			continue
		}

		// enum и ref проверим ниже своими блоками; здесь коэрсим всё остальное
		ft := strings.ToLower(f.Type)
		if ft == "ref" || (ft == "array" && strings.EqualFold(f.ElemType, "ref")) {
			continue
		}
		// enum как тип — пропустим до блока enum-проверки
		if ft == "enum" || (ft == "array" && strings.EqualFold(f.ElemType, "enum")) {
			// но всё равно дадим шанс коэрсингу на примитивы/массивы, если он это умеет
			norm, err := coerceValue(storage, f, val)
			if err != nil {
				errs = append(errs, ferr(ErrTypeMismatch, name, "Field '"+name+"' "+err.Error()))
				continue
			}
			obj[name] = norm
			continue
		}

		norm, err := coerceValue(storage, f, val)
		if err != nil {
			errs = append(errs, ferr(ErrTypeMismatch, name, "Field '"+name+"' "+err.Error()))
			continue
		}
		obj[name] = norm
	}

	// 3) enum (значение ∈ перечислению или каталогу)
	for _, f := range schema.Fields {
		val, ok := obj[f.Name]
		if !ok || val == nil {
			continue
		}

		// статический enum [...]
		if len(f.Enum) > 0 {
			s := fmt.Sprintf("%v", val)
			found := false
			for _, ev := range f.Enum {
				if s == ev {
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, ferr(ErrEnumInvalid, f.Name, "Invalid value for '"+f.Name+"'"))
				continue
			}
		}

		// catalog=<name> (значение должно быть одним из codes каталога)
		if f.Options != nil && lc(f.Options["catalog"]) != "" {
			cat := lc(f.Options["catalog"])
			dir, ok := storage.Enums[cat]
			if !ok {
				// схема ссылалась на неизвестный каталог — для API даём enum_invalid
				errs = append(errs, ferr(ErrEnumInvalid, f.Name, "Unknown catalog '"+cat+"'"))
				continue
			}
			s := fmt.Sprintf("%v", val)
			found := false
			for _, it := range dir.Items {
				if s == it.Code {
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, ferr(ErrEnumInvalid, f.Name, "Invalid value for '"+f.Name+"'"))
			}
		}
	}

	// 4) ref: проверка существования цели (single и array[ref])
	for _, f := range schema.Fields {
		ft := strings.ToLower(f.Type)
		if ft != "ref" && !(ft == "array" && strings.EqualFold(f.ElemType, "ref")) {
			continue
		}
		val, ok := obj[f.Name]
		if !ok || val == nil {
			continue
		}

		// определить FQN целевой сущности
		ref := f.RefTarget
		if ref == "" {
			continue // нечего проверять
		}
		// ref может быть кратким (без модуля) — дополним текущим
		refMod := schema.Module
		refEnt := ref
		if strings.Contains(ref, ".") {
			parts := strings.SplitN(ref, ".", 2)
			refMod, refEnt = parts[0], parts[1]
		}
		targetFQN, ok := storage.NormalizeEntityName(refMod, refEnt)
		if !ok {
			// схема ссылается на несуществующую сущность — для API аккуратно считаем "not found"
			errs = append(errs, ferr(ErrRefNotFound, f.Name, "Reference target not found"))
			continue
		}

		// проверка существования ID(ов)
		checkID := func(id string) bool {
			storage.mu.RLock()
			m := storage.Data[targetFQN]
			var hit *Record
			if m != nil {
				hit = m[id]
			}
			storage.mu.RUnlock()
			return hit != nil && !hit.Deleted
		}

		if ft == "ref" {
			id := fmt.Sprintf("%v", val)
			if id == "" || !checkID(id) {
				errs = append(errs, ferr(ErrRefNotFound, f.Name, "Referenced id not found"))
			}
		} else {
			// array[ref]
			arr, ok := val.([]any)
			if !ok {
				// может прийти как []string — приведём
				if ss, ok2 := val.([]string); ok2 {
					arr = make([]any, len(ss))
					for i := range ss {
						arr[i] = ss[i]
					}
				} else {
					errs = append(errs, ferr(ErrTypeMismatch, f.Name, "Field '"+f.Name+"' expected array"))
					continue
				}
			}
			for idx, v := range arr {
				id := fmt.Sprintf("%v", v)
				if id == "" || !checkID(id) {
					errs = append(errs, ferr(ErrRefNotFound, f.Name, fmt.Sprintf("Referenced id at index %d not found", idx)))
				}
			}
		}
	}

	// если уже есть ошибки — смысла дальше идти мало, но продолжим, чтобы показать все проблемы разом
	// 5) уникальность поля (single unique)
	records := storage.Data[entityKey]
	if records == nil {
		records = make(map[string]*Record)
	}
	for _, f := range schema.Fields {
		if !isTrue(f.Options, "unique") {
			continue
		}
		v, ok := obj[f.Name]
		if !ok || v == nil {
			continue // пустое значение не участвует в уникальности
		}
		needle := fmt.Sprintf("%v", v)

		conflict := false
		storage.mu.RLock()
		for id, rec := range records {
			if idForUniqueExclusion != "" && id == idForUniqueExclusion {
				continue
			}
			if rec == nil || rec.Deleted {
				continue
			}
			got := fmt.Sprintf("%v", rec.Data[f.Name])
			if got == needle {
				conflict = true
				break
			}
		}
		storage.mu.RUnlock()

		if conflict {
			key := "unique:" + f.Name
			if !reported[key] {
				errs = append(errs, ferr(ErrUniqueViolation, f.Name, "Field '"+f.Name+"' must be unique"))
				reported[key] = true
			}
		}
	}

	// 6) составные уникальности из constraints: unique(a,b,...) (только если есть все поля)
	if len(schema.Constraints.Unique) > 0 {
		// подготовим строковые значения из obj для быстрого сравнения
		valStr := func(m map[string]any, key string) (string, bool) {
			v, ok := m[key]
			if !ok || v == nil {
				return "", false
			}
			return fmt.Sprintf("%v", v), true
		}

		for _, set := range schema.Constraints.Unique {
			// собрать ключ из obj
			keyParts := make([]string, 0, len(set))
			allPresent := true
			for _, fname := range set {
				if s, ok := valStr(obj, fname); ok {
					keyParts = append(keyParts, s)
				} else {
					allPresent = false
					break
				}
			}
			if !allPresent {
				continue // нечего проверять, не все поля есть в текущем объекте
			}
			needle := strings.Join(keyParts, "\x00") // безопасный сепаратор

			// поиск конфликта
			conflict := false
			storage.mu.RLock()
			for id, rec := range records {
				if idForUniqueExclusion != "" && id == idForUniqueExclusion {
					continue
				}
				if rec == nil || rec.Deleted {
					continue
				}
				parts := make([]string, 0, len(set))
				miss := false
				for _, fname := range set {
					s := fmt.Sprintf("%v", rec.Data[fname])
					// допускаем пустую строку как значение; конфликтуем по точному совпадению
					parts = append(parts, s)
				}
				if miss {
					continue
				}
				if strings.Join(parts, "\x00") == needle {
					conflict = true
					break
				}
			}
			storage.mu.RUnlock()

			if conflict {
				combo := strings.Join(set, ",")
				key := "unique:" + combo
				if !reported[key] {
					// в качестве "field" оставим первое из набора
					errs = append(errs, ferr(ErrUniqueViolation, set[0], "Fields ["+combo+"] must be unique together"))
					reported[key] = true
				}
			}
		}
	}

	return errs
}

func violatesUnique(storage *Storage, entity, field string, value interface{}, excludeID string) bool {
	needle := fmt.Sprintf("%v", value)

	storage.mu.RLock()
	defer storage.mu.RUnlock()

	for id, rec := range storage.Data[entity] {
		if rec.Deleted || id == excludeID {
			continue
		}
		if fmt.Sprintf("%v", rec.Data[field]) == needle {
			return true
		}
	}
	return false
}

func violatesCompositeUnique(storage *Storage, entity string, fields []string, values []string, excludeID string) bool {
	storage.mu.RLock()
	defer storage.mu.RUnlock()

	recMap := storage.Data[entity]
	for id, rec := range recMap {
		if rec == nil || rec.Deleted || id == excludeID {
			continue
		}
		match := true
		for i, fname := range fields {
			if fmt.Sprintf("%v", rec.Data[fname]) != values[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

var (
	dateRe     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)                    // YYYY-MM-DD
	datetimeRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`) // RFC3339 (UTC, без миллисекунд)
)

func coerceValue(storage *Storage, f dsl.Field, v interface{}) (interface{}, error) {
	switch f.Type {
	case "string":
		return toStringStrict(v)
	case "int":
		return toIntStrict(v)
	case "float":
		return toFloatStrict(v)
	case "bool":
		return toBoolStrict(v)
	case "date":
		s, err := toStringStrict(v)
		if err != nil {
			return nil, err
		}
		if !dateRe.MatchString(s) {
			return nil, errors.New("must match YYYY-MM-DD")
		}
		// легкая валидация корректности даты
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return nil, errors.New("invalid date")
		}
		return s, nil
	case "datetime":
		s, err := toStringStrict(v)
		if err != nil {
			return nil, err
		}
		// примем RFC3339 (в т.ч. с миллисекундами)
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			return nil, errors.New("must be RFC3339 datetime")
		}
		return s, nil
	case "enum":
		s, err := toStringStrict(v)
		if err != nil {
			return nil, err
		}
		ok := false
		for _, ev := range f.Enum {
			if s == ev {
				ok = true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("value '%s' is not allowed", s)
		}
		return s, nil
	case "ref":
		// ожидаем строковый id
		s, err := toStringStrict(v)
		if err != nil {
			return nil, err
		}
		target, ok := storage.NormalizeEntityName("", f.RefTarget)
		if !ok {
			return nil, fmt.Errorf("unknown target entity '%s'", f.RefTarget)
		}
		if !storage.Exists(target, s) {
			return nil, fmt.Errorf("references non-existent %s '%s'", target, s)
		}
		return s, nil
	case "array":
		arr, ok := v.([]interface{})
		if !ok {
			// некоторые JSON-библиотеки уже дают []any, но если пришёл пустой массив как []string — нормализуем
			if s, isStr := v.(string); isStr {
				// позволим CSV для простоты: "a,b,c"
				parts := strings.Split(s, ",")
				tmp := make([]interface{}, 0, len(parts))
				for _, p := range parts {
					tmp = append(tmp, strings.TrimSpace(p))
				}
				arr = tmp
			} else {
				return nil, errors.New("must be array")
			}
		}
		out := make([]interface{}, 0, len(arr))
		// сконструируем "виртуальное" поле для элемента
		elemField := dsl.Field{
			Type:      f.ElemType,
			Enum:      f.Enum,
			RefTarget: f.RefTarget,
		}
		for i, ev := range arr {
			norm, err := coerceValue(storage, elemField, ev)
			if err != nil {
				return nil, fmt.Errorf("array element %d: %v", i, err)
			}
			out = append(out, norm)
		}
		return out, nil
	default:
		// неизвестный тип — оставим как есть
		return v, nil
	}
}

func toStringStrict(v interface{}) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case float64: // json.Number по умолчанию в Go — float64
		// не будем автоматически форматировать числа как строки — лучше отдать ошибку
		return "", errors.New("must be string")
	case bool:
		return "", errors.New("must be string")
	case nil:
		return "", errors.New("must be string")
	default:
		return "", errors.New("must be string")
	}
}

func toIntStrict(v interface{}) (int64, error) {
	switch t := v.(type) {
	case float64:
		// JSON числа приходят как float64 — проверяем целостность
		if t != float64(int64(t)) {
			return 0, errors.New("must be integer")
		}
		return int64(t), nil
	case string:
		n, err := strconv.ParseInt(t, 10, 64)
		if err != nil {
			return 0, errors.New("must be integer")
		}
		return n, nil
	default:
		return 0, errors.New("must be integer")
	}
}

func toFloatStrict(v interface{}) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case string:
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return 0, errors.New("must be float")
		}
		return f, nil
	default:
		return 0, errors.New("must be float")
	}
}

func toBoolStrict(v interface{}) (bool, error) {
	switch t := v.(type) {
	case bool:
		return t, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true", "1", "yes", "y", "on":
			return true, nil
		case "false", "0", "no", "n", "off":
			return false, nil
		default:
			return false, errors.New("must be boolean")
		}
	default:
		return false, errors.New("must be boolean")
	}
}

// normalizeEntityName — используем тот же маппинг, что и в storage.NormalizeEntityName,
// но без зависимостей на gin. Если уже есть метод у Storage — можно убрать эту функцию.
func normalizeEntityName(s *Storage, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if i := strings.IndexByte(raw, '.'); i > 0 {
		return s.NormalizeEntityName(raw[:i], raw[i+1:])
	}
	return s.NormalizeEntityName("", raw) // без модуля — только если имя уникально
}

func ferr(code, field, msg string) FieldError {
	return FieldError{Code: code, Field: field, Message: msg}
}

// refExists — проверяет существование записи с id в целевой сущности (FQN), игнорируя soft-deleted.
func refExists(storage *Storage, targetFQN, id string) bool {
	storage.mu.RLock()
	defer storage.mu.RUnlock()
	recMap := storage.Data[targetFQN]
	if recMap == nil {
		return false
	}
	rec, ok := recMap[id]
	if !ok || rec == nil || rec.Deleted {
		return false
	}
	return true
}

// resolveRefTarget — аккуратно извлекает целевую сущность из поля (поддержка как явных полей, так и options["ref"]).
func resolveRefTarget(f dsl.Field) (kind string, target string) {
	// single ref
	if strings.EqualFold(f.Type, "ref") && f.RefTarget != "" {
		return "ref", f.RefTarget
	}
	// array of refs
	if strings.EqualFold(f.Type, "array") && strings.EqualFold(f.ElemType, "ref") && f.RefTarget != "" {
		return "array_ref", f.RefTarget
	}
	// fallback: иногда парсер кладёт в Options
	if f.Options != nil {
		if tgt := f.Options["ref"]; tgt != "" {
			// не знаем single/array — пусть будет single по умолчанию
			return "ref", tgt
		}
	}
	return "", ""
}

// 1) в applyDefaults — пропускаем ref и array[ref]
func applyDefaults(schema *dsl.Entity, obj map[string]any) {
	for _, f := range schema.Fields {
		if f.Options == nil {
			continue
		}
		// не трогаем ссылки
		if f.Type == "ref" || (f.Type == "array" && strings.EqualFold(f.ElemType, "ref")) {
			continue
		}

		def, ok := f.Options["default"]
		if !ok || obj[f.Name] != nil {
			continue
		}
		v, err := coerceValue(nil, f, def)
		if err == nil {
			obj[f.Name] = v
		}
	}
}

// проверка системных/readonly полей.
// Возвращает []FieldError, если клиент пытался задать/менять защищённые поля.
// Особый случай: "version" разрешаем передавать как hint для optimistic lock,
// но СНИМАЕМ его из payload, чтобы не перезаписать в хранилище.
func checkReadonlyAndSystem(schema *dsl.Entity, obj map[string]any, isCreate bool) (errs []FieldError) {
	// системные поля
	sys := []string{"id", "created_at", "updated_at", "version"}
	for _, k := range sys {
		if _, ok := obj[k]; ok {
			if k == "version" {
				// Разрешаем присутствие для If-Match-подобной логики, но не даём записать в Data
				delete(obj, k)
				continue
			}
			errs = append(errs, ferr(ErrReadOnly, k, "Field '"+k+"' is read-only"))
		}
	}
	// readonly из схемы
	for _, f := range schema.Fields {
		if f.Options != nil && strings.EqualFold(f.Options["readonly"], "true") {
			if _, ok := obj[f.Name]; ok {
				errs = append(errs, ferr(ErrReadOnly, f.Name, "Field '"+f.Name+"' is read-only"))
			}
		}
	}
	return
}

// parent_id: ref[Self] — проверка самоссылки и циклов
func checkSelfParentAndCycles(storage *Storage, entityKey, currentID string, parent any) []FieldError {
	pid, _ := parent.(string)
	if pid == "" {
		return nil
	}
	// self-parent запрещён на update/patch (на create currentID == "")
	if currentID != "" && pid == currentID {
		return []FieldError{ferr("self_parent", "parent_id", "parent_id cannot reference the record itself")}
	}

	seen := map[string]bool{}
	cur := pid
	for steps := 0; steps < 2048 && cur != ""; steps++ {
		if seen[cur] {
			return []FieldError{ferr("cycle_detected", "parent_id", "parent chain contains a cycle")}
		}
		seen[cur] = true

		storage.mu.RLock()
		rec := storage.Data[entityKey][cur]
		storage.mu.RUnlock()
		if rec == nil || rec.Deleted {
			// отсутствие родителя отдельно ловится ref-валидацией
			break
		}
		if currentID != "" && rec.ID == currentID {
			return []FieldError{ferr("cycle_detected", "parent_id", "parent chain points back to the record (cycle)")}
		}
		next, _ := rec.Data["parent_id"].(string)
		cur = next
	}
	return nil
}
