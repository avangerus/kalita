# Second minimal vertical slice: reviewable workflow action requests

## Proposed next slice
The next smallest meaningful slice should add **submission and persistence of a workflow action request** after proposal validation, without executing the action.

In concrete terms:
- the existing workflow action endpoint continues to support **proposal-only validation**;
- a new additive endpoint lets a client say **"submit this validated action proposal for review"**;
- Kalita stores a small immutable request record that captures the requested action, the target record/version, the computed transition preview, and basic submission metadata;
- the requested business record is **not** mutated, and no committed transition path is introduced yet.

This is the narrowest step from "validate an action" toward an actual **execution-control layer** because it adds a durable control object between intent and execution.

---

## New capability introduced
This slice introduces one new capability:

### A durable execution-intent envelope for workflow actions
Today, Kalita can validate that:
- an action exists,
- the action is allowed from the current status,
- the supplied `record_version` matches,
- and the system can compute the `from` → `to` proposal preview.

With the second slice, Kalita would also be able to:
- accept a request to **submit** that proposal,
- persist a reviewable record of the request,
- assign it a stable request ID,
- expose its current review state (initially just something like `pending`),
- and let clients retrieve/list those requests later.

That is a meaningful control-plane capability because it turns a transient HTTP validation result into a **server-tracked execution request**.

What it still does **not** do:
- no approval engine,
- no execution of the action,
- no mutation of the target business record,
- no role-based authorization redesign,
- no generalized agent framework.

---

## Why this is the best next step
This is the best next step because it advances the architecture by exactly one layer without widening the safety boundary.

### 1. It builds directly on the first slice instead of bypassing it
The first slice established:
- workflow action definitions in DSL,
- schema validation/linting,
- an HTTP action endpoint,
- and proposal-only execution.

A submission/review-request slice reuses all of that. The new flow would be:
1. validate the action exactly as today,
2. snapshot the validated proposal,
3. store it as a pending request.

That is a direct continuation, not a redesign.

### 2. It moves toward execution control without enabling execution yet
An execution-control layer needs a place where the system can say:
- *this action was requested*,
- *this is what was evaluated*,
- *this is waiting for a later decision*.

Persisting a request object creates that seam. It is the minimal predecessor to any future:
- review,
- approval,
- audit,
- or explicit execute-approved-action behavior.

### 3. It stays safer than adding commit mode next
Adding immediate committed execution would widen risk quickly:
- more concurrency questions,
- stronger write guarantees,
- rollback/error semantics,
- future authorization coupling,
- ambiguity about whether actions and CRUD should share exactly the same mutation pipeline.

By contrast, storing a request record is bounded and reversible in architectural impact. It creates control-plane structure first, before allowing controlled execution.

### 4. It creates a durable API contract that future slices can reuse
If a later slice adds approval or execution, it can operate on a stored request ID rather than on a one-shot HTTP call. That is a cleaner and safer progression than jumping straight from proposal preview to mutation.

### 5. It remains backward-compatible
The existing proposal endpoint can remain unchanged. Existing clients that only validate actions do not need to change. The new behavior can be entirely additive.

---

## Exact scope
The second slice should be intentionally narrow.

### In scope
1. **A new additive request type for workflow action submissions**
   - A new internal model such as `WorkflowActionRequest` or `ActionSubmission`.
   - It stores:
     - request ID,
     - entity FQN,
     - target record ID,
     - target `record_version`,
     - action name,
     - status field,
     - `from` and `to` states,
     - proposal snapshot,
     - request state (`pending` only in this slice),
     - timestamps,
     - optional minimal submitter metadata if already available in request context.

2. **A new additive HTTP submission endpoint**
   Conceptually something like:
   - `POST /api/:module/:entity/:id/_actions/:action/requests`

   Behavior:
   - validates the workflow action using the same runtime logic as the proposal endpoint;
   - creates and stores a pending action request;
   - returns the stored request document.

3. **A small read surface for the stored request**
   At minimum one of:
   - `GET /api/_action_requests/:id`, or
   - `GET /api/:module/:entity/:id/_action_requests/:request_id`

   The exact route can follow Kalita's current API style, but the slice should include at least one retrieval path so the persisted object is testable and inspectable.

