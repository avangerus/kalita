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

var reserved = map[string]struct{}{
	"user": {}, "select": {}, "table": {}, "insert": {}, "update": {}, "delete": {},
	"where": {}, "join": {}, "group": {}, "order": {}, "limit": {}, "offset": {},
	"primary": {}, "foreign": {}, "key": {}, "constraint": {}, "default": {},
	"from": {}, "into": {}, "values": {}, "unique": {}, "index": {}, "create": {},
	"drop": {}, "alter": {}, "schema": {}, "grant": {}, "revoke": {},
}

func isReserved(s string) bool { _, ok := reserved[strings.ToLower(s)]; return ok }

// элементарная плюрализация (достаточно для users, projects, ...)
// при желании затем подключим инфлектор
func plural(s string) string {
	s = strings.ToLower(s)
	if strings.HasSuffix(s, "s") {
		return s
	}
	return s + "s"
}

// schema = module (lower), table = plural(entity) с защитой keyword'ов
func safeSchema(module string) string { return strings.ToLower(module) }

func safeTable(entity string) string {
	t := plural(entity)
	t = strings.ToLower(t)
	if isReserved(t) {
		// помечаем «опасное» имя префиксом
		t = "e_" + t
	}
	return t
}

func fqn(mod, tbl string) string {
	return fmt.Sprintf("%s.%s", strings.ToLower(mod), strings.ToLower(tbl))
}

// sqlIdent уже есть у тебя:
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
	out := make(map[string]string, len(entities)+2)

	// стабильный порядок сущностей
	keys := make([]string, 0, len(entities))
	for k := range entities {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// --- Phase A: schemas + tables + unique ---
	var phaseASb strings.Builder
	seenSchemas := map[string]struct{}{}

	// соберём FK для второй фазы
	type fkStmt struct {
		mod, tbl, idxName, col, refMod, refTbl string
		onDelete                               OnDeletePolicy
	}
	var fks []fkStmt

	for _, fqnKey := range keys {
		e := entities[fqnKey]

		// безопасные имена
		mod := safeSchema(e.Module)
		tbl := safeTable(e.Name)

		// schema
		if _, ok := seenSchemas[mod]; !ok {
			fmt.Fprintf(&phaseASb, "create schema if not exists %s;\n", sqlIdent(mod))
			seenSchemas[mod] = struct{}{}
		}

		// системные колонки
		var cols []string
		cols = append(cols, `"id" text primary key`)
		cols = append(cols, `"version" bigint not null`)
		cols = append(cols, `"created_at" timestamp with time zone not null`)
		cols = append(cols, `"updated_at" timestamp with time zone not null`)

		seen := map[string]struct{}{"id": {}, "version": {}, "created_at": {}, "updated_at": {}}

		// пользовательские поля
		for _, f := range e.Fields {
			nameLower := strings.ToLower(f.Name)
			if _, exists := seen[nameLower]; exists {
				return nil, fmt.Errorf("%s: field %q duplicates a system or duplicate column", fqnKey, f.Name)
			}
			seen[nameLower] = struct{}{}

			name := sqlIdent(f.Name)
			typ, err := mapType(f)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", fqnKey, f.Name, err)
			}

			null := "null"
			if f.Options != nil {
				if _, ok := f.Options["required"]; ok {
					null = "not null"
				}
			}
			def := ""
			if f.Options != nil {
				if dv, ok := f.Options["default"]; ok && strings.TrimSpace(dv) != "" {
					def = " default " + fmt.Sprintf("'%s'", dv)
				}
			}
			cols = append(cols, fmt.Sprintf("%s %s %s%s", name, typ, null, def))
		}

		// CREATE TABLE
		fmt.Fprintf(&phaseASb, "create table if not exists %s.%s (\n  %s\n);\n",
			sqlIdent(mod), sqlIdent(tbl), strings.Join(cols, ",\n  "))

		// UNIQUE по полям (убрал predicate на deleted, т.к. колонки нет)
		for _, f := range e.Fields {
			if f.Options != nil {
				if _, ok := f.Options["unique"]; ok {
					fmt.Fprintf(&phaseASb, "create unique index if not exists %s_%s_uq on %s.%s(%s);\n",
						strings.ToLower(e.Name), strings.ToLower(f.Name),
						sqlIdent(mod), sqlIdent(tbl), sqlIdent(f.Name))
				}
			}
		}

		// UNIQUE составные
		for _, set := range e.Constraints.Unique {
			if len(set) == 0 {
				continue
			}
			idxName := strings.ToLower(e.Name + "_" + strings.Join(set, "_") + "_uq")
			var parts []string
			for _, p := range set {
				parts = append(parts, sqlIdent(p))
			}
			fmt.Fprintf(&phaseASb, "create unique index if not exists %s on %s.%s(%s);\n",
				sqlIdent(idxName), sqlIdent(mod), sqlIdent(tbl), strings.Join(parts, ", "))
		}

		// FK собираем, но не исполняем пока
		for _, f := range e.Fields {
			if strings.EqualFold(f.Type, "ref") && f.RefTarget != "" {
				refMod := e.Module
				refEnt := f.RefTarget
				if strings.Contains(refEnt, ".") {
					parts := strings.SplitN(refEnt, ".", 2)
					refMod, refEnt = parts[0], parts[1]
				}
				fks = append(fks, fkStmt{
					mod:      mod,
					tbl:      tbl,
					idxName:  strings.ToLower(e.Name + "_" + f.Name + "_fk"),
					col:      f.Name,
					refMod:   safeSchema(refMod),
					refTbl:   safeTable(refEnt),
					onDelete: onDeletePolicy(f),
				})
			}
		}

		// ключ для сортировки ApplyDDL: сначала схемы/таблицы
		out["100_"+fqn(mod, tbl)] = "" // placeholder, чтобы ключ попал в сортировку
	}

	// общий SQL для схем и таблиц
	out["000_schemas_and_tables"] = phaseASb.String()

	// --- Phase B: foreign keys (после создания всех таблиц) ---
	var phaseBSb strings.Builder
	for _, fk := range fks {
		fmt.Fprintf(&phaseBSb,
			"alter table %s.%s add constraint %s foreign key (%s) references %s.%s(id) on delete %s;\n",
			sqlIdent(fk.mod), sqlIdent(fk.tbl),
			strings.ToLower(fk.idxName),
			sqlIdent(fk.col),
			sqlIdent(fk.refMod), sqlIdent(fk.refTbl),
			fk.onDelete,
		)
	}
	if phaseBSb.Len() > 0 {
		out["200_foreign_keys"] = phaseBSb.String()
	}

	return out, nil
}
