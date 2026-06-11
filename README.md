# Kalita

**An executable runtime for business systems in the agent era.**

Agents and humans describe a business system in a constrained DSL — entities, workflows, permissions, automation, UI. Kalita executes the description directly (no code generation): every change is a signed diff, every action is an event in a tamper-evident journal, every agent is an employee with an identity, permissions and an audit trail.

Why: LLM agents silently corrupt what they are trusted with when the artifact is free-form (code, documents). Kalita replaces welding with Lego — a grammar where drift does not compile, critical transitions require a human signature, and nothing happens silently.

## Design documents

- [HLD](docs/HLD.md) — architecture, principles, components
- [DSL Specification v0](docs/DSL-SPEC-v0.md) — the grammar (MVP scope)
- [MCP Contract v0](docs/MCP-CONTRACT-v0.md) — the agent interface
- [Event Store v0](docs/EVENT-STORE-v0.md) — journal, hash chain, signatures
- [MVP Backlog](docs/BACKLOG-MVP.md) — 8-week plan with acceptance gates

## Status

Week 1 of MVP: event store core (hash chain, replay, idempotency). Pre-alpha, nothing to run yet.

## Layout

```
cmd/kalita/        entry point
internal/          engines (eventstore, compiler, entity, workflow, permission, automation)
examples/          acceptance packs: collections, dev_department
docs/              design documents
```
