package engine

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
)

// Validation of incoming values against the entity declaration. Values arrive
// as decoded JSON (string / float64 / bool). Unknown fields are rejected:
// the grammar is closed, so is the data.

func (e *Engine) validateValues(decl *dsl.EntityDecl, values map[string]any, partial bool, actorID string) *Err {
	fields := map[string]*dsl.FieldDecl{}
	for _, f := range decl.Fields {
		fields[f.Name] = f
	}

	for name, v := range values {
		f, ok := fields[name]
		if !ok {
			return invalid(name, "unknown field "+decl.Name+"."+name,
				"declared fields: "+fieldNames(decl))
		}
		if f.Computed != "" {
			return invalid(name, name+" is computed and read-only",
				"remove the field from the request; the runtime computes it")
		}
		if v == nil {
			continue
		}
		if err := checkType(f, v); err != nil {
			return err
		}
	}

	if !partial {
		// defaults, then required
		for _, f := range decl.Fields {
			if _, present := values[f.Name]; !present && f.Default != "" && f.Computed == "" {
				values[f.Name] = evalLiteral(f.Default, evalCtx{actorID: actorID})
			}
		}
		for _, f := range decl.Fields {
			if f.Required {
				if v, present := values[f.Name]; !present || v == nil || v == "" {
					return invalid(f.Name, decl.Name+"."+f.Name+" is required",
						"provide a non-empty value for "+f.Name)
				}
			}
		}
	}
	return nil
}

func checkType(f *dsl.FieldDecl, v any) *Err {
	switch f.Type.Kind {
	case dsl.TyScalar:
		return checkScalar(f, v)
	case dsl.TyEnum:
		s, ok := v.(string)
		if !ok || !containsStr(f.Type.EnumValues, s) {
			return invalid(f.Name, fmt.Sprintf("%v is not a valid value for %s", v, f.Name),
				"allowed values: "+strings.Join(f.Type.EnumValues, ", "))
		}
	case dsl.TyRef:
		if _, ok := v.(string); !ok {
			return invalid(f.Name, f.Name+" must be a record id (string)", "pass the id of a "+f.Type.RefTarget)
		}
	case dsl.TyArrayRef:
		xs, ok := v.([]any)
		if !ok {
			return invalid(f.Name, f.Name+" must be an array of record ids", "pass [\"id1\", \"id2\"]")
		}
		for _, x := range xs {
			if _, ok := x.(string); !ok {
				return invalid(f.Name, f.Name+" must contain string ids", "pass [\"id1\", \"id2\"]")
			}
		}
	case dsl.TyTags:
		xs, ok := v.([]any)
		if !ok {
			return invalid(f.Name, f.Name+" must be an array of labels", "pass [\"urgent\", \"backend\"]")
		}
		for _, x := range xs {
			if _, ok := x.(string); !ok {
				return invalid(f.Name, f.Name+" labels must be strings", "pass [\"urgent\", \"backend\"]")
			}
		}
	case dsl.TyMultiEnum:
		xs, ok := v.([]any)
		if !ok {
			return invalid(f.Name, f.Name+" must be an array of values", "pass a subset of the declared options")
		}
		for _, x := range xs {
			s, ok := x.(string)
			if !ok || !containsStr(f.Type.EnumValues, s) {
				return invalid(f.Name, fmt.Sprintf("%v is not a valid option for %s", x, f.Name),
					"allowed: "+strings.Join(f.Type.EnumValues, ", "))
			}
		}
	}
	return nil
}

