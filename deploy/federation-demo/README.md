# Federation demo (LLD-07)

Two kalita nodes on one machine plus a bridge worker between them, proving the
federation contract: a domain event on one node becomes a mirrored record on
another, **exactly once**, carrying its origin — with **no sync-RPC between the
nodes** (HLD §8a).

```
site node A                         bridge worker                     crm node B
(packs/site)                     (workers/bridge)                   (packs/site)
  Lead --capture-->                                                   MirroredLead
  event.emitted        --read_journal-->  cursor + map + idem  --emit_event-->  on event
  site.LeadCaptured                        (pull, no callback)      LeadMirrored --> create
                                                                     origin = {A, lead-id}
```

## Run

```bash
bash deploy/federation-demo/run.sh
```

The script (in-memory nodes, dev only):

1. builds `kalita.exe` and starts node **A** on `:8101` and node **B** on `:8102`,
   both loading `packs/site`;
2. bootstraps four least-privilege actors — an `Operator` on each node, a
   `BridgeReader` on A (source identity: read only), a `BridgeEmitter` on B
   (target identity: emit only);
3. captures a `Lead` on A (`capture` transition emits `site.LeadCaptured`);
4. runs the bridge once — it reads the source journal, maps the payload, stamps
   `origin_node`/`origin_id`, and emits `LeadMirrored` on B, whose `on event`
   subscriber creates a `MirroredLead`;
5. proves **exactly-once**: it drops the cursor and replays the source from
   `seq=0` — the deterministic idempotency key `bridge|<node>|<seq>` collapses
   the duplicate on B, so node B still holds exactly one mirror.

Expected tail:

```
   mirror: Ada Lovelace | origin_node= site-node-a origin_id= <lead-id>
DEMO OK: lead mirrored exactly once (origin stamped), replay did not duplicate.
```

### Durable (Postgres) variant

Set `KALITA_PG_DSN` and give each node its own schema (`kalita_a` / `kalita_b`)
via `KALITA_PG_SCHEMA` before starting it, instead of the in-memory default.

## Configuration

* `deploy/federation-demo/bridge-config.json` — the declarative subscription
  map (source event → target event + field map). Secrets (both node tokens) are
  supplied to the bridge via env by `run.sh`, never in a pack DSL (LLD-07 §5).
* Least-privilege roles live in `packs/site` (`BridgeReader` = read only,
  `BridgeEmitter` = emit only).

## Architectural check (LLD-07 §7 test 5)

```bash
bash deploy/federation-demo/check_no_sync_rpc.sh
```

Greps `internal/` (the core) for any outbound HTTP / remote-node addressing and
fails if found — the constitution forbids node→node sync-RPC; all cross-node
flow goes through the bridge worker.

## Known limitation (core gap, LLD-07 §8)

The MCP `read_journal` tool is per-record and has no node-wide `since=seq`
stream, so the bridge cannot pull the whole source journal in one governed call.
It works around this with `query` + per-record `read_journal` (correct for
transition-emitted events, but O(records) per poll). The efficient path needs a
node-wide journal-since read — a **core** change, recorded for review in
`agent/kalita/QUESTIONS-LLD.md`, not made here.
