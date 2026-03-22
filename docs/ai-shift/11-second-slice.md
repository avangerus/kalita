# Second minimal vertical slice: persisted workflow action requests

## Proposed next slice
The next smallest meaningful slice should add **create-and-retrieve of a validated workflow action request** after proposal validation, without executing the action and without introducing approval workflow behavior.

In concrete terms:
- the existing workflow action endpoint continues to support **proposal-only validation**;
- a new additive endpoint lets a client say **"create a request from this validated proposal"**;
- Kalita stores a **minimal server-tracked request artifact** that captures the validated action intent and preview;
- a client can retrieve that stored request by ID;
- the requested business record is **not** mutated, and no approval or execution path is introduced yet.

This is the narrowest step from "validate an action" toward an execution-control layer because it adds exactly one persisted-in-runtime control object between intent and any later execution work.

---

## New capability introduced
This slice introduces one new capability:

### A minimal persisted request artifact for workflow actions
Today, Kalita can validate that:
- an action exists,
- the action is allowed from the current status,
- the supplied `record_version` matches,
- and the system can compute the `from` → `to` proposal preview.

With the second slice, Kalita would also be able to:
- accept a request to **create** a stored action request from that validated proposal,
- assign it a stable request ID within the running server instance,
- persist the request in the current in-memory runtime store,
- and let clients **retrieve that request by ID**.

That is the entire control-plane gain for this slice: turning a transient HTTP validation result into a **server-tracked request artifact** that exists beyond the immediate request/response cycle of a single API call.

What it still does **not** do:
- no approval engine,
- no review workflow,
- no execution of the action,
- no mutation of the target business record,
- no role-based authorization redesign,
- no list/discovery/meta expansion,
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

A request-creation slice reuses all of that. The new flow would be:
1. validate the action exactly as today,
2. capture the validated action/from/to/version result,
3. store it as a pending request,
4. retrieve it later by request ID.

That is a direct continuation, not a redesign.

### 2. It moves toward execution control without enabling review or execution yet
An execution-control layer needs a place where the system can say:
- *this action was requested*,
- *this is what was validated at request time*,
- *this request now has a stable server-side identifier*.

Persisting a request object creates that seam. It is the minimal predecessor to any future:
- approval,
- audit expansion,
- or explicit execute-request behavior.

### 3. It stays safer than adding commit mode next
Adding immediate committed execution would widen risk quickly:
- more concurrency questions,
- stronger write guarantees,
- rollback/error semantics,
- future authorization coupling,
- ambiguity about whether actions and CRUD should share exactly the same mutation pipeline.

By contrast, storing a minimal request record is bounded and reversible in architectural impact. It creates control-plane structure first, before allowing controlled execution.

### 4. It creates a reusable seam without expanding API surface prematurely
A later slice can build approval, execution, or list/discoverability behavior on top of a stored request ID. This slice does not need to expose those broader surfaces yet. It only needs the smallest proof that a validated action proposal can become a retrievable request artifact.

### 5. It remains backward-compatible
The existing proposal endpoint can remain unchanged. Existing clients that only validate actions do not need to change. The new behavior can be entirely additive.

---

## Exact scope
The second slice should be intentionally narrow.

### In scope
1. **A minimal request artifact for validated workflow action submissions**
   - A new internal model such as `WorkflowActionRequest` or `ActionRequest`.
   - It stores only the fields required for this slice:
     - request ID,
     - entity FQN,
     - target record ID,
     - target `record_version`,
     - action name,
     - validated `from` state,
     - validated `to` state,
     - request state (`pending` only in this slice),
     - created/updated timestamps.
   - No reviewer metadata, queue metadata, comments, or broad proposal blobs are required.

2. **A new additive HTTP creation endpoint**
   Conceptually something like:
   - `POST /api/:module/:entity/:id/_actions/:action/requests`

   Behavior:
   - validates the workflow action using the same runtime logic as the proposal endpoint;
   - creates and stores a pending action request;
   - returns the stored request document.

3. **A single read path for the stored request**
   At minimum one route such as:
   - `GET /api/_action_requests/:id`, or
   - `GET /api/:module/:entity/:id/_action_requests/:request_id`

   The exact route can follow Kalita's current API style, but the slice should include **one get-by-id retrieval path only** so the persisted object is testable and inspectable.

