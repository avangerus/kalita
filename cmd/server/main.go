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
	// 1. Загружаем DSL-сущности (например, из dsl/core/entities.dsl)
	//entities, err := dsl.LoadEntities("dsl/core/entities.dsl")
	entities, err := dsl.LoadAllEntities("dsl")
	if err != nil {
		log.Fatalf("Ошибка загрузки DSL: %v", err)
	}
	fmt.Printf("Загружено сущностей: %d\n", len(entities))

	// 2. Загружаем enum-справочники
	enumCatalog, err := reference.LoadEnumCatalog("reference/enums/")
	if err != nil {
		log.Fatalf("Ошибка загрузки enum-справочников: %v", err)
	}
	fmt.Printf("Загружено enum-справочников: %d\n", len(enumCatalog))

	// 3. Инициализируем in-memory хранилище
	storage := api.NewStorage(entities, enumCatalog)

	// 4. Запускаем REST API сервер
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("Стартуем сервер Kalita на :%s...\n", port)
	api.RunServer(":"+port, storage)
}
