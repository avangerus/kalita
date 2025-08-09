package dsl

type Entity struct {
	Name        string
	Module      string
	Fields      []Field
	Constraints Constraints
}

type Constraints struct {
	Unique [][]string `json:"unique,omitempty"` // наборы полей
}

// Field описывает поле сущности
type Field struct {
	Name      string            // имя поля
	Type      string            // базовый тип: string,int,float,bool,date,datetime,enum,ref,array
	Enum      []string          // значения enum, если Type == "enum"
	RefTarget string            // целевая сущность для ref[...], если Type == "ref"
	ElemType  string            // тип элемента для array[T], если Type == "array" (T без префикса array)
	Options   map[string]string // required, unique, default...
}
