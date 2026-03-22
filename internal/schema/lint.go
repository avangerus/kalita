package schema

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

// Lint checks basic contradictions in the schema definitions.
func Lint(schemas map[string]*Entity) []SchemaIssue {
	var issues []SchemaIssue

	for fqn, e := range schemas {
		fieldByName := make(map[string]Field, len(e.Fields))
		for _, f := range e.Fields {
			fieldByName[f.Name] = f
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

		if e.Workflow != nil {
			if strings.TrimSpace(e.Workflow.StatusField) == "" {
				issues = append(issues, SchemaIssue{
					Entity:  fqn,
					Code:    "workflow_status_field_missing",
					Message: "workflow.status_field must be set",
				})
			} else {
				statusField, ok := fieldByName[e.Workflow.StatusField]
				if !ok {
					issues = append(issues, SchemaIssue{
						Entity:  fqn,
						Field:   e.Workflow.StatusField,
						Code:    "workflow_status_field_unknown",
						Message: "workflow status_field does not exist on entity",
					})
				} else {
					allowed := make(map[string]struct{}, len(statusField.Enum))
					for _, v := range statusField.Enum {
						allowed[v] = struct{}{}
					}
					for name, action := range e.Workflow.Actions {
						if len(action.From) == 0 {
							issues = append(issues, SchemaIssue{
								Entity:  fqn,
								Field:   e.Workflow.StatusField,
								Code:    "workflow_action_from_empty",
								Message: fmt.Sprintf("workflow action %q must declare at least one from state", name),
							})
						}
						if strings.TrimSpace(action.To) == "" {
							issues = append(issues, SchemaIssue{
								Entity:  fqn,
								Field:   e.Workflow.StatusField,
								Code:    "workflow_action_to_empty",
								Message: fmt.Sprintf("workflow action %q must declare target state", name),
							})
						}
						if strings.EqualFold(statusField.Type, "enum") && len(allowed) > 0 {
							for _, from := range action.From {
								if _, ok := allowed[from]; !ok {
									issues = append(issues, SchemaIssue{
										Entity:  fqn,
										Field:   e.Workflow.StatusField,
										Code:    "workflow_from_unknown",
										Message: fmt.Sprintf("workflow action %q references unknown from state %q", name, from),
									})
								}
							}
							if _, ok := allowed[action.To]; !ok {
								issues = append(issues, SchemaIssue{
									Entity:  fqn,
									Field:   e.Workflow.StatusField,
									Code:    "workflow_to_unknown",
									Message: fmt.Sprintf("workflow action %q references unknown target state %q", name, action.To),
								})
							}
						}
					}
				}
			}
		}
	}
	return issues
}
