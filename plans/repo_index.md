# Repository Index

## Project Purpose

REST API backend Synchronous operations

Let me create the repository index document. for a data management system with DSL-based entity definitions. Provides CRUD operations, blob storage, PostgreSQL persistence, and enum reference management.

## Technology Stack

- **Language**: Go
- **Web Framework**: Gin
- **Database**: PostgreSQL (via pgx)
- **ID Generation**: ULID
- **Configuration**: JSON + Environment variables

## Entry Points

- `internal/api/router.go` — `RunServer(addr, storage)` starts the HTTP server
- `internal/config/config.go` — `LoadWithPath(jsonPath)` loads configuration

## Key Modules

### `internal/api/`
Core API layer handling HTTP requests/responses.
- `handlers.go` — REST handlers (Create, List, Get, Update, Patch, Delete, Bulk operations)
- `storage.go` — In-memory storage with mutex protection
- `validation.go` — Schema validation and type coercion
- `blob.go` — Blob storage interface and local implementation
- `router.go` — Server initialization and route registration
- `names.go` — Entity name normalization

### `internal/config/`
Configuration management.
- `config.go` — Config struct, loading from JSON, environment variable overrides

### `internal/dsl/`
DSL parsing and entity modeling.
- `parser.go` — Loads and parses `.dsl` files
- `model.go` — Entity, Field, Constraints definitions

### `internal/pg/`
PostgreSQL integration.
- `schema.go` — DDL generation from entities
- `apply.go` — DDL execution with error handling

### `internal/reference/`
Enum reference management.
- `leader.go` — Loads enum catalogs from YAML
- `model.go` — EnumDirectory, EnumItem models

## Important Configs

JSON config file with fields:
- `port` — HTTP server port (default "8080")
- `dslDir` — Directory with `.dsl` entity definitions
- `enumsDir` — Directory with enum YAML files
- `dbUrl` — PostgreSQL connection URL
- `autoMigrate` — Enable automatic schema migration
- `blobDriver` — "local" or "s3"
- `filesRoot` — Local storage root path

## Main Data Flow

1. **Startup**: Load config → Parse DSL files → Generate DDL → Apply to PostgreSQL → Initialize storage
2. **Request**: Gin handler → Normalize entity name → Validate against schema → Read/Write storage → Return JSON
3. **Blobs**: Upload → LocalBlobStore.Put → Generate key with date path + random hex → Save to filesystem

## High Impact Files

- `internal/api/storage.go` — Core data management, mutex-protected CRUD
- `internal/api/handlers.go` — All REST endpoints, validation orchestration
- `internal/dsl/parser.go` — Entity definition parsing, schema discovery
- `internal/pg/schema.go` — SQL DDL generation, foreign key policies
- `internal/api/validation.go` — Type coercion, unique constraint checks

## Architectural Risks

- **In-memory storage**: Data not persisted across restarts; not suitable for distributed部署
- **No authentication**: No visible auth middleware; security must be added externally
- **Synchronous operations**: Storage uses RWMutex but no async processing
- **Limited query capabilities**: Basic filtering and sorting only
- **Error handling**: Generic error responses; limited detailed validation feedback
- **Blob storage**: Local implementation only; S3 driver mentioned but not implemented
