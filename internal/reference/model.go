package reference

// EnumDirectory описывает один справочник типа enum
type EnumDirectory struct {
	Name  string     `yaml:"name"`
	Items []EnumItem `yaml:"items"`
}

type EnumItem struct {
	Code string `yaml:"code"`
	Name string `yaml:"name"`
	// Дополнительные поля: Order, Aliases, ValidFrom, ValidTo и т.д.
	Order     int    `yaml:"order,omitempty"`
	ValidFrom string `yaml:"valid_from,omitempty"`
	ValidTo   string `yaml:"valid_to,omitempty"`
}
