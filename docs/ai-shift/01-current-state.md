# Kalita current-state architecture memo

## Purpose of this memo
This memo describes Kalita as it exists in the repository today so future AI-oriented evolution can preserve the current YAML/DSL contract instead of replacing it.

## 1. Repository structure at a glance
- `cmd/server/` contains the executable entrypoint and starts the HTTP server through application bootstrap.
- `internal/app/` wires configuration, DSL loading, enum catalog loading, optional PostgreSQL DDL generation, and in-memory runtime storage.
- `internal/schema/` defines the DSL entity model, parser, and schema lint checks.
- `internal/runtime/` holds the in-memory execution model: schema registry, records, IDs, name resolution, list parameter parsing, and query helpers.
- `internal/validation/` enforces request-time data validation and normalization against the loaded schema.
- `internal/http/` exposes the operational surface: CRUD, bulk operations, meta endpoints, file upload/download, and admin reload.
- `internal/postgres/` generates and optionally applies add-only database DDL from the loaded schema, but is not yet the runtime record store.
- `internal/catalog/` loads YAML enum/reference catalogs.
- `internal/blob/` provides the blob storage abstraction with a local implementation.
- `dsl/` contains the business DSL source files that define modules and entities.
- `reference/enums/` contains YAML enum catalogs used by validation and meta exposure.
- `testdata/python/` contains API-level and behavior tests, which is a practical map of externally visible platform behavior.

## 2. Current DSL / YAML model

### Entity DSL reality today
Kalita's implemented DSL is centered on `module` declarations, `entity` blocks, field definitions, and `constraints` blocks. The parser currently understands:
- scalar field types such as `string`, `int`, `float`, `money`, `bool`, `date`, `datetime`
- inline enums with `enum[...]`
- references with `ref[...]`
- arrays with `array[...]`, including `array[ref[...]]` and `array[enum[...]]`
- field options encoded as flags or `key=value` tokens, such as `required`, `unique`, `default=...`, `readonly`, `catalog=...`, `on_delete=...`, and validation-like options such as `min`, `max`, `pattern`, `max_len`
- composite uniqueness via `constraints: unique(...)`

The effective in-memory schema model is still narrow: each entity has `Name`, `Module`, `Fields`, and `Constraints`; each field has `Name`, `Type`, `Enum`, `RefTarget`, `ElemType`, and free-form `Options`. This is important: most higher-level semantics are still represented as stringly-typed options rather than dedicated AST nodes.

### YAML model reality today
YAML is currently used for enum/reference catalogs, not for the entity DSL itself. Each catalog has a `name` and `items`, where items provide at least `code` and `name`, with optional metadata such as `order`, `valid_from`, and `valid_to`.

### Observed examples in the repo
- `dsl/core/entities.dsl` shows the base platform module (`core`) with `User`, `Project`, `Attachment`, plus a small `test` module.
- `dsl/modules/olga/entities.dsl` is the strongest domain example and demonstrates cross-module refs, self-refs, arrays, composite uniqueness, and document-like enterprise entities.
- `reference/enums/project_status.yaml` shows catalog-backed field validation through YAML.

## 3. Runtime / execution model

### Boot and load sequence
At startup, Kalita:
1. loads config
2. loads all DSL entities from the configured DSL directory
3. optionally connects to PostgreSQL and applies add-only generated DDL
4. loads enum catalogs from YAML
5. creates an in-memory `runtime.Storage`
6. attaches a local blob store implementation
7. starts Gin-based HTTP routes

This means the authoritative runtime model is the loaded schema plus in-memory record maps, not the PostgreSQL database.

### Record execution model
Runtime storage is an in-memory registry:
- `Schemas`: `module.Entity` -> parsed entity schema
- `Data`: `module.Entity` -> `id` -> record
- `Enums`: catalog name -> enum directory
- ULID-based ID generation
- optimistic concurrency with `version`, `ETag`, and `If-Match`
- soft delete via the `Deleted` flag

Records are stored as a generic `map[string]interface{}` payload plus system fields (`id`, `version`, timestamps). There is no separate workflow engine, rule engine, or job engine in the current implementation; execution is primarily synchronous request validation plus CRUD mutation.

### API execution model
The HTTP package is the main orchestration layer. It currently performs:
- entity name normalization
- request binding
- default application
- readonly/system-field protection
- validation against schema
- in-memory create/update/delete/restore/bulk mutation
- filtering, sorting, pagination, and free-text `q`
- expansion of related records for reads
- Meta API generation from the loaded schema
- admin reload of DSL and enum catalogs without restart

This makes `internal/http/handlers.go` the de facto execution-control layer today, even though it is transport-centric rather than policy-centric.

### Persistence model
Persistence is split:
- runtime data is in memory
- database integration is presently schema-oriented only (DDL generation/apply)
- blob storage has an interface boundary and a local driver implementation

For AI-assisted enterprise execution, that split matters because any future policy or agent-control layer must not assume durable transactional execution already exists.

## 4. Major bounded contexts in the current codebase

### 4.1 Schema loading and static contract
Packages: `internal/schema`, `dsl/`
- concerns: parse entity DSL, represent schema, lint schema contradictions
- role in current system: defines the platform contract other packages consume
- stability requirement: highest, because it defines compatibility for existing DSL files

### 4.2 Catalog/reference data loading
Packages: `internal/catalog`, `reference/enums/`
- concerns: load YAML reference catalogs for enum-like validation and UI/meta use
- role: externalized controlled vocabularies and metadata
- AI relevance: strong candidate for policy vocabularies, approval classes, risk levels, allowed tool catalogs

