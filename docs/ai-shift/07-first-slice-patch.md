# First workflow-action slice patch

## What was fixed
- The workflow action route no longer allows blind writes: `record_version` is now required for action requests.
- The action request contract no longer silently accepts unsupported fields. Requests with ignored fields such as `payload` are rejected.
- The workflow action executor was narrowed to a validated proposal response for this slice instead of committing the transition.

## What behavior was narrowed
- `POST /api/:module/:entity/:id/_actions/:action` now validates the requested transition and returns a proposal preview only.
- The response still describes `from`, `to`, and the proposed record shape, but the stored record is not mutated, the stored version is not incremented, and `committed` is always `false` in this slice.
- Requests must include a positive `record_version`; omitting it now fails fast.

## What remains for later
- A deliberate design for committed workflow transitions, including whether that is a flag on the same route or a separate route.
- A consistent shared write-concurrency contract across action and CRUD APIs, including possible `If-Match` support on the action route.
- Any real use of structured action payload/audit fields once the platform is ready to persist and validate them.
- Authorization and approval semantics for workflow actions.

## Residual risks
- The action route is now intentionally narrower than the previously shipped implementation, so clients that relied on immediate mutation must switch to the bounded proposal-only behavior.
- Non-inline-enum workflow status validation remains a separate follow-up concern for a later slice.
- This patch does not redesign the workflow architecture; it only removes the highest-risk behavior from the current slice.
