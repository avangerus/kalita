# KnowVault — perimeter RAG search as a kalita pack.
# The kernel knows nothing about search; this file IS the knowvault module:
# workspaces, sources, the indexing workflow and the search journal are plain
# kalita entities. The heavy machinery (embeddings, Qdrant, connectors) runs
# as worker AGENTS with roles, identities and deny-boundaries like any other.

# Business settings live as a singleton entity: changed via the UI within
# permissions, every change is a journal event ("who switched the model").
# Secrets are FORBIDDEN here (SECURITY rule #5) — workers hold their own keys
# and reference them by id.
entity VaultSettings singleton:
    embedding_model: string required default="bge-m3"
    llm_endpoint: string
    chunk_size: int default=512
    language: enum[Auto, Ru, En] default=Auto

entity Workspace:
    name: string required unique
    description: text

entity Source:
    workspace: ref[Workspace] on_delete=restrict
    kind: enum[Files, Mail, Repo, Database, Chat, Upload]
    path: string required
    document: file
    status: enum[New, Indexing, Indexed, Failed, Paused] default=New
    last_indexed: datetime
    documents: int

constraints:
    unique(workspace, path)

entity SearchQuery:
    workspace: ref[Workspace] on_delete=restrict
    query: string required
    actor_role: string
    results: int
    status: enum[Logged] default=Logged

workflow Source on status:
    New      -> Indexing: start_index assignee=agent(Indexer)
    Failed   -> Indexing: retry_index
    Indexing -> Indexed:  finish_index assignee=agent(Indexer)
    Indexing -> Failed:   fail_index assignee=agent(Indexer)
    any      -> Paused:   pause requires approval(VaultAdmin)

roles:
    VaultAdmin
    Indexer agent
    Searcher agent

permissions:
    VaultAdmin:
        full    [Workspace, Source, VaultSettings]
        read    [SearchQuery]
        act     [retry_index, pause]
        approve [pause]
    Indexer:
        read   [Workspace, Source, VaultSettings]
        update [Source]
        act    [start_index, finish_index, fail_index]
        deny   [delete *, update Workspace.*, update VaultSettings.*, read SearchQuery, update Source.path]
    Searcher:
        read   [Workspace]
        create [SearchQuery]
        deny   [delete *, update Source.*, update Workspace.*, read Source where kind = Database]

automation:
    on stuck Source in Indexing for 12h:
        escalate_to VaultAdmin

ui Source:
    list: [path, kind, status, last_indexed, documents] sort=-last_indexed
        filters: [status, kind]
    board: by status

ui Workspace:
    list: [name, description]

ui SearchQuery:
    list: [query, actor_role, results] sort=-results