4. **Tests for persistence-in-runtime and non-mutation guarantees**
   The slice must prove that creating a request:
   - stores the request artifact in the running instance,
   - does not change the target record,
   - and allows retrieval of the same request by ID.

### Explicitly out of scope within this slice
- approving a request,
- rejecting a request,
- reviewer assignment,
- execution of a pending request,
- expiring requests,
- deduplicating semantically identical requests,
- policy/risk scoring,
- human/agent auth model changes,
- side effects or notifications,
- generic command bus/orchestrator abstractions,
- request collection/list endpoints,
- meta/discoverability exposure,
- migrations to durable SQL-backed storage,
- any restart-stable persistence guarantee.

---

## Affected modules/files
This slice should remain close to the modules already involved in the first slice.

### Runtime / storage
- `internal/runtime/actions.go`
  - keep the existing proposal validator as the source of truth for action legality;
  - add a small helper that converts a valid proposal into a minimal stored request object.

- `internal/runtime/storage.go` or the runtime storage file that currently owns in-memory state
  - add an in-memory collection for action requests;
  - add ID generation and lookup helpers.

- **Possible new file:** `internal/runtime/action_requests.go`
  - define the minimal request model and request-store helpers;
  - keep request persistence logic separate from HTTP concerns.

### HTTP
- `internal/http/actions.go`
  - keep the current proposal endpoint unchanged;
  - add a new handler for request creation.

- `internal/http/router.go`
  - register the new create route and one get route.

- **Possible new file:** `internal/http/action_requests.go`
  - add focused handlers for create and get request resources.

### Schema / metadata
- **no DSL grammar changes required** for this slice;
- **no Meta API changes required** for this slice.

That is an important part of why this is the right next step: the second slice can advance the execution-control boundary **without expanding the DSL or meta surface again**.

### Tests
- `internal/runtime/actions_test.go`
  - keep proposal tests intact;
  - add request creation, retrieval, and non-mutation coverage.

- `internal/http/actions_test.go`
  - add endpoint coverage for create-and-get request flows.

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
Entities that already declare workflow actions should automatically support request creation.
- No new required YAML/DSL syntax.
- No schema migration burden for current models.

### 3. Existing CRUD behavior remains unchanged
Create/read/update/patch/delete semantics remain exactly as they are today.
- Creating an action request does not alter CRUD contracts.
- Direct CRUD writes are not blocked by the existence of pending requests in this slice.

### 4. Target records are never mutated by request creation
A successful request creation:
- does not update the target record status,
- does not increment the target record version,
- does not alter unrelated fields,
- does not change existing action-proposal semantics.

### 5. New API surface is additive only
Clients that do not know about request creation can ignore it safely.
- No current endpoint is removed.
- No current response contract is narrowed.

### 6. Persistence expectations are explicitly limited
The request artifact is persisted only in the current in-memory runtime store for this slice.
- It is server-tracked within a running instance.
- It is **not** guaranteed to survive process restarts.
- It does **not** imply SQL-backed durability, migrations, or cross-instance persistence.

---

## Test plan
The second slice should be testable with a compact, explicit matrix.

### Runtime tests
1. **Create valid request from a valid proposal**
   - given a record in `Draft`,
   - when request creation is submitted with the matching `record_version`,
   - then an action request is created with state `pending` and the correct `from`/`to` snapshot.

2. **Request creation reuses proposal validation rules**
   - unknown action is rejected,
   - disallowed source state is rejected,
   - stale `record_version` is rejected.

3. **Request creation does not mutate target record**
   - record status remains unchanged,
   - record version remains unchanged,
   - timestamps on the target record remain unchanged.

4. **Retrieval returns the same stored request**
   - get by request ID returns the expected request payload.

5. **Stored request reflects the validated submission-time preview**
   - the stored `from`, `to`, action, and `record_version` match the values computed during creation.

### HTTP integration tests
6. **POST creation route returns created request**
   - returns 201 or the chosen success status,
   - includes request ID and `pending` state,
   - includes target entity/id/action/from/to fields.