4. **Meta/API discoverability updates where useful**
   If the Meta API already exposes workflow actions, it may add an optional hint that an entity supports request submission in addition to proposal validation. This should be additive only.

5. **Tests for persistence and non-mutation guarantees**
   The slice must prove that creating a request does not change the target record.

### Explicitly out of scope within this slice
- approving a request,
- rejecting a request via workflow logic,
- executing a pending request,
- expiring requests,
- deduplicating semantically identical requests,
- policy/risk scoring,
- human/agent auth model changes,
- side effects or notifications,
- generic command bus/orchestrator abstractions,
- migrations to durable SQL-backed storage.

---

## Affected modules/files
This slice should remain close to the modules already involved in the first slice.

### Runtime / storage
- `internal/runtime/actions.go`
  - keep the existing proposal validator as the source of truth for action legality.
  - add a small helper that converts a valid proposal into a persisted request object.

- `internal/runtime/storage.go` or the runtime storage file that currently owns in-memory state
  - add an in-memory collection for action requests.
  - add ID generation and lookup helpers.

- **Possible new file:** `internal/runtime/action_requests.go`
  - define the request model and request-store helpers.
  - keep request persistence logic separate from HTTP concerns.

### HTTP
- `internal/http/actions.go`
  - keep the current proposal endpoint unchanged.
  - add a new handler for submission.

- `internal/http/router.go`
  - register the new submission and retrieval routes.

- **Possible new file:** `internal/http/action_requests.go`
  - add focused handlers for create/get/list request resources.

### Schema / metadata
- likely **no DSL grammar changes required** for this slice.
- possibly `internal/http/meta.go` if optional discoverability fields are added.

That is an important part of why this is the right next step: the second slice can advance the execution-control boundary **without expanding the DSL again**.

### Tests
- `internal/runtime/actions_test.go`
  - keep proposal tests intact.
  - add submission persistence and non-mutation coverage.

- `internal/http/actions_test.go`
  - add endpoint coverage for submit-and-read request flows.

If existing test files become crowded, a new `internal/http/action_requests_test.go` would be reasonable.

---

## Compatibility guarantees
The slice should commit to the following guarantees.

### 1. No change to existing proposal behavior
`POST /api/:module/:entity/:id/_actions/:action` remains proposal-only.
- It still validates and previews.
- It still does not mutate the record.
- It still does not create a request implicitly.

### 2. No DSL changes required
Entities that already declare workflow actions should automatically support request submission.
- No new required YAML/DSL syntax.
- No schema migration burden for current models.

### 3. Existing CRUD behavior remains unchanged
Create/read/update/patch/delete semantics remain exactly as they are today.
- Submitting an action request does not alter CRUD contracts.
- Direct CRUD writes are not blocked by the existence of pending requests in this slice.

### 4. Target records are never mutated by request submission
A successful request submission:
- does not update the target record status,
- does not increment the target record version,
- does not alter unrelated fields,
- does not change existing action-proposal semantics.

### 5. New API surface is additive only
Clients that do not know about request submission can ignore it safely.
- No current endpoint is removed.
- No current response contract is narrowed.

### 6. Request persistence is internal-state additive, not a storage redesign
The slice may store requests in the existing in-memory runtime store for now.
- No commitment to long-term durable persistence yet.
- No immediate dependency on PostgreSQL or schema migrations.

---

## Test plan
The second slice should be testable with a compact, explicit matrix.

### Runtime tests
1. **Submit valid request from a valid proposal**
   - given a record in `Draft`,
   - when `submit` is requested with the matching `record_version`,
   - then an action request is created with state `pending` and the correct `from`/`to` snapshot.

2. **Submission reuses proposal validation rules**
   - unknown action is rejected,
   - disallowed source state is rejected,
   - stale `record_version` is rejected.

3. **Submission does not mutate target record**
   - record status remains unchanged,
   - record version remains unchanged,
   - timestamps on the target record remain unchanged.

