# Second workflow-action slice review

## Verdict

**Approve with narrowing changes.**

The proposed direction is correct: introducing a persisted action-request artifact is the smallest meaningful step from proposal validation toward an execution-control layer. It is also mostly backward-compatible and additive as written. However, the current slice description is still slightly too wide in three places: list/discoverability surface, request-shape richness, and storage language that overstates durability for an in-memory implementation.

If those are narrowed, this becomes a safe second slice.

## Assessment against the slice goals

### 1. Is the slice minimal enough?
**Almost, but not fully.**

The core move is minimal:
- reuse existing proposal validation,
- persist one reviewable request object,
- expose a way to fetch it back,
- do not execute.

That is the right center of gravity for the next slice.

What makes it slightly larger than necessary:
- the doc allows both **get and list** semantics even though a single create-and-get loop is enough to prove the artifact exists;
- it suggests optional Meta/API discoverability updates, which are not required to validate the control-plane concept;
- the proposed stored object includes a fairly broad snapshot/metadata shape before the system has proven which fields are actually needed.

### 2. Does it preserve backward compatibility?
**Yes, if the existing proposal route remains untouched and submission stays on a new route.**

The proposal is good on the important boundaries:
- existing proposal-only behavior remains unchanged;
- CRUD semantics stay unchanged;
- no DSL change is required;
- submitting a request does not mutate the target business record.

The main compatibility caution is semantic, not mechanical: calling the artifact “durable” while also saying it can live in the current in-memory runtime may create expectations that survive process restarts. That should be described more carefully so clients do not infer persistence guarantees that the implementation does not actually provide.

### 3. Does it stay additive?
**Yes, with one caveat.**

New route + new internal object + no change to existing route behavior is additive.

The caveat is that optional Meta discoverability work and optional list APIs are additive in protocol terms but still widen the slice and increase surface area, tests, and future compatibility obligations. For this specific slice, additive is not the same as minimal.

### 4. Does it avoid premature approval/execution/policy complexity?
**Mostly yes.**

The proposal explicitly excludes:
- approval transitions,
- execution,
- auth redesign,
- policy/risk engines,
- side effects,
- orchestration abstractions.

That is exactly the right instinct.

The remaining risk is conceptual creep through the request model itself. Once the request object starts carrying reviewer-oriented fields, rich metadata, comments, queue semantics, or multiple request states, the slice stops being “persist validated intent” and starts becoming an approval system. The doc should guard against that more aggressively.

### 5. Does it introduce a durable artifact in the narrowest possible way?
**Not quite. It introduces the right artifact, but the artifact should be narrower and the durability wording should be tighter.**

The artifact that must exist in this slice is:
- a server-assigned request ID;
- a reference to the target entity/record/version;
- the requested action;
- the validated from/to preview captured at submission time;
- a minimal request state such as `pending`.

That is enough to prove the control-plane seam.

What is probably unnecessary in this slice:
- broad “proposal snapshot” storage if it duplicates the explicit action/from/to/version fields;
- submitter metadata unless it is already trivially available and already part of established request context plumbing;
- any implication that the artifact is durable beyond runtime lifetime when the same doc says storage may remain in memory.

### 6. Are the proposed APIs and storage assumptions bounded enough?
**Partially. They need a tighter lower bound.**

Bounded enough:
- a separate submission endpoint;
- reuse of existing validation logic;
- one retrieval path;
- in-memory storage for the slice.

Not bounded enough:
- the route section opens the door to both nested and global request resources without choosing one minimal shape;
- the read surface says “retrieve/list later,” but list is not needed to validate the slice;
- “durable” conflicts with “existing in-memory runtime store,” which blurs the actual guarantee.

For the smallest safe slice, the API/storage story should commit to:
- **one create route**;
- **one get-by-id route**;
- **one in-memory internal store with no restart-survival guarantee**.

## Narrowing changes required

### What to remove
1. **Remove list scope from this slice.**
   - Do not include request listing in scope, route planning, or tests.
   - Create + get-by-id is enough to prove persistence and inspectability.

2. **Remove optional Meta/API discoverability work from this slice.**
   - No `meta.go` changes.
   - No new discoverability hints.
   - The slice should stand on explicit routes only.

3. **Remove nonessential request fields from the required shape.**
   - Do not require broad proposal blobs if the same information can be represented by explicit minimal fields.
   - Do not require submitter metadata in this slice.
   - Do not introduce reviewer-facing or queue-facing fields.

### What to postpone
1. **Any request list/read collections.**
   - Postpone collection endpoints and filtering until there is a real consumer need.

2. **Any discoverability or meta exposure.**
   - Postpone until the request API is stable enough to advertise.

3. **Any stronger persistence story.**
   - Postpone SQL/schema/migration work and also postpone any restart-stable guarantee.

4. **Any richer request lifecycle.**
   - Postpone `approved`, `rejected`, `cancelled`, expiration, dedupe, comments, history, assignment, or queue semantics.

### What must stay in scope
1. **A new additive submission endpoint** that reuses the existing proposal validator and creates a request record.
2. **A minimal stored request artifact** containing only request ID, target entity/id/version, action, validated `from`/`to`, `pending` state, and timestamps.
3. **A single retrieval endpoint** for reading the stored request back by ID.
4. **Explicit non-mutation coverage** proving request submission does not alter the target business record.
5. **Explicit wording that storage is in-memory for now** and therefore not durable across restarts, even though it is a server-tracked artifact within a running instance.

## File/scope guidance

Keep the implementation surface limited to the files already closest to first-slice action handling:
- `internal/runtime/actions.go`: submission should call the same validation path already used for proposal handling.
- `internal/runtime/storage.go` or a focused `internal/runtime/action_requests.go`: hold the minimal request record and in-memory lookup helpers.
- `internal/http/actions.go` and `internal/http/router.go`: add one submit route and one get route only.
- Tests should stay limited to runtime and HTTP coverage for submit/get/non-mutation behavior.

Do **not** expand this slice into:
- `internal/http/meta.go`,
- broader workflow abstractions,
- policy/approval modules,
- persistent database plumbing.

## Top risks of over-expansion

1. **Turning a request record into an approval framework too early.**
   - Extra states, reviewers, comments, queues, and policy hooks would multiply scope immediately.

2. **Committing to API surface before usage is clear.**
   - List endpoints and discoverability hints create compatibility obligations that are unnecessary for this proof step.

3. **Overstating persistence guarantees.**
   - Calling the artifact durable while storing it only in memory risks a misleading contract and future migration pressure.

## Concise summary

The proposed second slice is directionally correct and close to the smallest safe next step, but it should be narrowed to a strict create-and-get request artifact. Keep the artifact minimal, keep storage explicitly in-memory for now, and do not pull list, meta, approval, or richer lifecycle concerns into this slice.

## Final verdict

**Approve with narrowing changes.**

## Top 3 risks

1. Approval/review complexity creeping into the request model.
2. Unnecessary API expansion through list and meta/discoverability work.
3. Misleading durability expectations from an in-memory-only implementation.
