package dsl

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	entityRe           = regexp.MustCompile(`^entity\s+(\w+):`)
	fieldRe            = regexp.MustCompile(`^\s*([\w_]+):\s*([^\s#]+)(.*)$`)
	enumRe             = regexp.MustCompile(`^enum\[(.*)\]$`)
	refRe              = regexp.MustCompile(`^ref\[([A-Za-z0-9_.]+)\]$`)
	arrayRe            = regexp.MustCompile(`^array\[(.+)\]$`)
	moduleRe           = regexp.MustCompile(`^\s*module\s+([A-Za-z0-9_.-]+)\s*$`)
	reConstraintsStart = regexp.MustCompile(`^\s*constraints\s*:\s*$`)
	reUniqueLine       = regexp.MustCompile(`^\s*unique\s*\(\s*([^)]+)\s*\)\s*$`)
)

// // parse: options tokenizer — делит "k=v k2='v 2' pattern=^[A-Z0-9 _-]+$" на токены, не рвёт по пробелам внутри кавычек/скобок
func splitOptionTokens(s string) []string {
	var out []string
	var buf []rune
	inSingle, inDouble := false, false
	bracketDepth := 0 // внутри [ ... ] у регэкспа

	flush := func() {
		if len(buf) > 0 {
			out = append(out, string(buf))
			buf = buf[:0]
		}
	}

	for _, r := range s {
		switch r {
		case '\'':
			if !inDouble && bracketDepth == 0 {
				inSingle = !inSingle
			}
			buf = append(buf, r)
		case '"':
			if !inSingle && bracketDepth == 0 {
				inDouble = !inDouble
			}
			buf = append(buf, r)
		case '[':
			if !inSingle && !inDouble {
				bracketDepth++
			}
			buf = append(buf, r)
		case ']':
			if !inSingle && !inDouble && bracketDepth > 0 {
				bracketDepth--
			}
			buf = append(buf, r)
		default:
			// разделитель — пробел И ТОЛЬКО если мы не в кавычках и не внутри [...]
			if (r == ' ' || r == '\t') && !inSingle && !inDouble && bracketDepth == 0 {
				flush()
				continue
			}
			buf = append(buf, r)
		}
	}
	flush()
	return out
}