4. **Stored request is immutable enough for the slice contract**
   - proposal snapshot in the request remains the computed snapshot from submission time.

5. **Retrieval returns the same stored request**
   - get by request ID returns the expected request payload.

### HTTP integration tests
6. **POST submission route returns created request**
   - returns 201 or the chosen success status,
   - includes request ID and `pending` state,
   - includes target entity/id/action/from/to snapshot.

7. **GET request route returns the stored request**
   - request can be fetched after creation.

8. **Invalid submission returns same error shape as proposal validation**
   - version conflict,
   - unknown action,
   - invalid state.

9. **Proposal route remains unchanged**
   - existing first-slice tests continue to pass unchanged.

10. **Submission does not change underlying record fetch result**
   - reading the original record after submission shows the original status/version.

### Optional meta tests
11. **Meta output stays backward compatible**
   - if request-submission hints are added, old fields remain unchanged and new fields are optional.

---

## Risks
This slice is intentionally low-risk, but there are still a few design hazards to contain.

### 1. Accidental drift between proposal and submission validation
If proposal and submission use separate validation paths, they may diverge.

**Mitigation:**
- submission must call the same runtime action-validation helper already used by the proposal endpoint.
- avoid re-encoding action legality in the HTTP layer.

### 2. Implicitly creating a review/approval model that is too broad
Once requests are persisted, it is tempting to add reviewers, approvals, comments, queues, and policies immediately.

**Mitigation:**
- keep request state minimal, ideally just `pending` in this slice.
- do not add approval transitions yet.

### 3. Overcommitting to storage shape too early
A rich request model or relational design would make the slice larger than necessary.

**Mitigation:**
- store only the fields needed to preserve the validated action intent and preview.
- keep storage in-memory and internal for now.

### 4. Confusion between "request created" and "action executed"
API consumers may misread a successful submission as a committed state change.

**Mitigation:**
- response fields should clearly distinguish request state from execution state.
- include explicit markers such as `state: pending` and `committed: false` or equivalent semantics.

### 5. Interaction with future direct CRUD writes
If the target record changes after a request is submitted, the stored request may become stale.

**Mitigation:**
- treat staleness handling as a later execution-time concern.
- preserve the original target `record_version` in the request so future slices can detect drift.

---

## Why this slice remains bounded
This slice is bounded because it introduces only one new durable artifact: **the action request record**.

It does **not** also introduce:
- execution,
- approval workflows,
- authorization policy engines,
- side effects,
- DSL redesign,
- background processing,
- or broad runtime refactoring.

The implementation can remain small if it follows this boundary:
- reuse the existing action proposal validator,
- persist the resulting proposal as a pending request,
- expose create/get APIs,
- test non-mutation and retrieval.

That is a true vertical slice, but still a small one.

---

## Explicit out-of-scope list
To keep the second slice minimal, the following should be explicitly excluded:
- executing approved requests,
- commit flags on the current action route,
- approval roles or reviewer assignment,
- generic policy DSL or risk engine,
- notifications/webhooks,
- request comments/history threads,
- bulk request submission,
- cancellation semantics beyond simple deletion/no-op handling,
- record locking,
- idempotency-key infrastructure,
- persistence beyond the current in-memory runtime,
- any redesign of CRUD, workflow DSL, or handler architecture beyond what is required for this feature.

---

## Concise summary
The next minimal vertical slice should add **reviewable workflow action requests**:
- keep the current action route proposal-only,
- add a new additive endpoint that persists a validated action proposal as a `pending` request,
- expose that request for later retrieval,
- and still do **no execution**.

## Why this slice is safe
It is safe because it does not widen the mutation boundary:
- the target record is still not changed,
- existing CRUD and proposal semantics stay intact,
- the new behavior is additive,
- and all validation continues to flow through the same workflow-action rules already introduced in the first slice.

## Why it is not too big
It is not too big because it adds only one new concept: a persisted action-request object.
It deliberately avoids approvals, execution, policy engines, side effects, storage redesign, and DSL expansion. That keeps the slice narrow, testable, and directly connected to the first workflow-action slice.
