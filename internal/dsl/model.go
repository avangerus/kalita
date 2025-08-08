package dsl

// Entity описывает структуру сущности из DSL
type Entity struct {
	Name   string
	Fields []Field
}

// Field описывает поле сущности
type Field struct {
	Name    string
	Type    string            // string, int, date, enum, ref и т.д.
	Enum    []string          // значения enum, если поле типа enum
	Options map[string]string // required, unique, default и прочие опции
}
