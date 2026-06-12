# Kalita Feature Tree (recorded 2026-06-13)

Priority rule: first everything the live pilot needs (the Bitrix acquaintance),
then public growth, then strategic layers.

## 0. Configuration — Three Levels (principle)

Where a setting lives is determined by WHO changes it:

1. **Node (infrastructure):** listen/TLS/PG DSN/approver — flags, env,
   config file 0600. Secrets live only here or in workers. Changed by admin.
2. **Pack (business settings):** `Settings` singleton entity in DSL — e.g.
   `VaultSettings: embedding_model, chunk_size, llm_endpoint, language`.
   Changed via UI according to permissions; changing the model = a log event
   (audit "who switched the model" for free). **Secrets are forbidden here**
   (SECURITY rule #5) — only key_id in the record, the key itself belongs to
   the worker.
3. **Worker (agent):** its process parameters are its config; but business
   settings it MUST read from the node's Settings entity and apply.

## 1. Agent as Actor (unified connection model)

Any external entity — knowvault worker, Claude Code, federation bridge —
connects in ONE way: registry actor (id, role, token/key, deny).
Add: **registration metadata** (model, endpoint, owner, description) in
payload actor.registered → visible in UI and log; "Agents" screen (admin):
list, status, disable, key/token rotation, last activity.

## 2. v0.2 — Before the Pilot (≈2-3 weeks)

Core:
- [ ] singleton entities (`entity X: ... singleton`) for Settings
- [ ] actor metadata + `kalita user revoke` (emergency revocation, P1.7)
- [ ] incremental projections for hot scans (registry, facts)
UI:
- [ ] Agents screen (admin)
- [ ] ref fields with dropdowns (currently bare id), filters/search in lists, pagination
- [ ] file fields (upload to content-addressed storage)
MCP:
- [ ] long-polling `wait_for_task` (instead of polling)
KnowVault:
- [ ] indexer worker as an actor (first integration), `search_perimeter`
- [ ] VaultSettings singleton in the pack
Portal (minimum for the pilot):
- [ ] self-registration of an external user with binding to a record (Customer)

## 3. v1 — Public Growth

- escape hatch: WASM components (custom screens, integrations, payments)
- pre-signature simulation (seed of layer 2; event sourcing already ready)
- aggregates and dashboards in DSL (`metric`, `dashboard` — words reserved)
- i18n labels (not grammar)
- destructive migrations via manual procedure with export
- federation: bridge-agent MVP (log-as-outbox; no sync RPC)
- pack marketplace (semver+author signature already in pack format)
- WebAuthn/passkey (P1.4), crypto-shredding (P2)

## 4. Layers 2–3 — Strategy (designed, not being built)

Agent ratings on the signature corpus · drift detector · portable
reputation · insurability · cross-organizational contracts · signer market.
See HLD §8.
