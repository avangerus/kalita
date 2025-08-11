package pg

import (
	"fmt"
	"sort"
	"strings"

	"kalita/internal/dsl"
)

type OnDeletePolicy string

const (
	OnDeleteRestrict OnDeletePolicy = "RESTRICT"
	OnDeleteSetNull  OnDeletePolicy = "SET NULL"
)

func sqlIdent(s string) string { return `"` + strings.ToLower(s) + `"` }

func mapType(f dsl.Field) (string, error) {
	t := strings.ToLower(f.Type)
	switch t {
	case "string":
		// можно расширить до varchar(n) через опции
		return "text", nil
	case "int":
		return "bigint", nil
	case "float":
		return "double precision", nil
	case "money":
		return "numeric(18,2)", nil
	case "bool":
		return "boolean", nil
	case "date":
		return "date", nil
	case "datetime":
		return "timestamp with time zone", nil
	case "enum":
		// пока как text; можно генерить enum types отдельно
		return "text", nil
	case "ref":
		return "text", nil // id целевой записи
	case "array":
		// массив примитивов — маппим в jsonb, чтобы быстро поехать
		return "jsonb", nil
	default:
		return "", fmt.Errorf("unknown type: %s", f.Type)
	}
}

func onDeletePolicy(f dsl.Field) OnDeletePolicy {
	if f.Options == nil {
		return OnDeleteRestrict
	}
	switch strings.ToLower(strings.TrimSpace(f.Options["on_delete"])) {
	case "set_null":
		return OnDeleteSetNull
	default:
		return OnDeleteRestrict
	}
}

// GenerateDDL возвращает карту FQN -> SQL DDL (CREATE TABLE + индексы + FK)
func GenerateDDL(entities map[string]*dsl.Entity) (map[string]string, error) {
	out := make(map[string]string, len(entities))

	// стабильный порядок генерации
	keys := make([]string, 0, len(entities))
	for k := range entities {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, fqn := range keys {
		e := entities[fqn]

		var cols []string
		// системные
		cols = append(cols, `"id" text primary key`)
		cols = append(cols, `"version" bigint not null`)
		cols = append(cols, `"created_at" timestamp with time zone not null`)
		cols = append(cols, `"updated_at" timestamp with time zone not null`)

		// пользовательские
		for _, f := range e.Fields {
			name := sqlIdent(f.Name)

			// массивы/сложные типы сразу в jsonb (кроме ref single)
			typ, err := mapType(f)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", fqn, f.Name, err)
			}

			null := "null"
			if f.Options != nil {
				if _, ok := f.Options["required"]; ok {
					null = "not null"
				}
			}

			// readonly/default можно потом оформить в DEFAULT/триггеры
			def := ""
			if f.Options != nil {
				if dv, ok := f.Options["default"]; ok && strings.TrimSpace(dv) != "" {
					// простейший случай: константы
					def = " default " + fmt.Sprintf("'%s'", dv)
				}
			}

			cols = append(cols, fmt.Sprintf("%s %s %s%s", name, typ, null, def))
		}

		var ddl strings.Builder
		// схема = модуль
		mod := strings.ToLower(e.Module)
		tbl := strings.ToLower(e.Name)
		fmt.Fprintf(&ddl, "create schema if not exists %s;\n", sqlIdent(mod))
		fmt.Fprintf(&ddl, "create table if not exists %s.%s (\n  %s\n);\n",
			sqlIdent(mod), sqlIdent(tbl), strings.Join(cols, ",\n  "))

		// UNIQUE (полевые)
		for _, f := range e.Fields {
			if f.Options != nil {
				if _, ok := f.Options["unique"]; ok {
					fmt.Fprintf(&ddl, "create unique index if not exists %s_%s_uq on %s.%s(%s) where deleted is null;\n",
						strings.ToLower(e.Name), strings.ToLower(f.Name),
						sqlIdent(mod), sqlIdent(tbl), sqlIdent(f.Name))
				}
			}
		}

		// UNIQUE (составные)
		for _, set := range e.Constraints.Unique {
			if len(set) == 0 {
				continue
			}
			idxName := strings.ToLower(e.Name + "_" + strings.Join(set, "_") + "_uq")
			var parts []string
			for _, p := range set {
				parts = append(parts, sqlIdent(p))
			}
			fmt.Fprintf(&ddl, "create unique index if not exists %s on %s.%s(%s);\n",
				sqlIdent(idxName), sqlIdent(mod), sqlIdent(tbl), strings.Join(parts, ", "))
		}

		// FK (только для single ref)
		for _, f := range e.Fields {
			if strings.EqualFold(f.Type, "ref") && f.RefTarget != "" {
				refMod := e.Module
				refEnt := f.RefTarget
				if strings.Contains(refEnt, ".") {
					parts := strings.SplitN(refEnt, ".", 2)
					refMod, refEnt = parts[0], parts[1]
				}
				pol := onDeletePolicy(f)
				// FK как индекс + check в приложении (или нормальный FK, если хочешь строгий)
				// здесь покажу нормальный FK:
				fmt.Fprintf(&ddl, "alter table %s.%s add constraint %s_%s_fk foreign key (%s) references %s.%s(id) on delete %s;\n",
					sqlIdent(mod), sqlIdent(tbl),
					strings.ToLower(e.Name), strings.ToLower(f.Name),
					sqlIdent(f.Name),
					sqlIdent(strings.ToLower(refMod)), sqlIdent(strings.ToLower(refEnt)),
					pol)
			}
		}

		out[fmt.Sprintf("%s.%s", mod, strings.ToLower(e.Name))] = ddl.String()
	}
	return out, nil
}
