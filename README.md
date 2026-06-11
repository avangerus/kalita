# Kalita

**An executable runtime for business systems in the agent era.**

Agents and humans describe a business system in a constrained DSL — entities, workflows, permissions, automation, UI. Kalita executes the description directly (no code generation): every change is a signed diff, every action is an event in a tamper-evident journal, every agent is an employee with an identity, permissions and an audit trail.

Why: LLM agents silently corrupt what they are trusted with when the artifact is free-form (code, documents) — and tooling does not help (DELEGATE-52). Kalita replaces welding with Lego: a grammar where drift does not compile, critical transitions require a human signature, and nothing happens silently.

## What works today (MVP)

- **DSL compiler**: entities, workflows, permissions (default deny, deny>allow, field/row level; an agent role without explicit deny does not compile), automation, generated-UI declarations. Errors are structured `{code, file:line, message, fix_hint}` — built for agent self-correction loops.
- **Runtime**: CRUD with validation, workflow transitions with guards and auto-moves, approval queue (a transition behind `requires approval` does not exist until a human signs — Ed25519, offline-verifiable), task pool with TTL leases, automation triggers (schedule/events/stuck), fact-checked progress reports.
- **Event store**: append-only journal in PostgreSQL with a SHA-256 hash chain, DB-level immutability, node-key checkpoints. Definitions replay from the journal — the pack directory is only the genesis seed.
- **MCP gateway** at `/mcp`: 17 tools; an agent can start from an empty node, iterate DSL to green via `validate_dsl`, `propose_change` a pack, and work inside it after a human signs — that loop is the acceptance test.
- **REST + Meta API** for the generated UI (universal client in progress).

## Quick start

```bash
docker compose up --build
# REST:  http://localhost:8080/api/system   (v0 dev auth: X-Actor-Id/X-Actor-Role headers — local only!)
# MCP:   http://localhost:8080/mcp          (bearer tokens: kalita agent add --id bot --role Collector)
```

Or natively: `go build ./cmd/kalita && kalita serve --pack examples/collections` (in-memory journal without `KALITA_PG_DSN` — dev only).

`kalita check --pack <dir>` compiles a pack and prints agent-grade diagnostics.

## Design documents

- [HLD](docs/HLD.md) · [DSL Spec v0](docs/DSL-SPEC-v0.md) · [MCP Contract v0](docs/MCP-CONTRACT-v0.md) · [Event Store v0](docs/EVENT-STORE-v0.md)
- [Security threat model](docs/SECURITY.md) — read before deploying anywhere beyond localhost
- [MVP Backlog](docs/BACKLOG-MVP.md) · [KnowVault module](docs/KNOWVAULT-INTEGRATION.md) · [Portal vision](docs/PORTAL-VISION.md)

## Layout

```
cmd/kalita/        single-binary entry point (serve, check, agent add)
internal/          kernel: eventstore, dsl, engine, identity, api, mcp
packs/             product modules (knowvault, boards) — the kernel knows no domains
examples/          acceptance packs (collections, dev_department)
docs/              design documents + ADRs
```

## Status

MVP weeks 1–8 of the backlog are code-complete except the universal UI client (week 7 frontend) — REST/Meta backend for it is ready. Pre-alpha: do not deploy outside a trusted network (see SECURITY.md P0 list).
