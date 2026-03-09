## Overall architecture
Kalita is organized as a modular monolith: `cmd/server` boots the app, `internal/app` composes runtime dependencies, `internal/http` exposes transport handlers, and domain-support packages (`schema`, `validation`, `runtime`, `postgres`, `catalog`, `blob`, `config`) provide core capabilities.

## Good decisions
- Clear top-level layering (`cmd` entrypoint → app bootstrap → package-level subsystems) makes startup and ownership easy to follow.
- Core concerns are split into dedicated packages: schema parsing/linting, validation, runtime storage/querying, Postgres DDL/connect/apply, enum catalogs, blob storage, and config loading.
- A small interface seam exists for file storage (`blob.BlobStore`), which supports future backend swaps.
- Router initialization is centralized and fail-fast schema linting happens before serving traffic.

## Architectural problems
- `internal/http/handlers.go` is very large and aggregates CRUD, bulk, filtering, restore, and delete semantics; this is a God-module trend.
- Transport and business logic are tightly coupled: handlers directly orchestrate storage and validation behavior instead of calling a service layer.
- Persistence boundaries are blurred: Postgres is used for schema migration while runtime data remains in-memory, creating a split-brain architecture.
- Cross-cutting concerns (validation, query parsing, relation checks) are spread across `http` + `runtime`, so responsibilities are only partially separated.
- Module boundaries are mostly reasonable for a small system, but weak around `http` ↔ `runtime` where orchestration and domain rules overlap.

## Biggest risks
1. Continued growth of the HTTP layer into a single change hotspot (`handlers.go`) will reduce maintainability and increase regression risk.
2. Mixed persistence model (in-memory data + Postgres DDL) can create operational confusion and production-readiness gaps.
3. Lack of an explicit service/domain boundary makes it hard to evolve business rules, transactions, and testing strategy independently of transport.
