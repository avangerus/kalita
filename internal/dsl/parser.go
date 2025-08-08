package dsl

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func LoadAllEntities(rootDir string) ([]*Entity, error) {
	var entities []*Entity

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == "entities.dsl" {
			found, err := LoadEntities(path)
			if err != nil {
				return fmt.Errorf("Ошибка парсинга %s: %v", path, err)
			}
			entities = append(entities, found...)
		}
		return nil
	})
	return entities, err
}

// LoadEntities читает entities.dsl и возвращает список Entity
func LoadEntities(path string) ([]*Entity, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entities []*Entity
	var current *Entity

	entityRe := regexp.MustCompile(`^entity\s+(\w+):`)
	fieldRe := regexp.MustCompile(`^\s*([\w_]+):\s*([\w_]+)(.*)$`)
	enumRe := regexp.MustCompile(`enum\[(.*)\]`)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if match := entityRe.FindStringSubmatch(line); match != nil {
			if current != nil {
				entities = append(entities, current)
			}
			current = &Entity{Name: match[1]}
		} else if match := fieldRe.FindStringSubmatch(line); match != nil && current != nil {
			name := match[1]
			typ := match[2]
			opts := strings.Fields(match[3])
			options := map[string]string{}
			var enumValues []string

			// Если поле — enum, то вытаскиваем значения
			if typ == "enum" || strings.HasPrefix(typ, "enum") {
				// enum[Draft, Approved, Closed]
				if enumMatch := enumRe.FindStringSubmatch(line); enumMatch != nil {
					vals := strings.Split(enumMatch[1], ",")
					for i := range vals {
						vals[i] = strings.TrimSpace(vals[i])
					}
					enumValues = vals
				}
				typ = "enum"
			}

			// Обработка опций (required, unique, default)
			for _, o := range opts {
				if strings.HasPrefix(o, "default") {
					arr := strings.SplitN(o, "=", 2)
					if len(arr) == 2 {
						options["default"] = arr[1]
					}
				} else if o == "required" {
					options["required"] = "true"
				} else if o == "unique" {
					options["unique"] = "true"
				}
			}

			current.Fields = append(current.Fields, Field{
				Name:    name,
				Type:    typ,
				Enum:    enumValues,
				Options: options,
			})
		}
	}
	if current != nil {
		entities = append(entities, current)
	}
	return entities, scanner.Err()
}
