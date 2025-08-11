// cmd/server/main.go
package main

import (
	"fmt"
	"log"
	"strings"

	"kalita/internal/api"
	"kalita/internal/config"
	"kalita/internal/dsl"
	"kalita/internal/pg"
	"kalita/internal/reference"
)

func main() {

	cfgPath := "./config/config.json"
	cfg := config.LoadWithPath(cfgPath)

	fmt.Printf("Kalita starting on :%s (db=%s, autoMigrate=%v, blob=%s)\n",
		cfg.Port, tern(cfg.DBURL != "", "pg", "memory"), cfg.AutoMigrate, cfg.BlobDriver)

	// DSL
	entityMap, err := dsl.LoadAllEntities(cfg.DSLDir)
	if err != nil {
		log.Fatalf("Ошибка загрузки DSL из %q: %v", cfg.DSLDir, err)
	}
	fmt.Printf("Загружено сущностей: %d\n", len(entityMap))
	entities := make([]*dsl.Entity, 0, len(entityMap))
	for _, e := range entityMap {
		entities = append(entities, e)
	}

	// --- PostgreSQL: подключение + (опц.) add-only DDL
	if cfg.DBURL != "" {
		db, err := pg.Open(cfg.DBURL)
		if err != nil {
			log.Fatalf("PG connect failed: %v", err)
		}
		defer db.Close()
		log.Printf("PG connected")

		if cfg.AutoMigrate {
			ddl, err := pg.GenerateDDL(entityMap)
			if err != nil {
				log.Fatalf("DDL generate failed: %v", err)
			}
			if err := pg.ApplyDDL(db, ddl); err != nil {
				log.Fatalf("DDL apply failed: %v", err)
			}
			log.Printf("DDL applied (add-only)")
		}
	}

	// Enums
	enumCatalog, err := reference.LoadEnumCatalog(cfg.EnumsDir)
	if err != nil {
		log.Fatalf("Ошибка загрузки enum-справочников из %q: %v", cfg.EnumsDir, err)
	}
	fmt.Printf("Загружено enum-справочников: %d\n", len(enumCatalog))

	// In-memory API (данные пока в памяти; PG — только схема)
	st := api.NewStorage(entities, enumCatalog)
	if strings.EqualFold(cfg.BlobDriver, "s3") {
		log.Printf("[warn] blob=s3 ещё не подключён — используем локальное хранилище (root=%q)\n", cfg.FilesRoot)
	}
	st.Blob = &api.LocalBlobStore{Root: cfg.FilesRoot}

	// HTTP
	fmt.Printf("Стартуем сервер Kalita на :%s...\n", cfg.Port)
	api.RunServer(":"+cfg.Port, st)
}

func tern[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
