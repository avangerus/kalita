// cmd/server/main.go
package main

import (
	"fmt"
	"log"
	"os"

	"kalita/internal/api"
	"kalita/internal/dsl"
	"kalita/internal/reference"
)

func main() {
	// 1) Загружаем все *.dsl рекурсивно из папки dsl/
	entityMap, err := dsl.LoadAllEntities("dsl")
	if err != nil {
		log.Fatalf("Ошибка загрузки DSL: %v", err)
	}
	fmt.Printf("Загружено сущностей: %d\n", len(entityMap))

	// Преобразуем map[string]*Entity -> []*Entity для api.NewStorage
	entities := make([]*dsl.Entity, 0, len(entityMap))
	for _, e := range entityMap {
		entities = append(entities, e)
	}

	// 2) Загружаем enum-справочники
	enumCatalog, err := reference.LoadEnumCatalog("reference/enums/")
	if err != nil {
		log.Fatalf("Ошибка загрузки enum-справочников: %v", err)
	}
	fmt.Printf("Загружено enum-справочников: %d\n", len(enumCatalog))

	// 3) Инициализируем storage
	storage := api.NewStorage(entities, enumCatalog)

	// 4) Запускаем сервер (RunServer ничего не возвращает)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("Стартуем сервер Kalita на :%s...\n", port)
	api.RunServer(":"+port, storage)
}
