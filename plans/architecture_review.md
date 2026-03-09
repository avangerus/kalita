# Architecture Review

## 1. Architecture Overview

**Architecture Style:** Modular Monolith

The system follows a modular monolith architecture with clear package boundaries. It provides a REST API backend for a data management system with DSL-based entity definitions, supporting CRUD operations, blob storage, PostgreSQL persistence, and enum reference management.

**Major Subsystems:**
- **API Layer** (`internal/api/`) - HTTP handlers and request processing
- **Configuration** (`internal/config/`) - Application configuration management
- **DSL Parser** (`internal/dsl/`) - Entity definition parsing
- **PostgreSQL Integration** (`internal/pg/`) - Database schema and DDL execution
- **Reference Management** (`internal/reference/`) - Enum catalog handling

## 2. Key Architectural Components

### `internal/api/`
Core API layer handling HTTP requests/responses.
- **handlers.go** - REST endpoints (Create, List, Get, Update, Patch, Delete, Bulk operations)
- **storage.go** - In-memory storage with mutex protection for CRUD operations
- **validation.go** - Schema validation and type coercion
- **blob.go** - Blob storage interface and local implementation
- **router.go** - Server initialization and route registration
- **names.go** - Entity name normalization
- **query.go** - List parameters parsing and sorting logic
- **meta.go** - Metadata endpoints for entity/field introspection
- **schema_lint.go** - DSL schema validation

### `internal/config/`
Configuration management.
- **config.go** - Config struct, JSON loading, environment variable overrides

### `internal/dsl/`
DSL parsing and entity modeling.
- **parser.go** - Loads and parses `.dsl` files
- **model.go** - Entity, Field, Constraints definitions

### `internal/pg/`
PostgreSQL integration.
- **schema.go** - DDL generation from entities
- **apply.go** - DDL execution with error handling
- **conn.go** - Database connection management

### `internal/reference/`
Enum reference management.
- **leader.go** - Loads enum catalogs from YAML
- **model.go** - EnumDirectory, EnumItem models

## 3. Data Flow

### Startup Flow
1. Load configuration from JSON file + environment variables
2. Parse DSL entity definitions from `dslDir`
3. Load enum catalogs from `enumsDir`
4. Generate DDL from entities (if `autoMigrate` enabled)
5. Apply DDL to PostgreSQL
6. Initialize in-memory Storage with schemas and enums
7. Start HTTP server on configured port

### Request Flow
1. **HTTP Request** → Gin router
2. **Handler** (`handlers.go`) → Normalize entity name via `storage.NormalizeEntityName()`
3. **Validation** (`validation.go`) → Validate payload against schema, coerce types
4. **Storage** (`storage.go`) → Read/Write in-memory data with RWMutex protection
5. **Response** → Return JSON to client

### Blob Upload Flow
1. File upload request → `UploadFileHandler`
2. Generate unique key: `YYYY/MM/randomHex(16)`
3. `LocalBlobStore.Put()` → Create directory structure, write file
4. Store reference in entity data

## 4. Coupling and Boundaries

### Tight Coupling
- **api/handlers.go** - Large file with many responsibilities (all CRUD operations, filtering, bulk operations, child discovery)
- **api/storage.go** - Directly couples in-memory storage with business logic; no repository abstraction
- **api/validation.go** - Strongly coupled to both Storage and DSL models

### Unclear Responsibilities
- **api/query.go** - Mixed concerns: sorting, filtering, list parameters parsing
- **api/files.go** - File handling mixed with entity operations
- **api/meta.go** - Metadata endpoints in same package as core handlers

### Missing Boundaries
- No clear separation between "service layer" and "data layer"
- In-memory storage (`storage.go`) mixes data access with business rules (e.g., `FindIncomingRefs`)
- No transaction management abstraction
- Blob storage implementation tightly coupled to handlers

## 5. Architectural Strengths

- **Clear package structure** - Logical separation between API, config, DSL, database, and references
- **Interface-based design** - `BlobStore` interface allows different implementations
- **Schema-driven** - Entity definitions in DSL files provide flexibility
- **Mutex protection** - Storage uses RWMutex for concurrent access safety
- **Configuration flexibility** - JSON + environment variable override pattern
- **DDL generation** - Automatic schema migration reduces manual database work
- **Enum catalog** - Externalized enum management via YAML files

## 6. Architectural Weaknesses

- **In-memory storage** - Data not persisted across restarts; unsuitable for production distributed systems
- **No authentication** - No security middleware; must be added externally
- **Large handlers.go** - 1000+ lines with too many responsibilities
- **No async processing** - All operations synchronous; potential blocking
- **Limited query capabilities** - Basic filtering and sorting only
- **Generic error handling** - Limited detailed validation feedback
- **S3 driver not implemented** - Blob storage only supports local filesystem

## 7. Scalability and Maintainability Risks

### Scalability Risks
- **In-memory storage** - Single instance limitation; no horizontal scaling
- **No caching layer** - Repeated queries hit in-memory map every time
- **Synchronous operations** - Blocking I/O limits throughput

### Maintainability Risks
- **Monolithic handlers.go** - Difficult to navigate and modify
- **Mixed concerns in api/ package** - File handling, metadata, queries all in one package
- **Tight coupling to DSL** - Changes to DSL format require code updates
- **No unit test stubs visible** - Testing may be challenging

### Complexity Hotspots
- `handlers.go` - Bulk operations, filtering logic, child discovery
- `storage.go` - Complex locking patterns, reference resolution
- `validation.go` - Type coercion logic, unique constraint checking

## 8. Refactoring Opportunities

### High Priority
1. **Split handlers.go** - Separate into distinct handler files per entity type or operation group
2. **Extract service layer** - Create dedicated service package between handlers and storage
3. **Add repository interface** - Abstract storage to enable different backends

### Medium Priority
4. **Separate query logic** - Move `query.go` parsing to dedicated package
5. **Isolate file handling** - Move `files.go` to separate package or integrate with blob package
6. **Add caching layer** - Introduce cache interface for frequently accessed data

### Lower Priority
7. **Implement S3 blob driver** - Complete the blob storage abstraction
8. **Add authentication middleware** - Security boundary at router level
9. **Async operation support** - Consider message queue for long-running operations
10. **Transaction management** - Add database transaction support for multi-step operations
