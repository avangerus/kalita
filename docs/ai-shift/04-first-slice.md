# First minimal vertical slice: validated workflow action proposals

> Note: the referenced `docs/ai-shift/` analysis set was not present in this checkout, so this proposal is grounded in the current Kalita codebase shape and the target direction stated in the task.

## One concrete feature to implement first

Implement **proposal-only workflow transition requests** for records that already have a status field.

In the first slice, Kalita should accept a request shaped like:

```json
{
  "action": "submit",
  "record_version": 3,
  "payload": {
    "comment": "ready for approval"
  }
}
```

against a new additive endpoint conceptually like:

```text
POST /api/:module/:entity/:id/_actions/:action
```

But the first implementation should support **only one narrow behavior**:

1. resolve the entity from existing schema metadata,
2. verify that the record exists,
3. verify that the requested action is declared in YAML/DSL metadata,
4. verify that the action is allowed from the record's current status,
5. return a **validated proposal result** describing the allowed state transition,
6. optionally persist the status change only when the request includes an explicit commit flag in the server-side implementation plan.

For the very first slice, the safest default is **validate + execute a status transition only**, with no side effects beyond the status field update and version increment.

A minimal DSL extension should be additive and optional, for example by allowing an entity-level workflow block or YAML metadata that maps current status to named actions and target statuses. Existing entity definitions with just `status: enum[...]` continue to work unchanged.

Example of the smallest additive configuration shape:

```yaml
workflow:
  status_field: status
  actions:
    submit:
      from: [Draft]
      to: InApproval
    approve:
      from: [InApproval]
      to: Approved
```

This is not a grand workflow engine. It is only a declarative map of:

- action name,
- allowed source states,
- target state,
- optional later validation hooks.

## Why this is the best first step

This is the best first slice because it reaches the target direction with the least architectural risk:

1. **It introduces agent proposals without giving agents control of the canonical model.**
   Agents can ask Kalita to perform an action, but Kalita remains the place that validates structure and allowed transitions.

2. **It uses concepts Kalita already has.**
   The platform already has:
   - schema loading and linting,
   - record versioning,
   - validation before writes,
   - HTTP handlers that operate over module/entity/id,
   - status fields already present in real DSL models.

3. **It creates a true vertical slice.**
   The slice touches:
   - DSL/schema loading,
   - metadata exposure,
   - runtime validation,
   - HTTP execution path,
   - tests.

4. **It is additive rather than a rewrite.**
   No new orchestration framework, no generic agent runtime, no redesign of storage, and no attempt to solve approvals, effects, or multi-step plans yet.

5. **It establishes the control boundary that matters most.**
   The core product direction is not "agents mutate records freely"; it is "agents propose actions, Kalita decides whether the model can move." A controlled status transition is the smallest useful proof of that boundary.

6. **It preserves backward compatibility cleanly.**
   Entities without workflow metadata continue to behave exactly as they do today through normal CRUD.

## Affected files/modules

The first slice should be intentionally narrow and likely affect only these areas.

### 1. Schema model and parser

- `internal/schema/model.go`
  - add optional workflow/action metadata structures to `schema.Entity`.
- `internal/schema/parser.go`
  - parse the new optional workflow/action block from YAML/DSL-compatible input.
- `internal/schema/lint.go`
  - validate basic workflow consistency:
    - referenced status field exists,
    - `from` states exist in the status enum where applicable,
    - `to` state exists,
    - action names are unique.

### 2. Runtime validation / execution boundary

- new small runtime/service helper, preferably **not** added as more bulk logic inside the giant HTTP handler file.
  Suggested new file:
  - `internal/runtime/actions.go` or `internal/app/actions.go`

This helper should:
- load the record,
- inspect current status,
- validate requested action against schema action map,
- prepare the target state,
- perform optimistic version check,
- update only the configured status field.

### 3. HTTP transport

- `internal/http/router.go`
  - register one new additive action endpoint.
- preferably a new focused handler file, such as:
  - `internal/http/actions.go`

The handler should:
- parse action request,
- call the action validator/executor helper,
- return a predictable response contract.

### 4. Meta exposure

