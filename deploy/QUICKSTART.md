# Run Kalita — the self-building business system — on your own iron

One command brings up the PRODUCT: the Kalita node (HTTPS, Postgres journal,
persistent key) AND the Ask Kalita builder worker — the conversational layer
that builds and changes your business processes.

## One command

```bash
cp .env.example .env          # set POSTGRES_PASSWORD (and KALITA_LLM_URL if you use a model)
docker compose up --build
```

You get:
- **The product UI** at `https://localhost:8443` — Home / Ask Kalita / Work /
  Approvals / Model / Changes (self-signed cert on first boot; accept it or
  trust `node.crt` from the `nodedata` volume).
- **Ask Kalita** — the builder worker on `http://localhost:8400` (the UI
  derives this automatically; behind a TLS proxy for remote deployments —
  set `KALITA_BUILDER_URL`).
- **MCP** for agents at `https://localhost:8443/mcp` (`Authorization: Bearer <token>`).
- The **journal in Postgres** (`journal` volume) — the single source of truth.
- The **node key** persisted (`nodedata` volume) so signed checkpoints stay
  verifiable across restarts.

Restart-safe: `docker compose restart kalita` (or a full `down`/`up`) replays the
journal — records, identities and history survive (a 100k-event journal replays
in ~1 s; see ADR-001).

## First user (the owner / approver)

A fresh node is empty (genesis). Create the first account with a PASSWORD —
that person signs in through the web UI (вкладка «Вход»):

```bash
docker compose exec kalita kalita user add --id owner --role Owner --password change-me-on-first-login
```

Every further user onboards through the product itself: Agents → приглашение →
the person redeems the invite code at the login screen («Приглашение»).
The role `Owner` (KALITA_APPROVER) is the one whose signature applies model
changes built in Ask Kalita.

Additional identities via CLI when needed:

```bash
docker compose exec kalita kalita agent add --id legal-bot --role Legal
```

Or run the demo seed for a throwaway node (never on a real journal):

```bash
# add --demo to the command in docker-compose.yml, then `up` on an empty journal
```

## Choosing the module

Set `KALITA_PACK` in `.env` to any module baked into the image
(`/opt/kalita/packs/<name>` or `/opt/kalita/examples/<name>`), or bind-mount your
own pack directory and point `KALITA_PACK` at it.

## Production TLS

Two options instead of the self-signed default:

1. **Real certificate.** Put `node.crt` / `node.key` into the `nodedata` volume's
   `tls/` directory; they are reused instead of generating one.
2. **Reverse proxy.** Set `KALITA_TLS=` (empty) and add `--insecure-http` to the
   command; terminate TLS in a proxy (Caddy/nginx/Traefik) in front. The node
   still refuses plaintext on a non-loopback address unless `--insecure-http` is
   explicit.

## Backups

Back up the `journal` volume (the events) and the `nodedata` volume (the node
key). The journal is append-only and hash-chained; `kalita` verifies the chain on
read, and `UPDATE`/`DELETE` against it are blocked at the database level.
