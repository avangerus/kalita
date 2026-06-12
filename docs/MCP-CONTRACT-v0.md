# Kalita MCP Contract v0

Status: draft. The MCP server is a first-class agent interface (equal to the Web UI for humans). Transport: streamable HTTP (primary), stdio (local development).

## 0. Contract principles

1. **No anonymous agents.** Every call is authenticated with an agent key; agent = identity + role + scopes. Agents are registered by a human administrator.
2. **Unified permissions.** The MCP has no access model of its own — every call goes through the same Permission Engine as a human click. Permission downgrade = tool downgrade (the agent does not even see unavailable operations in `describe_system`).
3. **Every mutation requires `basis`** — a justification: a reference to a task, an automation rule, an ADR, or a human instruction. A mutation without a basis is rejected. The basis is written to the log (provenance).
4. **All calls are logged** with identity, arguments, and result.
5. **Errors are fuel for self-correction:** structured, with `fix_hint`, never free text.

## 1. Authentication and session

- `Authorization: Bearer <agent_key>` (Ed25519-derived token).
- The response to any call contains `def_version` (the SystemDefinition version) — the agent always knows which version of the system it is working against.
- Mutations accept `idempotency_key` (repeating a call is safe).
- Rate limits per agent: declared by the administrator; exceeding them returns `RATE_LIMITED` with `retry_after`.

## 2. Tools — Discovery (read-only)

### `describe_system()`
→ packs and versions, entities (names + brief schemas), workflows, roles, **my permissions** (what this identity can do), `def_version`. The first call any agent makes.

### `describe_entity(entity)`
→ full schema: fields, types, modifiers, workflow with transitions and guards, my permissions at the field/row level, ui-views.

### `get_grammar(format: "ebnf" | "json")`
→ machine-readable DSL grammar for the current version + canonical examples. For constrained decoding on the builder-agent side.

## 3. Tools — Data (runtime)

### `query(entity, filter?, sort?, limit?, cursor?)`
→ a page of records within the caller's permissions (row/field-level applied silently: what is not visible does not exist).

### `get_record(entity, id, with_journal?: bool)`

### `create_record(entity, values, basis, idempotency_key)`
### `update_record(entity, id, values, basis, idempotency_key, expected_updated_at?)`
Optimistic locking via `expected_updated_at` → `CONFLICT` on a race. No direct `delete` in v0 — deletion is modeled as a workflow state (rationale: data does not disappear; the event store is complete).

### `act(entity, id, action, args?, basis, idempotency_key)`
Execute a workflow transition. If the transition `requires approval` — returns `{status: "pending_approval", approval_id}`: **the agent cannot wait and cannot accelerate**; the signature will come from a human, and the task will continue on an event.

### `read_journal(scope: record|entity|system, id?, since?, limit?)`
Events within the caller's read permissions.

## 4. Tools — Tasks (agent as worker)

### `list_my_tasks(status?: open|taken)`
→ tasks assigned to the role of this identity (workflow steps, automation assignments) with the full record context.

### `take_task(task_id)` → exclusive lease with TTL; if expired — task returned to the pool.
### `complete_task(task_id, result, basis)`
### `fail_task(task_id, reason)` — an honest refusal is better than silent hanging; written to the log; impacts rating less than a TTL expiry failure.
### `report_progress(task_id, note)` — a note is attached to the task; the core cross-checks it against actual events on the task (anti-embellishment: a report with no backing events is flagged).

## 5. Tools — System changes (agent as builder)

### `validate_dsl(files: {path: text})` — dry-run
→ `{ok}` or `{errors: [{code, file, line, message, fix_hint}]}`. Requires no permissions — this is the compiler. Self-correction loop: generate → validate → fix → repeat, with no human involvement and no traces in the system.

### `propose_change(diff, base_def_version, description, basis)`
→ validation; on `ok` → `{proposal_id, migration_plan, status: "pending_approval"}` — enters the human signing queue. `base_def_version` is mandatory: a proposal against a stale version = `STALE_BASE`; the agent must re-read the system.

### `get_proposal(proposal_id)` → status: `pending | approved | rejected {reason} | applied`.
Rejection with a reason is a training signal for the agent; reasons are written by the human in the signing queue.

## 6. Error model (closed list v0)

| Code | Semantics |
|---|---|
| `PERMISSION_DENIED` | + which rule denied it (name from permissions) |
| `VALIDATION_ERROR` | + field, constraint, fix_hint |
| `GUARD_FAILED` | transition is impossible: + which guard and the current values |
| `PENDING_APPROVAL` | action created a signing request |
| `CONFLICT` / `STALE_BASE` | record version / definition version race |
| `RATE_LIMITED` | + retry_after |
| `BASIS_REQUIRED` | mutation without a basis |

Principle: an error always contains enough information for the agent to fix itself **without a human** — except for `PERMISSION_DENIED` and `PENDING_APPROVAL`, which by design can only be resolved by a human.

## 7. What is absent from v0 (reserved)

`simulate_change` (layer 2), `subscribe` (push events to agents — in v0 polling via `list_my_tasks`), cross-node calls (layer 3), identity management via MCP (human admin via UI only), `search_perimeter` (RAG search over corporate data via KnowVault — see KNOWVAULT-INTEGRATION.md).

## 8. Contract acceptance scenario

A single agent (Claude Code + this MCP) must, touching nothing outside the tools: (1) read the grammar, (2) propose the "requests" pack as a diff, (3) after a human signs, create 5 records, (4) advance one through the workflow to a HITL transition, (5) receive a structured permission-denied error on a forbidden operation, (6) report on a task. This is the end-to-end test for MVP week 8.