### 4.3 Runtime state and query semantics
Packages: `internal/runtime`
- concerns: storage, IDs, entity normalization, list/sort/query helpers
- role: current stateful execution substrate
- AI relevance: likely home for non-transport execution controls if Kalita becomes an orchestration layer

### 4.4 Validation and data integrity
Packages: `internal/validation`, parts of `internal/schema`
- concerns: required/type/enum/ref/unique/readonly/default validation and normalization
- role: current gatekeeper before mutation
- AI relevance: best current insertion point for agent-safe validation, policy checks, explainability, and dry-run semantics

### 4.5 HTTP / API orchestration
Packages: `internal/http`
- concerns: CRUD, bulk ops, meta, files, admin reload, response shaping
- role: operational surface and current application service layer by default
- AI relevance: likely entry point for execution intents, review flows, simulation mode, and audit wrappers

### 4.6 Database projection
Packages: `internal/postgres`
- concerns: schema projection to PostgreSQL DDL
- role: infrastructural add-on, not system-of-record behavior today
- AI relevance: useful for future durable execution logging, but not yet suitable as the basis of an AI control plane

### 4.7 Blob/file handling
Packages: `internal/blob`, `internal/http/files.go`
- concerns: attachment persistence and download/upload handling
- AI relevance: possible boundary for document-grounded agents, but should remain capability-scoped and policy-checked

## 5. Extension points where AI / agent-safe validation can be introduced incrementally

The safest extension points are the ones that wrap existing behavior instead of changing the DSL grammar first.

### 5.1 Pre-mutation policy checks before writes
Current create/update/patch/delete flows already centralize validation before commit. A new layer can be inserted between request normalization and storage mutation to evaluate:
- whether an action is allowed for an AI actor or tool
- whether required fields or referenced records are complete enough for automated execution
- whether a mutation should run immediately, require approval, or be rejected
- whether a mutation exceeds risk thresholds (bulk size, entity sensitivity, attachment presence, status changes)

This can initially operate from configuration and existing schema metadata without changing YAML syntax.

### 5.2 Schema lint expansion
`schema.Lint` already blocks contradictory definitions such as invalid `on_delete` policies and required refs with `set_null`. It could be extended conservatively to flag agent-risk patterns without breaking compatibility, for example:
- ambiguous entity names across modules
- writes to sensitive entities lacking explicit readonly or approval-related metadata
- unbounded text fields or attachment fields where automated execution should be reviewed

These should begin as warnings or advisory reports, not blocking DSL errors.

### 5.3 Meta API enrichment
The Meta API is a strong compatibility-preserving seam. Additional non-breaking metadata could expose:
- risk classification per entity/field
- automation suitability
- human-review requirements
- mutability constraints for agent clients
- provenance / explanation hooks

Because the Meta API is additive, existing DSL files can remain unchanged while new AI-aware clients consume richer metadata.

### 5.4 Admin reload and schema-version checkpoints
Admin reload already replaces loaded schemas and enum catalogs atomically. This is a natural place to add:
- compatibility diffing between old and new schema versions
- AI-safety policy validation on reload
- warnings when a schema change increases automation risk or breaks prior assumptions

### 5.5 Catalog-driven policy vocabularies
The YAML enum catalogs can evolve into controlled vocabularies for policy evaluation without touching entity syntax. Examples: approval levels, tool classes, execution modes, data sensitivity classes. This is probably the least disruptive way to introduce AI-governance semantics early.

### 5.6 File and action boundary checks
The blob store abstraction and attachment handlers are already a narrow capability boundary. If future agents are allowed to use files, this boundary can enforce content-type restrictions, virus/DLP scanning, provenance tagging, or tool-allowlists before an agent acts on uploaded content.

## 6. Likely compatibility risks for existing YAML/DSL

### High-risk changes
- changing existing field grammar or tokenization rules
- changing how `module.Entity` names are resolved or normalized
- changing the meaning of current options such as `required`, `unique`, `default`, `catalog`, `on_delete`
- changing Meta API shapes in a breaking way
- turning current advisory options into mandatory new syntax
- making previously valid DSL fail linting by default

### Medium-risk changes
- introducing reserved keywords into field option parsing that collide with current free-form `Options`
- changing enum catalog naming or lookup behavior (for example, case sensitivity or fallback rules)
- changing delete semantics, especially around `set_null`, restrict behavior, or soft-delete assumptions
- changing validation coercion rules in ways that alter accepted payloads
- moving runtime semantics from in-memory behavior to a durable engine without preserving API-visible behavior

### Lower-risk additions
- additive Meta API fields
- additive admin diagnostics
- additive policy evaluation that defaults to pass-through for existing schemas
- optional sidecar YAML/config for AI controls that references existing entities/fields
- dry-run/simulate endpoints that do not alter current CRUD contract

## 7. Practical conclusion
Kalita today is best understood as a schema-driven CRUD/meta platform with strong validation and a lightweight runtime registry. It is not yet an execution-control layer, but it already contains the minimum seams needed to become one incrementally:
- a textual schema contract
- a validation chokepoint before mutation
- a meta surface for machine-readable intent and capability description
- reloadable schema/catalog inputs
- a bounded storage abstraction for files

The most compatible path is therefore not to replace the DSL, but to add a policy-and-execution layer around the current schema, validation, and meta mechanisms.
