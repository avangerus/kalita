# Run a Kalita node on your own iron

One command brings up a self-contained node — HTTPS, a Postgres journal, and a
persistent node key — inside your own perimeter. This is the "agent inside the
client's contour" promise made concrete.

## One command

```bash
cp .env.example .env          # set POSTGRES_PASSWORD
docker compose up --build
```

You get:
- **HTTPS** on `https://localhost:8443` — the node generates a self-signed
  certificate on first boot (no public domain needed). Accept the cert warning,
  or trust `node.crt` from the `nodedata` volume.
- **MCP** for agents at `https://localhost:8443/mcp` (`Authorization: Bearer <token>`).
- The **journal in Postgres** (`journal` volume) — the single source of truth.
- The **node key** persisted (`nodedata` volume) so signed checkpoints stay
  verifiable across restarts.

Restart-safe: `docker compose restart kalita` (or a full `down`/`up`) replays the
journal — records, identities and history survive (a 100k-event journal replays
in ~1 s; see ADR-001).

## First tokens

A fresh node is empty (genesis). Mint identities through the journal:

```bash
docker compose exec kalita kalita user add --id alice --role ContractManager
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
