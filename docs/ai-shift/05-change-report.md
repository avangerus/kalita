# Change report: first workflow-action vertical slice

## What changed
- Added an optional additive `workflow:` entity block to the DSL schema model with:
  - `status_field`
  - `actions`
  - per-action `from` and `to` states
- Extended the DSL parser to read the new workflow block without changing existing field or constraints syntax.
- Extended schema linting to validate:
  - the workflow status field exists
  - action `from`/`to` states match enum values when the status field is an inline enum
  - action declarations are structurally complete
- Added a focused runtime workflow-action executor that:
  - loads an existing record
  - verifies optimistic version when provided
  - validates the requested action against declared workflow metadata
  - updates only the configured status field
  - increments version and updates timestamp
- Added a new additive endpoint:
  - `POST /api/:module/:entity/:id/_actions/:action`
- Extended entity meta output to expose declared workflow metadata so clients can discover actions.
- Added one minimal example entity with workflow metadata: `test.WorkflowTask`.
- Added focused parser/lint/runtime/HTTP tests for the new slice.

## What stayed compatible
- Existing DSL files still load without modification.
- Existing YAML enum catalog format is unchanged.
- Existing CRUD routes and behavior remain available.
- Entities without workflow metadata remain pass-through and unaffected.
- Direct `PUT`/`PATCH` writes to `status` are still allowed exactly as before.
- No major modules were renamed and no architecture was redesigned.

## Remaining risks
- Workflow state validation currently checks declared enum states only when the configured status field is an inline `enum[...]`; catalog-backed status fields are not yet cross-validated against YAML catalog values.
- The action endpoint currently executes the transition immediately once validated; it does not yet support proposal-only/dry-run mode or review gating.
- Workflow metadata is supported only in the DSL parser path used by this repository’s current schema format; no separate sidecar workflow YAML has been introduced in this slice.
- No authorization, approval, or side-effect model is included yet by design.

## Next smallest step
- Add a non-mutating proposal/dry-run mode for the same action endpoint or a sibling endpoint so clients can ask Kalita to validate a transition before execution, while reusing the same workflow metadata and transition validator.
