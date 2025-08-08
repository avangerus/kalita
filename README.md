kalita/
│
├── cmd/                        # Запуск: server, cli, миграции, воркеры
│   ├── server/
│   │   └── main.go
│   └── ...
│
├── internal/                   # Ядро бизнес-логики, НЕ экспортируется как lib
│   ├── dsl/                    # Парсеры/валидаторы всех dsl-слоёв (entity, workflow, ui, roles, integration, olap, report)
│   │   ├── entity.go
│   │   ├── workflow.go
│   │   ├── ui.go
│   │   ├── roles.go
│   │   ├── integration.go
│   │   ├── olap.go
│   │   ├── report.go
│   │   ├── parser.go      # общий парсер/лоадер DSL-файлов
│   │   └── validator.go
│   ├── model/                  # Go-структуры: Entity, Field, Workflow, Role, User и др.
│   ├── store/                  # Доступ к данным (EAV, SQL, in-memory, справочники, кэш)
│   ├── reference/              # Работа со справочниками: yaml, sql, динамика, каталоги
│   ├── rbac/                   # Роли, права, ABAC, политики доступа
│   ├── api/                    # REST API, автогенерация по DSL (gin/echo)
│   │   ├── router.go
│   │   ├── handlers.go
│   │   ├── middleware.go
│   │   └── docs.go
│   ├── workflow/               # Workflow engine, автоматизация бизнес-логики
│   ├── integration/            # REST, message bus, шина, внешние системы, синхронизация
│   ├── bi/                     # OLAP engine, аналитика, отчёты, витрины
│   ├── audit/                  # История, события, логирование изменений
│   ├── ui/                     # Генерация UI (формы, списки, layout) для фронта
│   ├── i18n/                   # Локализация, перевод DSL/справочников/UI
│   └── ...
│
├── dsl/                        # DSL-описания (ядро и модули)
│   ├── core/                   # User, Role, Department, ...
│   │   ├── entities.dsl
│   │   ├── workflow.dsl
│   │   ├── ui.dsl
│   │   ├── roles.dsl
│   │   └── ...
│   ├── modules/                # Расширения/домены/модули/плагины
│   │   ├── hr/
│   │   │   ├── entities.dsl
│   │   │   ├── workflow.dsl
│   │   │   └── ...
│   │   ├── vacations/
│   │   │   └── ...
│   │   └── ...
│   └── ...
│
├── reference/                  # Справочники (yaml/csv, в т.ч. enums)
│   ├── enums/
│   │   ├── project_status.yaml
│   │   ├── vacation_status.yaml
│   │   └── ...
│   ├── countries.yaml
│   ├── departments.yaml
│   ├── catalog.yaml            # Каталог/метаданные всех справочников
│   └── ...
│
├── migrations/                 # SQL-миграции БД (структура, EAV, OLAP, ...)
│   ├── 001_init.sql
│   ├── 002_eav.sql
│   └── ...
│
├── scripts/                    # Импорт/экспорт, генерация enum/структур из yaml, тесты
│   ├── generate_enum.go
│   ├── import_reference.go
│   └── ...
│
├── configs/                    # Конфиги приложения (yaml/json/env)
│   ├── app.yaml
│   ├── db.yaml
│   └── ...
│
├── web/                        # Фронт (если fullstack, SPA/SSR, статика)
│   ├── dist/
│   ├── templates/
│   └── ...
│
├── api/                        # Спецификации API, swagger/openapi, proto
│   ├── openapi.yaml
│   └── ...
│
├── testdata/                   # Тестовые данные, демо-fixtures
│   └── ...
│
├── docs/                       # Документация, схемы, спецификации DSL
│   ├── architecture.md
│   ├── dsl-spec.md
│   ├── reference.md
│   └── ...
│
├── .env
├── .gitignore
├── go.mod
├── go.sum
├── README.md
└── Dockerfile