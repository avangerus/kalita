# Kalita Event Store v0

Status: draft. This is the "irreversible day-1 decision" (HLD §8): the log is the single source of truth; everything else is a projection. Changing this model retroactively is impossible, so it is fixed before the first line of code.

## 0. Principles

1. **Append-only.** The application role in PostgreSQL has no UPDATE/DELETE rights on the events table — physically.
2. **Everything is an event.** Data changes, workflow transitions, DSL proposals/signatures/applications, task take/complete, agent registration. If it is not in the log — it did not happen.
3. **Projections are derivable and discardable.** The current state of any entity is rebuilt by replaying from scratch; bit-for-bit identity is the acceptance test.
4. **Signature and hash chain** — evidentiary force of the log (layers 2–3: reputation, insurability).
5. **Determinism for future simulation:** all values computed at the time of the event (`now()`, guard inputs) are fixed in the payload — replay does not recompute the world.

## 1. Event schema

```
event_id        uuid v7 (time in id)
seq             bigint, monotonic without gaps (per node)
ts              timestamptz
actor           {type: human|agent|system, id, role}
kind            see taxonomy
subject         {entity, record_id} | {proposal_id} | {task_id} | {actor_id}
payload         jsonb
basis           {type: task|rule|adr|human|approval, id}   — provenance
def_version     SystemDefinition version at the time of the event
idempotency_key nullable
prev_hash       bytea
hash            bytea = SHA-256(prev_hash || canonical_json(everything above))
signature       bytea nullable (Ed25519/WebAuthn, see §4)
```

`payload` for `record.updated` — **diff format**: `[{field, old, new}]`, not a snapshot. The log is human-readable ("who changed what") and feeds drift analytics (layer 2) without parsing snapshots.

## 2. Kind taxonomy (closed list v0)

| Group | Events |
|---|---|
| data | `record.created`, `record.updated`, `record.action` (workflow transition: + action, from, to, guard_inputs) |
| definition | `definition.proposed`, `.validated`, `.approved`, `.rejected`, `.applied`, `.reverted` |
| approval | `approval.requested`, `.granted`, `.rejected` (+reason) |
| task | `task.created`, `.taken`, `.progress`, `.completed`, `.failed`, `.expired` |
| actor | `actor.registered`, `.role_changed`, `.key_rotated`, `.disabled` |
| system | `node.started`, `projection.rebuilt`, `checkpoint.sealed` |

New kinds are added only additively (like an enum in the DSL).

## 3. Integrity: chain and checkpoints

- The `hash` of each event includes `prev_hash` → substituting or deleting a middle event breaks the chain (tamper-evident).
- Every N events (and once daily) — `checkpoint.sealed`: the node signs the chain head with the node key. A checkpoint can be exported externally (client backup, later — external anchoring) — proof that "the log has not been rewritten retroactively".

## 4. Signatures (who signs with what)

| Subject | Mechanism | What is signed (mandatory) |
|---|---|---|
| Human | **WebAuthn/passkey** (key on the user's device, not on the server) | `approval.granted/.rejected`, `definition.approved` — the assertion is stored in `signature` |
| Agent | Ed25519 agent key (client-side request signature) | all mutations; the core verifies and stores the signature in the event |
| Node | node key | checkpoints |

Decision: no server-side storage of human keys — otherwise the signature has no evidentiary force ("the server could have done it itself"). The MVP includes WebAuthn from day 1: it is cheap (standard libraries) and cannot be retrofitted later.

## 5. Storage and projections

- PostgreSQL, `events` table, partitioned by month; indexes: `(seq)`, `(subject)`, `(actor.id, ts)`, `(kind, ts)`. SMB node load profile — 10–100k events/month: trivial.
- **Projections are updated in the same transaction as the event append** (single-node MVP: no eventual consistency, no outbox — you read exactly what you wrote). Async replay — only for rebuilding/recovery.
- Projections v0: current-state tables per entity (jsonb + extracted index columns), task queue, signing queue, definition versions.
- Files (`file` fields) — content-addressed storage alongside; the event holds the content hash.

## 6. Retention and "right to be forgotten"

- By default the log is permanent — that is the point of the system.
- The conflict with GDPR/152-FZ is resolved via the **crypto-shredding** pattern (reserved, not in MVP): sensitive payloads are encrypted with a per-subject key; destroying the key renders the data unreadable while the hash chain remains intact. In the v0 schema there is already a slot for this: payload allows `{encrypted: true, key_id}`.

## 7. Event Store acceptance tests

1. **Replay:** run 100k mixed events → drop projections → rebuild → state is identical bit-for-bit.
2. **Tamper:** modify any event in the middle directly in the DB → chain verification finds the break and points to the location.
3. **Signature:** an approval signed with WebAuthn is verified by an offline utility without server access (evidentiary force).
4. **Idempotency:** repeating a mutation with the same `idempotency_key` does not create a second event.
