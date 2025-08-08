package api

import (
	"kalita/internal/dsl"
	"kalita/internal/reference"
	"sync"
)

// Storage — универсальное in-memory хранилище объектов по сущностям
type Storage struct {
	mu      sync.RWMutex
	Schemas map[string]*dsl.Entity
	Enums   map[string]reference.EnumDirectory
	Data    map[string][]map[string]interface{}
}

func NewStorage(entities []*dsl.Entity, enums map[string]reference.EnumDirectory) *Storage {
	schemas := make(map[string]*dsl.Entity)
	for _, e := range entities {
		schemas[e.Name] = e
	}
	return &Storage{
		Schemas: schemas,
		Enums:   enums,
		Data:    make(map[string][]map[string]interface{}),
	}
}
