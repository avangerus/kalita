# KnowVault as a finished product — definition of done

Principle (T12): a company installs KnowVault as an application without knowing about kalita.
"Finished" = installed by following instructions → in active use → never once encountered
DSL, MCP, tokens, or workers manually. kalita is the invisible engine.

## What separates "the engine works" from "the product can be handed to people"

### 1. Single installation command 🔥
- [ ] `docker compose up` brings up the ENTIRE stack: kalita node + Postgres +
      Qdrant + indexer-worker + search-worker, with linked configs
- [ ] the node starts with the knowvault pack as genesis, workers auto-register
      (a bootstrap-token mechanism is required: on first start a worker receives
      its identity via a shared secret from compose, not manually)
- [ ] branding: "KnowVault" — "kalita" never appears in UI/logs/docs

### 2. Product UI, not an admin panel 🔥
- [ ] **"Search"** screen — the face of the product: question field → answer with sources →
      click opens the document. This is a custom screen (escape hatch UI).
- [ ] **document upload** drag-drop (file fields from V1-GATE) — users "upload
      documents", not "specify paths to folders"
- [ ] workspaces, sources, indexing statuses — in a product wrapper
      (generated UI under the brand, "Documents/Sources" section)
- [ ] login by username/password or invite link, NOT "paste your token" (that is for
      a developer, not an accountant)

### 3. HTTP, not scripts 🔥
- [ ] search — HTTP endpoint (node proxies to search-worker, or worker
      hosts a service), UI screen calls it. Current logic lives in ask.py — migrate it.
- [ ] indexer and search as compose services, not manually launched py files

### 4. Operations for non-technical users
- [ ] one-button/one-command backup, restore by documented procedure
- [ ] "system health": is the indexer alive? Is Qdrant alive? Is the model responding? —
      status page so nobody has to dig through logs

### 5. The kalita reveal moment (upsell, not upfront)
- [ ] "Automation" section in the admin panel — locked/empty in base KnowVault,
      unlocks on upgrade; that is where "turns out there's a platform here" begins

## Order (autopilot goes top-down)
HTTP search endpoint → Search screen in UI → file upload → worker bootstrap →
full stack compose → branding → status page.

This is the path from today's live demo to a boxed product. The other
V1-GATE tasks (passkeys, i18n, aggregates) are also needed for KnowVault, but these seven —
are the ones without which it cannot be called a product.
