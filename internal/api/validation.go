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
func ValidateAgainstSchema(
	storage *Storage,
	schema *dsl.Entity,
	obj map[string]interface{},
	idForUniqueExclusion string, // id текущей записи при обновлении (исключаем из unique-поиска)
	entityKey string, // FQN сущности: "<module>.<name>"
) []FieldError {
	var errs []FieldError

	// 1) required
	for _, f := range schema.Fields {
		if f.Options != nil && strings.EqualFold(f.Options["required"], "true") {
			if _, ok := obj[f.Name]; !ok {
				errs = append(errs, ferr(ErrRequired, f.Name, "Field '"+f.Name+"' is required"))
			}
		}
	}

	// 1.5) строгая проверка типов и нормализация значений для примитивов и массивов
	// (enum и ссылки проверим ниже в существующих блоках)
	fieldByName := make(map[string]dsl.Field, len(schema.Fields))
	for _, f := range schema.Fields {
		fieldByName[f.Name] = f
	}

	for name, val := range obj {
		f, ok := fieldByName[name]
		if !ok {
			// неизвестные поля можно игнорировать или ругаться — сейчас игнор
			continue
		}
		if strings.EqualFold(f.Type, "bool") {
			if _, ok := val.(bool); !ok {
				errs = append(errs, ferr(ErrTypeMismatch, name, "Field '"+name+"' expected bool"))
				continue
			}
			// уже корректный bool — можно оставить как есть без коэрсинга
			obj[name] = val
			continue
		}
		// пропускаем enum: у тебя ниже уже есть отдельная проверка блока // 2) enum
		if strings.EqualFold(f.Type, "enum") {
			continue
		}
		// пропускаем ссылки: их ты проверяешь ниже в блоке // 4) ref (single и array)
		if strings.EqualFold(f.Type, "ref") ||
			(strings.EqualFold(f.Type, "array") && strings.EqualFold(f.ElemType, "ref")) {
			continue
		}

		// всё остальное (string/int/float/bool/date/datetime и array[...] примитивов/enum) — через coerceValue
		norm, err := coerceValue(storage, f, val)
		if err != nil {
			errs = append(errs, ferr(ErrTypeMismatch, name, "Field '"+name+"' "+err.Error()))
			continue
		}
		obj[name] = norm
	}

	// 2) enum (строгое соответствие одному из значений)
	for _, f := range schema.Fields {
		if len(f.Enum) == 0 {
			continue
		}
		if v, ok := obj[f.Name]; ok {
			s := fmt.Sprintf("%v", v)
			found := false
			for _, ev := range f.Enum {
				if s == ev {
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, ferr(ErrEnumInvalid, f.Name, "Invalid value for '"+f.Name+"'"))
			}
		}
	}

	// 3) unique (конфликт целостности → 409)
	for _, f := range schema.Fields {
		if f.Options != nil && strings.EqualFold(f.Options["unique"], "true") {
			if v, ok := obj[f.Name]; ok {
				if violatesUnique(storage, entityKey, f.Name, v, idForUniqueExclusion) {
					errs = append(errs, ferr(ErrUniqueViolation, f.Name, "Field '"+f.Name+"' must be unique"))
				}
			}
		}
	}

	// 3.1) composite unique (constraints.unique)
	if len(schema.Constraints.Unique) > 0 {
		for _, uniqueSet := range schema.Constraints.Unique {
			if len(uniqueSet) == 0 {
				continue
			}
			// собрать значения ключа из obj
			key := make([]string, len(uniqueSet))
			allPresent := true
			for i, fname := range uniqueSet {
				v, ok := obj[fname]
				if !ok {
					allPresent = false
					break
				}
				key[i] = fmt.Sprintf("%v", v)
			}
			if !allPresent {
				continue
			}
			if violatesCompositeUnique(storage, entityKey, uniqueSet, key, idForUniqueExclusion) {
				errs = append(errs, ferr(ErrUniqueViolation, uniqueSet[0],
					fmt.Sprintf("Fields %v must be unique together", uniqueSet)))
			}
		}
	}

	// 4) ref — проверка существования ссылок (single и array)
	for _, f := range schema.Fields {
		kind, target := resolveRefTarget(f)
		if kind == "" || target == "" {
			continue
		}

		// ⬇️ ДОБАВЬ ЭТО: если target без модуля — префикс текущего модуля схемы
		targetFQN := target
		if !strings.Contains(target, ".") {
			targetFQN = schema.Module + "." + target
		}

		v, ok := obj[f.Name]
		if !ok {
			continue // поле не передано — пропускаем
		}

		switch kind {
		case "ref":
			s, _ := v.(string)
			if s == "" || !refExists(storage, targetFQN, s) { // ⬅️ используем targetFQN
				errs = append(errs, ferr(ErrRefNotFound, f.Name, "Referenced '"+targetFQN+"' not found"))
			}
		case "array_ref":
			switch arr := v.(type) {
			case []any:
				for _, it := range arr {
					s, _ := it.(string)
					if s == "" || !refExists(storage, targetFQN, s) {
						errs = append(errs, ferr(ErrRefNotFound, f.Name, "Referenced '"+targetFQN+"' not found"))
						break
					}
				}
			case []string:
				for _, s := range arr {
					if s == "" || !refExists(storage, targetFQN, s) {
						errs = append(errs, ferr(ErrRefNotFound, f.Name, "Referenced '"+targetFQN+"' not found"))
						break
					}
				}
			default:
				errs = append(errs, ferr(ErrTypeMismatch, f.Name, "Field '"+f.Name+"' must be an array of ids"))
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
		target, ok := normalizeEntityName(storage, f.RefTarget)
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

// применяет default= для отсутствующих полей (на Create и, опционально, на PUT)
func applyDefaults(schema *dsl.Entity, obj map[string]any) {
	for _, f := range schema.Fields {
		if f.Options == nil {
			continue
		}
		def, ok := f.Options["default"]
		if !ok {
			continue
		}
		if _, exists := obj[f.Name]; exists {
			continue
		}
		// пробуем привести дефолт к типу поля через имеющийся coercer
		// def приходит строкой — coerceValue сам ругнется, если не влезет
		v, err := coerceValue(nil, f, def) // storage не нужен для примитивов
		if err == nil {
			obj[f.Name] = v
		} else {
			// если дефолт некорректен — просто не подставляем (не валим запрос)
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
