package api

import (
	"fmt"
	"strings"
	"time"
)

func flatten(rec *Record) map[string]interface{} {
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

// простая проверка уникальности по полю (in-memory)
func (s *Storage) uniqueOK(entity, field string, value interface{}, exceptID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byID := s.Data[entity]
	for id, rec := range byID {
		if rec.Deleted || id == exceptID {
			continue
		}
		if v, ok := rec.Data[field]; ok {
			// сравнение по строковому представлению как MVP (потом можно сузить)
			if stringify(v) == stringify(value) {
				return false
			}
		}
	}
	return true
}

func stringify(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return strings.TrimSpace(fmtAny(v))
	}
}

func fmtAny(v interface{}) string {
	return fmt.Sprintf("%v", v)
}