7. **GET request route returns the stored request**
   - request can be fetched after creation.

8. **Invalid creation returns the same error shape as proposal validation**
   - version conflict,
   - unknown action,
   - invalid state.

9. **Proposal route remains unchanged**
   - existing first-slice tests continue to pass unchanged.

10. **Request creation does not change underlying record fetch result**
   - reading the original record after request creation shows the original status/version.

---

## Risks
This slice is intentionally low-risk, but there are still a few design hazards to contain.

### 1. Accidental drift between proposal and request-creation validation
If proposal and request creation use separate validation paths, they may diverge.

**Mitigation:**
- request creation must call the same runtime action-validation helper already used by the proposal endpoint;
- avoid re-encoding action legality in the HTTP layer.

### 2. Implicitly creating an approval model that is too broad
Once requests are persisted, it is tempting to add reviewers, approvals, comments, queues, and policies immediately.

**Mitigation:**
- keep request state minimal, ideally just `pending` in this slice;
- do not add approval transitions;
- do not add reviewer-oriented fields.

### 3. Overcommitting to storage shape too early
A rich request model or relational design would make the slice larger than necessary.

**Mitigation:**
- store only the fields needed to preserve validated action intent and preview;
- keep storage in-memory and internal for now.

### 4. Confusion between "request created" and "action executed"
API consumers may misread a successful request creation as a committed state change.

**Mitigation:**
- response fields should clearly distinguish request state from execution state;
- document that request creation does not mutate the target record;
- keep request state as `pending` only.

### 5. Misleading durability expectations
Calling the artifact durable while storing it only in memory risks a misleading contract.

**Mitigation:**
- describe the artifact as persisted in the running server instance, not restart-durable;
- state explicitly that restart survival is out of scope for this slice.

---

## Why this slice remains bounded
This slice is bounded because it introduces only one new persisted-in-runtime artifact: **the action request record**.

It does **not** also introduce:
- approval workflows,
- execution,
- authorization policy engines,
- side effects,
- DSL redesign,
- background processing,
- list/read collections,
- meta/discoverability expansion,
- or broad runtime refactoring.

The implementation can remain small if it follows this boundary:
- reuse the existing action proposal validator,
- persist the resulting validated action intent as a pending request,
- expose create/get APIs only,
- test non-mutation and retrieval,
- and make in-memory persistence expectations explicit.

That is a true vertical slice, but still a small one.

---

## Exact items removed from scope
- request list endpoints or collection reads,
- meta/API discoverability updates,
- reviewer metadata,
- submitter metadata as a required field,
- broad proposal snapshot blobs beyond minimal explicit fields,
- approval/rejection transitions,
- comments, queues, assignment, or history,
- any claim of restart-stable durability,
- storage/database migration work.

## Exact items kept in scope
- one additive request-creation endpoint,
- reuse of the existing proposal validator,
- one minimal in-memory request artifact,
- one get-by-id retrieval endpoint,
- explicit non-mutation guarantees for the target record,
- tests covering create, get, validation reuse, and non-mutation.

## Exact items postponed
- list/filter/search request APIs,
- discoverability/meta exposure,
- richer request lifecycle states,
- approval/review logic,
- execution of pending requests,
- dedupe/idempotency infrastructure,
- notifications and policy hooks,
- SQL-backed or restart-stable persistence.

---

## Concise summary
The next minimal vertical slice should add **create-and-get workflow action requests**:
- keep the current action route proposal-only,
- add a new additive endpoint that stores a validated action proposal as a minimal `pending` request,
- expose that request by ID,
- and still do **no approval and no execution**.

## Why this slice is safe
It is safe because it does not widen the mutation boundary:
- the target record is still not changed,
- existing CRUD and proposal semantics stay intact,
- the new behavior is additive,
- request state remains minimal,
- and persistence expectations are explicit: stored in memory for the running instance only.

## Why it is not too big
It is not too big because it adds only one new concept: a minimal server-tracked request artifact.
It deliberately avoids approvals, execution, list APIs, discoverability work, policy engines, side effects, storage redesign, and DSL expansion. That keeps the slice narrow, testable, and directly connected to the first workflow-action slice.
