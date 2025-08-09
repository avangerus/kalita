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

// LoadEntities читает entities.dsl и возвращает список Entity
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
			current = &Entity{Name: m[1]}
			if current.Module == "" {
				current.Module = currentModule
			}
			inConstraints = false
			continue
		}
		if current == nil {
			// игнорируем всё вне сущности
			continue
		}

		// ----- БЛОК CONSTRAINTS -----
		// старт блока
		if reConstraintsStart.MatchString(line) {
			inConstraints = true
			continue
		}

		if inConstraints {
			// строка unique(...)
			if m := reUniqueLine.FindStringSubmatch(line); m != nil {
				parts := strings.Split(m[1], ",")
				set := make([]string, 0, len(parts))
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if p != "" {
						set = append(set, p)
					}
				}
				if len(set) > 0 {
					current.Constraints.Unique = append(current.Constraints.Unique, set)
				}
				continue
			}

			// если началась новая секция (entity/module) — выходим из constraints и обработаем строку заново
			if strings.HasPrefix(line, "entity ") || strings.HasPrefix(line, "module ") {
				inConstraints = false
				// НЕ continue — пускай ниже обработается как entity/module
			} else {
				// любая другая строка — выходим из блока constraints
				inConstraints = false
				continue
			}
		}
		// ----- КОНЕЦ БЛОКА CONSTRAINTS -----

		// Поля
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

			optsTokens := strings.Fields(strings.TrimSpace(tail))

			f := Field{
				Name:    name,
				Type:    rawType,
				Options: map[string]string{},
			}

			// распознаём тип
			if mm := enumRe.FindStringSubmatch(rawType); mm != nil {
				f.Type = "enum"
				inside := strings.TrimSpace(mm[1])
				parts := strings.Split(inside, ",")
				for _, p := range parts {
					s := strings.Trim(strings.TrimSpace(p), `"'`)
					if s != "" {
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
					parts := strings.Split(inside, ",")
					for _, p := range parts {
						s := strings.Trim(strings.TrimSpace(p), `"'`)
						if s != "" {
							f.Enum = append(f.Enum, s)
						}
					}
				}
				// array[ref[...]]
				if rm := refRe.FindStringSubmatch(elem); rm != nil {
					f.ElemType = "ref"
					f.RefTarget = strings.TrimSpace(rm[1])
				}
			} else {
				// примитивы: string,int,float,bool,date,datetime — оставляем как есть
			}

			// --- опции: required, unique, default=..., readonly, on_delete=... ---
			for _, o := range optsTokens {
				if strings.HasPrefix(o, "default=") {
					kv := strings.SplitN(o, "=", 2)
					if len(kv) == 2 {
						f.Options["default"] = kv[1]
					}
					continue
				}
				if o == "required" {
					f.Options["required"] = "true"
					continue
				}
				if o == "unique" {
					f.Options["unique"] = "true"
					continue
				}
				if o == "readonly" {
					f.Options["readonly"] = "true"
					continue
				}
				if strings.HasPrefix(o, "on_delete=") { // ← НОВОЕ
					kv := strings.SplitN(o, "=", 2)
					if len(kv) == 2 {
						f.Options["on_delete"] = strings.ToLower(strings.TrimSpace(kv[1])) // restrict/set_null/cascade
					}
					continue
				}
			}

			current.Fields = append(current.Fields, f)
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
