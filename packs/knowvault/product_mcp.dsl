# product.mcp — domain-facing MCP tools the façade worker exposes over the
# node MCP (DSL-SPEC-v0 §1.2, LLD-06 §1, PK-2), for KnowVault standalone.
# Pure manifest DATA: the façade worker (workers/product_mcp/facade.py) reads
# this section and executes each tool as a composition of node calls under the
# CALLER'S OWN bearer token (strict pass-through, no service-account writes —
# ABAC/HITL apply exactly as if the caller invoked create_record/act/query
# directly). `compose` is declarative, uninterpreted-by-this-compiler text;
# workers/product_mcp/compose.py is the interpreter, and it understands
# EXACTLY three verbs — `create`, `act`, `query` — nothing else. No new verb
# is invented here.

product.mcp:
    # Fully functional: the indexer picks up any Source with status=New on its
    # own (packs/knowvault/knowvault.dsl workflow `New -> Indexing:
    # start_index assignee=agent(Indexer)`, driven by the task pool — see
    # packs/knowvault/README.md §1.2), so `create Source(...)` alone is
    # enough to get a document indexed; no extra `act` step is needed or
    # permitted here (start_index is only in the Indexer role's act list,
    # packs/knowvault/knowvault.dsl permissions — an MCP caller does not hold
    # that role). `path` is a required Source field but is never read by the
    # indexer for kind=Upload (workers/knowvault_indexer/indexer.py `units()`:
    # for Upload it reads `document` via /api/files/{hash}, not `path`) — the
    # constant below just satisfies the entity's `required` constraint.
    tool knowvault.upload "Upload a document into a workspace so it gets indexed and becomes searchable":
        in:
            workspace: ref[Workspace] required
            document:  file required
        compose:
            create Source(workspace: $workspace, document: $document, kind: Upload, path: "upload") -> $source
        out: source

    # There is DELIBERATELY no `knowvault.search` tool here. A tool named
    # "search" that only writes a SearchQuery log record and returns no answer
    # would be a trap for agent callers (the name promises retrieval; the
    # compose interpreter — workers/product_mcp/compose.py, exactly three
    # verbs create/act/query — cannot reach the real RAG path, the node's
    # `/api/search` REST proxy in internal/api/ragproxy.go). Humans search
    # through the UI / POST /api/search under their own token (see
    # deploy/knowvault/README.md "Как искать"). The real MCP-native search
    # for agents is `search_perimeter` — a NODE-level core tool reserved by
    # packs/knowvault/README.md §2; when it lands, agents get retrieval
    # with the same governance (search logged as SearchQuery by the node,
    # not by the caller's façade compose).
