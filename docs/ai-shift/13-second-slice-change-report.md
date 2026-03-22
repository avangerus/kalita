# Second slice change report

## What changed
- Added a minimal in-memory `WorkflowActionRequest` model and runtime helpers to create a pending request from the existing validated workflow proposal path and retrieve it by request ID.
- Added one additive HTTP creation route for workflow action requests and one get-by-id route for stored requests.
- Reused the existing workflow action request body parsing and validation behavior so proposal validation rules remain the single source of truth.
- Added focused runtime and HTTP tests for create, get, validation reuse, non-mutation, and proposal endpoint backward compatibility.

## What stayed compatible
- `POST /api/:module/:entity/:id/_actions/:action` remains proposal-only and does not create requests implicitly.
- Workflow request creation still does not mutate the target record, update its status, or increment record version.
- No DSL, meta, CRUD, or persistence-contract changes were introduced.

## What is intentionally not implemented
- No approval, rejection, assignment, comments, or richer request lifecycle.
- No execution or commit behavior for pending requests.
- No list endpoints, discoverability/meta expansion, or storage redesign.
- No persistence beyond the current in-memory runtime instance.

## Residual risks
- Request artifacts are lost on server restart because storage is runtime-memory only.
- The global get-by-id route is intentionally narrow; any future scoping or auth concerns remain unresolved for later slices.
- Future execution slices will need to decide whether request replay should revalidate against current record state or rely on captured request data.

## Next smallest step
- Add a single explicit execute-pending-request path that revalidates version/state at execution time while keeping approval and list/discovery concerns out of scope.