func LoadEntities(path string) ([]*Entity, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entities []*Entity
	var current *Entity
	currentModule := ""
	inConstraints := false
	// карта имён полей текущей сущности (для проверки дублей)
	var fieldNames map[string]struct{}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		raw := scanner.Text()
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// module ...
		if m := moduleRe.FindStringSubmatch(line); m != nil {
			currentModule = m[1]
			continue
		}

		// entity <Name>:
		if m := entityRe.FindStringSubmatch(line); m != nil {
			// закрыть предыдущую сущность
			if current != nil {
				entities = append(entities, current)
			}
			current = &Entity{Name: m[1], Module: currentModule}
			inConstraints = false
			fieldNames = make(map[string]struct{}, 16)
			continue
		}
		if current == nil {
			// игнорируем всё вне сущности
			continue
		}

		// ----- БЛОК CONSTRAINTS -----
		if reConstraintsStart.MatchString(line) {
			inConstraints = true
			continue
		}
		if inConstraints {
			// unique(...)
			if m := reUniqueLine.FindStringSubmatch(line); m != nil {
				parts := strings.Split(m[1], ",")
				set := make([]string, 0, len(parts))
				for _, p := range parts {
					if s := strings.TrimSpace(p); s != "" {
						set = append(set, s)
					}
				}
				if len(set) > 0 {
					current.Constraints.Unique = append(current.Constraints.Unique, set)
				}
				continue
			}
			// выход из constraints при новой секции
			if strings.HasPrefix(line, "entity ") || strings.HasPrefix(line, "module ") {
				inConstraints = false
				// и дайте обработаться следующими правилами (без continue)
			} else {
				inConstraints = false
				continue
			}
		}
		// ----- КОНЕЦ БЛОКА CONSTRAINTS -----

		// Поле: name: type [options]
		if m := fieldRe.FindStringSubmatch(line); m != nil {
			name := m[1]
			rawType := m[2]
			tail := m[3] // остаток после типа (опции)

			// склейка оборванных типов со скобками
			if strings.HasPrefix(rawType, "enum[") && !strings.Contains(rawType, "]") {
				if idx := strings.Index(tail, "]"); idx >= 0 {
					rawType = rawType + tail[:idx+1]
					tail = tail[idx+1:]
				}
			}
			if strings.HasPrefix(rawType, "array[") && !strings.Contains(rawType, "]") {
				if idx := strings.Index(tail, "]"); idx >= 0 {
					rawType = rawType + tail[:idx+1]
					tail = tail[idx+1:]
				}
			}

			// нормализация опций ПОСЛЕ типа
			optsRaw := strings.TrimSpace(tail)
			if i := strings.IndexByte(optsRaw, '#'); i >= 0 {
				optsRaw = strings.TrimSpace(optsRaw[:i])
			}
			if strings.HasPrefix(strings.ToLower(optsRaw), "options:") {
				optsRaw = strings.TrimSpace(optsRaw[len("options:"):])
			}
			optsRaw = strings.ReplaceAll(optsRaw, ",", " ")
			optsTokens := splitOptionTokens(optsRaw)

			// проверка на дубликаты и зарезервированные имена
			lower := strings.ToLower(name)
			if _, dup := fieldNames[lower]; dup {
				return nil, fmt.Errorf("%s.%s: duplicate field name in DSL", current.Module, current.Name)
			}
			switch lower {
			case "id", "version", "created_at", "updated_at":
				return nil, fmt.Errorf("%s.%s: field %q clashes with reserved system column", current.Module, current.Name, name)
			}

			f := Field{
				Name:    name,
				Type:    rawType,
				Options: map[string]string{},
			}

			// распознаём тип
			if mm := enumRe.FindStringSubmatch(rawType); mm != nil {
				f.Type = "enum"
				inside := strings.TrimSpace(mm[1])
				for _, p := range strings.Split(inside, ",") {
					if s := strings.Trim(strings.TrimSpace(p), `"'`); s != "" {
						f.Enum = append(f.Enum, s)
					}
				}
			} else if mm := refRe.FindStringSubmatch(rawType); mm != nil {
				f.Type = "ref"
				f.RefTarget = strings.TrimSpace(mm[1])
			} else if mm := arrayRe.FindStringSubmatch(rawType); mm != nil {
				f.Type = "array"
				elem := strings.TrimSpace(mm[1])
				f.ElemType = elem
				// array[enum[...]]
				if em := enumRe.FindStringSubmatch(elem); em != nil {
					f.ElemType = "enum"
					inside := strings.TrimSpace(em[1])
					for _, p := range strings.Split(inside, ",") {
						if s := strings.Trim(strings.TrimSpace(p), `"'`); s != "" {
							f.Enum = append(f.Enum, s)
						}
					}
				}
				// array[ref[...]]
				if rm := refRe.FindStringSubmatch(elem); rm != nil {
					f.ElemType = "ref"
					f.RefTarget = strings.TrimSpace(rm[1])
				}
			}

			// парсим опции: флаги и k=v
			for _, tok := range optsTokens {
				tok = strings.TrimSpace(tok)
				if tok == "" {
					continue
				}
				if !strings.Contains(tok, "=") {
					f.Options[strings.ToLower(tok)] = "true"
					continue
				}
				kv := strings.SplitN(tok, "=", 2)
				k := strings.ToLower(strings.TrimSpace(kv[0]))
				v := strings.TrimSpace(kv[1])
				if len(v) >= 2 {
					if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
						v = v[1 : len(v)-1]
					}
				}
				if k != "" {
					f.Options[k] = v
				}
			}

			current.Fields = append(current.Fields, f)
			fieldNames[lower] = struct{}{}
			continue
		}
	}

	if current != nil {
		entities = append(entities, current)
	}
	return entities, scanner.Err()
}

func LoadAllEntities(root string) (map[string]*Entity, error) {
	result := make(map[string]*Entity)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".dsl") {
			return nil
		}

		ents, err := LoadEntities(path) // твой парсер
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}

		for _, e := range ents {
			if e == nil || e.Name == "" {
				return fmt.Errorf("empty entity name in %s", path)
			}
			if e.Module == "" {
				return fmt.Errorf("entity %q in %s has no module — add `module <name>` at the top", e.Name, path)
			}
			fqn := e.Module + "." + e.Name
			if _, exists := result[fqn]; exists {
				return fmt.Errorf("duplicate entity %q in module %q (file: %s)", e.Name, e.Module, path)
			}
			result[fqn] = e
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}
