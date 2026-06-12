# Kalita MVP — 8-Week Backlog

Status: draft. Foundation: HLD.md, DSL-SPEC-v0.md, MCP-CONTRACT-v0.md, EVENT-STORE-v0.md.
Working mode: agent pipeline; a spec with acceptance criteria precedes code for each epic; architectural deviations from the four foundation documents — only via ADR.

## Week 1 — Foundation: Event Store + Identity
- Repo skeleton (Go, monorepo: `core/`, `ui/`, `examples/`, `docs/`), CI (build+test+lint).
- Event Store: append, hash chain, checkpoints, replay; actors table, Ed25519 keys.
- **DoD:** Event Store acceptance tests §7.1 (bit-exact replay), §7.2 (tamper), §7.4 (idempotency) are green.

## Week 2 — DSL Compiler
- Lexer/parser (recursive descent) → Semantic Model: `entity`, `roles`, `permissions` (no workflow/automation).
- Error model `{code, file:line, message, fix_hint}` — from day one, not "later".
- **DoD:** entities and permissions of the `examples/collections` pack compile; 20 deliberately broken files produce structured errors with fix_hint (golden tests).

## Week 3 — Entity Engine + REST + Permissions
- Per-entity projections (jsonb + index columns), CRUD, validation, computed fields, `query` with filters/cursor.
- Permission Engine: default deny, row-level (`where`), field-level, deny>allow.
- **DoD:** CRUD for accounts-receivable via REST; permission matrix (3 roles × 6 operations) covered by tests; everything produces events.

## Week 4 — Workflow + Signatures (HITL)
- Transitions: guards, `auto`, `assignee`, `requires approval`; signature queue; **WebAuthn signature** (Event Store test §7.3 — offline verification).
- Compiler: full v0 grammar (workflow, automation syntax, ui sections — parsing).
- **DoD (grammar gate):** both acceptance packs — `collections` and `dev_department` — compile in full. If they do not compile → stop and revise the grammar before writing further.

## Week 5 — Automation + Tasks
- Triggers: `on schedule`, `on create/update`, `on stuck`; actions: `agent task`, `notify email`, `webhook out`, `create/update`, `escalate_to`.
- Task subsystem: pool, `take` with TTL-lease, `complete/fail/expired`, `report_progress` reconciled against events.
- **DoD:** accounts-receivable scenario runs without humans up to the HITL point: overdue → task to agent → (stub agent) → escalation via `stuck`.

## Week 6 — MCP Server
- All v0 tools (contract §2–§5), agent identity/scopes/rate limits, closed error list.
- **DoD:** Claude Code via MCP passes steps 1, 3, 4, 5, 6 of the acceptance scenario (contract §8) on a pre-applied pack.

## Week 7 — Generated UI
- React + Meta API: list (filters, views), form (sections), board, **signature queue**, record journal, login (OIDC/local + WebAuthn).
- **DoD:** a human in the browser works with accounts-receivable end-to-end: sees data according to permissions, moves records through workflow, signs escalations with a passkey.

## Week 8 — Change Pipeline + Build
- `propose_change` → validation → additive migration plan → signature → atomic apply → `revert` via the same path.
- Docker Compose (kalita+postgres), README, quickstart "system in 10 minutes".
- **DoD: full MCP acceptance scenario §8** — agent proposes a pack from scratch, human signs, agent works within it, hits permission boundary and HITL. This is "MVP done".

## Cut Lines (if behind schedule — cut in this order)
1. `view`/`board` in UI (list + form + signatures remain);
2. `webhook out` and `notify email` (agent task remains);
3. `on stuck` + escalations;
4. file fields.
**Never cut:** event store with signatures, deny requirement for agents, signature queue, basis on mutations — these are the product.

## After Week 8 — Gates from the 90-Day Plan
- Publication: open core on GitHub + MCP registries; `dev_department` pack deployed for internal development (dogfood, pipeline migrates to kalita).
- First external pilot: an acquaintance with Bitrix (their top-10 operations → first client pack).
- **Kill gate +3 months:** zero external users → shut down, plan B (video-incidents).

## Key Execution Risks
1. Migrations, even additive ones, are the most delicate part of week 8; prototype migration application as early as week 3 (adding a field to live data).
2. The grammar gate in week 4 is the only point where a spec revision is allowed; after it the grammar is frozen until v1.
3. UI is a time sink; keep it strictly "good enough for a record-keeping system", no polishing perfectionism.
