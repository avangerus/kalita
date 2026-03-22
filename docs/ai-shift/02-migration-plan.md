# Incremental migration plan toward an AI execution-control layer

## Planning principle
The near-term goal should be to make Kalita safer and more explicit for AI-assisted execution while preserving the current DSL/YAML contract and the current API behavior for human-driven clients.

## Phase 0: baseline and observability

### What can be added
- A repository-level inventory of current entity, field, catalog, and API behaviors as the compatibility baseline.
- Schema-diff reporting during `admin/reload` so changes can be reviewed before they affect AI-oriented clients.
- Structured audit logging for create/update/patch/delete/bulk operations, including actor type (`human`, `service`, `agent`) even if only `human` exists initially.
- Non-blocking diagnostics that classify entities and fields by automation risk using existing schema features.

### What must remain backward compatible
- Existing `.dsl` grammar and option parsing.
- Existing YAML catalog format.
- Existing CRUD and Meta API routes.
- Existing request/response semantics for current clients.

### What should be postponed
- New DSL syntax for workflow/agent orchestration.
- New durable execution engine assumptions.

### What should stay out of scope for now
- Replacing the existing parser or schema model.
- Introducing a new platform-wide declarative language for agents.

## Phase 1: add policy evaluation as a sidecar, not a rewrite

### What can be added
- A new policy layer that runs before mutation and returns one of: `allow`, `warn`, `require_review`, `deny`.
- Sidecar configuration or YAML policy files that reference existing entities, fields, operations, and catalogs instead of modifying entity DSL syntax immediately.
- A dry-run mode for mutations so an AI client can ask, "what would happen if I executed this change?" without committing data.
- Meta API enrichment that publishes policy hints such as `automation_safe`, `review_required`, `sensitive_fields`, or `allowed_operations`.

### What must remain backward compatible
- If no policy sidecar exists, behavior should remain current pass-through validation plus CRUD.
- Existing entity definitions must not require any new fields or annotations.
- Existing clients must continue to ignore new metadata safely.

### What should be postponed
- Embedding these policy semantics directly into the entity DSL.
- Hard-coding AI concepts into core field parsing.

### What should stay out of scope for now
- Autonomous multi-step planning inside request handlers.
- LLM invocation from the core request path.

## Phase 2: introduce execution intents above CRUD

### What can be added
- A new additive API surface for execution intents, such as "propose change", "simulate", "submit for review", or "execute approved action".
- Explicit action envelopes that carry actor identity, tool identity, justification, risk score, and trace metadata.
- Approval checkpoints for high-risk mutations using existing entities and catalogs, possibly reusing document-like patterns already present in the domain model.
- Policy decisions that annotate, rather than replace, current validation errors.

### What must remain backward compatible
- Direct CRUD must remain available for existing integrations and tests.
- CRUD-side validation behavior should remain authoritative for data integrity.
- Existing DSL files must still load without any execution-intent definitions.

### What should be postponed
- Making intent APIs mandatory for all writes.
- Inferring business workflows from LLM outputs alone.

### What should stay out of scope for now
- Full agent autonomy over bulk destructive operations.
- Direct tool execution driven solely by natural language.

## Phase 3: formalize AI-safe metadata gradually

### What can be added
- Optional schema annotations, only after the sidecar policy model proves stable.
- Additive field/entity metadata such as sensitivity, approval class, mutability window, or automation eligibility.
- Optional catalog-backed vocabularies for risk classes, review states, and execution modes.

### What must remain backward compatible
- All new annotations must be optional.
- Unknown options must continue to behave as inert metadata unless explicitly consumed by a new subsystem.
- Existing field meanings must remain unchanged.

### What should be postponed
- Dedicated new top-level DSL blocks for agents, tools, or plans until the operational model is proven.
- Any renaming of current `entity`, `constraints`, or catalog concepts.

### What should stay out of scope for now
- A large-scale migration from free-form `Options` into a wholly different AST.
- Breaking changes to current Meta API consumers.

## Phase 4: converge runtime orchestration behind a service boundary

### What can be added
- An internal service/orchestration layer beneath HTTP handlers so policy evaluation, audit, simulation, and mutation share one execution pipeline.
- Durable execution logs and review records, potentially backed by PostgreSQL once runtime persistence strategy is ready.
- A uniform mutation command model for CRUD, bulk, agent requests, and review-driven actions.

### What must remain backward compatible
- HTTP routes and payloads should remain stable, even if handler internals are refactored.
- Schema loading and validation behavior must remain the contract of record.

### What should be postponed
- Full replacement of in-memory runtime storage unless accompanied by compatibility testing for API behavior.
- Deep workflow engine ambitions.

### What should stay out of scope for now
- Replatforming the whole repository around event sourcing, BPMN, or a new orchestration framework.

## Recommended first concrete increments
1. Add schema-diff and policy-diagnostic reporting to reload/admin flows.
2. Add additive audit context to write operations.
3. Add a dry-run mutation path using the current validation logic.
4. Add additive Meta API fields for automation/risk hints.
5. Add optional sidecar YAML policy files keyed by existing `module.Entity` names.
6. Only after those stabilize, consider optional DSL annotations for AI execution safety.

## Why this migration path fits Kalita's current reality
- The current strongest seams are validation, Meta API, reload, and catalog loading.
- The current runtime is synchronous and in-memory, so an AI control plane should begin with policy wrappers, not claims of autonomous durable orchestration.
- The current DSL model is intentionally simple and stringly-typed, so premature grammar expansion would create more compatibility risk than value.