- `internal/http/meta.go`
  - expose declared actions for an entity so UIs and agents can discover them.

This keeps the proposal model inspectable and machine-readable.

### 5. Example DSL/test data

- `dsl/core/entities.dsl` or a dedicated test fixture DSL under `testdata`
  - add one minimal entity example with a workflow action map.

## Proposed tests

The first slice should be covered mostly by parser/lint/unit tests plus a few HTTP integration tests.

### Parser and lint tests

1. **Parses optional workflow action metadata**
   - entity with status field and two actions loads successfully.

2. **No workflow metadata remains valid**
   - existing DSL files still load unchanged.

3. **Rejects unknown status field**
   - workflow references `status_field: lifecycle` when field is missing.

4. **Rejects unknown source/target states**
   - `from` or `to` mentions enum values not present in the status enum.

5. **Rejects duplicate action names**
   - duplicate action declarations fail lint/parsing.

### Runtime/service tests

6. **Allows valid action from allowed source state**
   - record in `Draft`, action `submit`, transitions to `InApproval`.

7. **Rejects action from disallowed source state**
   - record in `Approved`, action `submit`, returns validation error.

8. **Rejects unknown action**
   - action name not declared in entity workflow metadata.

9. **Rejects version mismatch**
   - request version differs from stored record version.

10. **Does not mutate unrelated fields**
    - only status, version, and updated timestamp change.

### HTTP integration tests

11. **Action endpoint returns success payload for valid action**
    - includes record id, action, from state, to state, new version.

12. **Action endpoint returns 400/409 for invalid action or stale version**
    - preserve predictable API contract.

13. **Entity meta includes declared actions**
    - `GET /api/meta/:module/:entity` returns workflow/action metadata when present.

14. **Existing CRUD tests remain green**
    - create/list/get/update/patch flows for entities without workflow metadata remain unchanged.

## Exact compatibility guarantees

The first slice should commit to these guarantees explicitly.

1. **Existing YAML/DSL files remain valid with no required edits.**
   If an entity does not declare workflow action metadata, schema loading and CRUD behavior stay exactly as before.

2. **Existing CRUD endpoints keep their current semantics.**
   `POST`, `GET`, `PUT`, `PATCH`, `DELETE`, bulk operations, meta endpoints, reload behavior, and validation contracts are unchanged for existing clients.

3. **The canonical record model remains server-controlled.**
   Clients and agents may request an action, but the server determines whether the transition is declared and legal before any status update occurs.

4. **Direct field writes remain backward compatible.**
   In this first slice, existing `PUT`/`PATCH` behavior for the `status` field is not removed unless the repository explicitly chooses to tighten it in a later phase.

5. **Workflow metadata is optional and additive.**
   Absence of workflow metadata means "no action model declared," not an error.

6. **No hidden side effects are introduced.**
   The first slice updates only the configured status field plus normal record bookkeeping (`version`, `updated_at`).

7. **Entity discovery stays stable.**
   Existing `/api/meta` responses remain backward compatible; new workflow/action metadata is added as optional fields only.

## Explicit out-of-scope list

The first slice should **not** include any of the following:

- full agent orchestration,
- multi-step plans,
- autonomous write access outside declared actions,
- general rule engines,
- approvals/signatures execution,
- side effects such as notifications, webhooks, or integrations,
- action guards with an expression language,
- role/permission-aware action authorization,
- audit/event sourcing redesign,
- async job processing,
- UI redesign,
- a new framework or plugin system,
- storage architecture changes,
- replacing current CRUD with command-only APIs,
- migration of all existing entities to workflow metadata,
- blocking direct `status` updates through normal patch/update endpoints,
- redesign of the whole YAML/DSL grammar.

## Recommended implementation boundary

To keep the slice truly minimal, ship only this scenario first:

- one entity with a `status` enum,
- one declared action such as `submit`,
- one endpoint to request that action,
- one server-side validator/executor that updates status if valid,
- one meta extension exposing available actions.

If that works, Kalita will already demonstrate the intended future shape:

- agents propose,
- Kalita validates,
- Kalita controls transitions,
- the canonical model stays authoritative,
- current YAML/DSL remains intact for everything not using the new feature.
