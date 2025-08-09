// api/names.go
package api

import "strings"

// NormalizeEntityName возвращает FQN ("module.name") по паре {module, entity}.
// Если module пустой, пытается найти уникальную сущность с таким именем среди всех модулей.
func (s *Storage) NormalizeEntityName(module, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	ml := strings.ToLower(strings.TrimSpace(module))
	nl := strings.ToLower(strings.TrimSpace(name))

	// 1) есть модуль — ищем точное/регистронезависимое совпадение FQN
	if ml != "" {
		// сначала прямой ключ
		if _, ok := s.Schemas[module+"."+name]; ok {
			return module + "." + name, true
		}
		// регистронезависимо
		for fqn := range s.Schemas {
			dot := strings.IndexByte(fqn, '.')
			if dot <= 0 {
				continue
			}
			fm, fn := fqn[:dot], fqn[dot+1:]
			if strings.ToLower(fm) == ml && strings.ToLower(fn) == nl {
				return fqn, true
			}
		}
		return "", false
	}

	// 2) модуля нет — ищем ИМЕНО ОДНО уникальное имя среди всех
	var found string
	for fqn := range s.Schemas {
		dot := strings.IndexByte(fqn, '.')
		if dot <= 0 {
			continue
		}
		fn := fqn[dot+1:]
		if strings.ToLower(fn) == nl {
			if found != "" { // неуникально
				return "", false
			}
			found = fqn
		}
	}
	if found != "" {
		return found, true
	}
	return "", false
}
