# Federation demo (LLD-07)

Two kalita nodes on one machine plus a bridge worker between them, proving the
federation contract: a domain event on one node becomes a mirrored record on
another, **exactly once**, carrying its origin — with **no sync-RPC between the
nodes** (HLD §8a).

```
site node A                         bridge worker                     crm node B
(packs/site)                     (workers/bridge)                   (packs/site)
  Lead --capture-->                                                   MirroredLead
  event.emitted        --read_events-->  cursor + map + idem  --emit_event-->  on event
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
   `BridgeReader` on A (source identity: `tail [event.emitted]` only, node-wide,
   no per-record read at all), a `BridgeEmitter` on B (target identity: emit
   only);
3. captures a `Lead` on A (`capture` transition emits `site.LeadCaptured`);
4. runs the bridge once — it pulls `event.emitted` from A via `read_events`
   (LLD-08 §1), maps the payload, stamps `origin_node`/`origin_id`, and emits
   `LeadMirrored` on B, whose `on event` subscriber creates a `MirroredLead`;
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

## Node-wide tail (closed, LLD-08)

Earlier revisions of this demo worked around a core gap: `read_journal` is
per-record with no node-wide `since=seq` stream, so the bridge scanned
`query` + per-record `read_journal` (O(records) per poll) and could not see
free-standing `emit_event` calls (empty subject — unreachable from any
record's own journal). LLD-08 closed this with the MCP tool `read_events
{since, kinds}` over the existing `eventstore.Store.Since`, and a new
permission verb `tail [kind]` scoping node-wide read access per kind. The
bridge (`workers/bridge/bridge.py`) now pulls `event.emitted` this way —
O(new events) per poll, and free-standing emits are visible. See
`agent/kalita/QUESTIONS-LLD.md` for the history and design decisions.
