// api/schema_lint.go
package api

import (
	"fmt"
	"strings"
)

type SchemaIssue struct {
	Entity  string `json:"entity"` // FQN: module.Entity
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// SchemaLint проверяет базовые противоречия в DSL.
func (s *Storage) SchemaLint() []SchemaIssue {
	var issues []SchemaIssue

	for fqn, e := range s.Schemas {
		for _, f := range e.Fields {
			// валидность on_delete
			if od := strings.TrimSpace(strings.ToLower(f.Options["on_delete"])); od != "" {
				switch od {
				case "restrict", "set_null", "cascade":
				default:
					issues = append(issues, SchemaIssue{
						Entity:  fqn,
						Field:   f.Name,
						Code:    "on_delete_unknown",
						Message: fmt.Sprintf("unknown on_delete policy %q (allowed: restrict|set_null|cascade)", od),
					})
				}
			}

			// required ref + set_null — конфликт
			if strings.EqualFold(f.Type, "ref") {
				req := strings.EqualFold(f.Options["required"], "true")
				od := strings.TrimSpace(strings.ToLower(f.Options["on_delete"]))
				if req && od == "set_null" {
					issues = append(issues, SchemaIssue{
						Entity:  fqn,
						Field:   f.Name,
						Code:    "required_conflicts_on_delete",
						Message: "required ref cannot have on_delete=set_null; use restrict (or make field optional)",
					})
				}
				// пустая цель ссылки
				if strings.TrimSpace(f.RefTarget) == "" {
					issues = append(issues, SchemaIssue{
						Entity:  fqn,
						Field:   f.Name,
						Code:    "ref_target_empty",
						Message: "ref field has empty RefTarget",
					})
				}
			}

			// array[ref] — set_null допустим (мы уже реализовали очистку массива при удалении),
			// конфликтов тут не поднимаем.
		}
	}
	return issues
}
