# KnowVault — module on the kalita core

Status: accepted (2026-06-13, revised after founder review: "on the kalita core,
but as a separate module; nothing is to be embedded into the core").

## 0. Sanity rule (the most important one)

**The kalita core knows nothing about any domain.** No accounts-receivable, no knowvault, no
service desk exists or can exist in `internal/` — only the words
`entity`, `workflow`, `permissions`, `tasks` live there. Everything domain-specific goes in packs:

- `examples/` — acceptance packs (test grammar reference implementations: collections,
  dev_department). Not included in a release.
- `packs/` — product modules. KnowVault is the first.

## 1. Module form: pack + worker agents

KnowVault on kalita consists of two parts:

### 1.1 `packs/knowvault/` — DSL pack (orchestration, permissions, audit log)
- `Workspace`, `Source` (files/mail/repositories/databases/chats) — ordinary entities;
- indexing workflow: `New → Indexing → Indexed/Failed`, `Paused` only with
  a VaultAdmin signature;
- `SearchQuery` — search log as an entity: each search = a record with actor,
  role, and result count (provenance via the platform itself);
- roles with deny boundaries: `Indexer agent` cannot touch source paths or
  read other actors' queries; `Searcher agent` cannot see database sources directly;
- stuck indexing (12 h) is escalated to a human.

The pack is compiled with the standard `kalita check --pack packs/knowvault`.

### 1.2 Workers — existing stack as kalita agents
The heavy machinery (ingest, embeddings, Qdrant, connectors) is the existing
Python stack `D:\work\knowvault_app`, connected to the node **as agents**:

- the indexer process registers as an actor with the `Indexer` role (Ed25519 key),
  picks up `start_index` tasks from the kalita pool (TTL-lease), indexes,
  advances Source through the workflow, reports back via `report_progress` (fact check);
- the search service responds to `search_perimeter` (kalita MCP-tool, §2),
  creating `SearchQuery` records under the `Searcher` role.

This gives knowvault everything kalita exists for, for free: identity,
permissions, signed audit log, task leases, escalations, replay — while the core
receives not a single line of search code.

## 2. MCP-tool `search_perimeter` (reserved, implementation after MVP)

`search_perimeter(query, workspace?, limit?)` → snippets + links to sources.
- Call permission — via the role's rights on `Workspace`/`SearchQuery` in the pack;
- results are filtered by the actor's workspaces BEFORE being returned;
- the call creates a `SearchQuery` record (logging is not optional).

## 3. Phasing

1. **Now:** pack in `packs/knowvault/` compiles (done); core is clean.
2. **After MVP (weeks 6–8):** knowvault_app indexer-worker connects
   as an agent via MCP; shared docker-compose (kalita + postgres + qdrant +
   knowvault-services) — one perimeter.
3. **v1:** mirroring source ACL → workspaces, one connector
   per paying vertical (nothing speculative).

## 4. Repository cleanup (founder's decision)

Working — `knowvault_app`. Archive candidates: `knowvault` (orchestration
experiments), `knowvault-v3`, `knowvault_v2`.