func checkScalar(f *dsl.FieldDecl, v any) *Err {
	name, want := f.Name, f.Type.Scalar
	bad := func() *Err {
		return invalid(name, fmt.Sprintf("%s expects %s, got %T", name, want, v),
			"pass a JSON "+jsonKind(want))
	}
	switch want {
	case "string", "text":
		if _, ok := v.(string); !ok {
			return bad()
		}
	case "file":
		// a file field carries a FileRef object {hash, name, size}
		m, ok := v.(map[string]any)
		if !ok {
			return invalid(name, name+" must be an uploaded file reference",
				"upload via POST /api/files, then put the returned ref in this field")
		}
		if h, _ := m["hash"].(string); h == "" {
			return invalid(name, name+" file reference has no hash", "re-upload the file")
		}
	case "bool":
		if _, ok := v.(bool); !ok {
			return bad()
		}
	case "int":
		n, ok := toFloat(v)
		if !ok || n != float64(int64(n)) {
			return bad()
		}
	case "float", "money":
		if _, ok := toFloat(v); !ok {
			return bad()
		}
	case "date":
		s, ok := v.(string)
		if !ok {
			return bad()
		}
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return invalid(name, s+" is not a date", "use the YYYY-MM-DD format")
		}
	case "datetime":
		s, ok := v.(string)
		if !ok {
			return bad()
		}
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			return invalid(name, s+" is not a datetime", "use RFC3339, e.g. 2026-06-12T09:00:00Z")
		}
	case "email":
		s, ok := v.(string)
		if !ok || !reEmail.MatchString(s) {
			return invalid(name, fmt.Sprintf("%v is not an email", v), "use name@example.com")
		}
	case "url":
		s, ok := v.(string)
		if !ok || !(strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")) {
			return invalid(name, fmt.Sprintf("%v is not a URL", v), "use http(s)://…")
		}
	case "phone":
		s, ok := v.(string)
		if !ok || !rePhone.MatchString(s) {
			return invalid(name, fmt.Sprintf("%v is not a phone number", v), "digits, optional leading +")
		}
	case "color":
		s, ok := v.(string)
		if !ok || !reColor.MatchString(s) {
			return invalid(name, fmt.Sprintf("%v is not a color", v), "use #RRGGBB")
		}
	case "duration":
		s, ok := v.(string)
		if !ok || s == "" || !reDuration.MatchString(s) {
			return invalid(name, fmt.Sprintf("%v is not a duration", v), "use forms like 2d, 4h, 90m, 1d6h")
		}
	case "percent":
		n, ok := toFloat(v)
		if !ok || n < 0 || n > 100 {
			return invalid(name, fmt.Sprintf("%v is not a percent", v), "a number between 0 and 100")
		}
	case "decimal":
		if _, ok := toFloat(v); !ok {
			return bad()
		}
	case "json":
		// free-form object/array; any JSON value is accepted
	}
	return nil
}

var (
	reEmail    = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	rePhone    = regexp.MustCompile(`^\+?[0-9][0-9\s\-()]{4,}$`)
	reColor    = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	reDuration = regexp.MustCompile(`^(\d+d)?(\d+h)?(\d+m)?$`)
)

// checkRefsExist verifies referenced records exist. core.* refs are accepted
// as opaque ids in v0 (the core pack lands with the identity UI, week 7).
func (e *Engine) checkRefsExist(decl *dsl.EntityDecl, values map[string]any) *Err {
	for _, f := range decl.Fields {
		v, ok := values[f.Name]
		if !ok || v == nil {
			continue
		}
		if f.Type.Kind == dsl.TyRef && !strings.HasPrefix(f.Type.RefTarget, "core.") {
			id, _ := v.(string)
			if _, exists := e.records[f.Type.RefTarget][id]; !exists {
				return invalid(f.Name, fmt.Sprintf("%s references missing %s %q", f.Name, f.Type.RefTarget, id),
					"create the "+f.Type.RefTarget+" first or pass an existing id")
			}
		}
	}
	return nil
}

// checkUnique enforces field-level `unique` and entity `unique(...)` constraints.
func (e *Engine) checkUnique(decl *dsl.EntityDecl, values map[string]any, selfID string) *Err {
	rows := e.records[decl.Name]
	for _, f := range decl.Fields {
		if !f.Unique {
			continue
		}
		v, ok := values[f.Name]
		if !ok || v == nil {
			continue
		}
		for id, r := range rows {
			if id != selfID && r.Values[f.Name] == v {
				return &Err{Code: CodeConflict, Field: f.Name,
					Message: fmt.Sprintf("%s.%s = %v already exists", decl.Name, f.Name, v),
					FixHint: "use a different value; the field is unique"}
			}
		}
	}
	for _, c := range decl.Constraints {
		for id, r := range rows {
			if id == selfID {
				continue
			}
			same := true
			for _, cf := range c.Fields {
				if r.Values[cf] != values[cf] {
					same = false
					break
				}
			}
			if same {
				return &Err{Code: CodeConflict,
					Message: fmt.Sprintf("unique(%s) violated", strings.Join(c.Fields, ", ")),
					FixHint: "a record with the same combination already exists"}
			}
		}
	}
	return nil
}

func fieldNames(decl *dsl.EntityDecl) string {
	names := make([]string, len(decl.Fields))
	for i, f := range decl.Fields {
		names[i] = f.Name
	}
	return strings.Join(names, ", ")
}

func containsStr(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func jsonKind(scalar string) string {
	switch scalar {
	case "int", "float", "money":
		return "number"
	case "bool":
		return "boolean"
	default:
		return "string"
	}
}
