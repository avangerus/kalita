#!/usr/bin/env python3
"""Federation bridge (LLD-07, transport upgraded by LLD-08, reliability loop
rebuilt on `workers/lib/subscribe.py` by LLD-21 PR-1) — a Worker (T8), NOT core.

A bridge has TWO identities: a least-privilege governed actor on the SOURCE
node (`tail [event.emitted]` ONLY — no per-record read at all, LLD-08 §4) and
a least-privilege governed actor on the TARGET node (`emit [Name]` only). It
pulls `event.emitted` facts node-wide from the source journal via the MCP tool
`read_events` and re-emits them on the target via `emit_event`. Neither node
knows the other exists — only the bridge does (the perimeter boundary stays
explicit). No sync-RPC between nodes (HLD §8a): the source is pulled, the
target is written; the source never waits for the target.

exactly-once (LLD-07 §2) = at-least-once + idempotent key. The delivery key is
`bridge|<source_node_id>|<source_seq>` — deterministic from the source event's
`seq`, taken straight from the read_events response (LLD-08 §4). If the bridge
crashes between delivering and advancing its cursor, the retry re-emits with
the SAME key and the target's `emit_event` ByIdemKey collapses the duplicate
(PR-5 made MCP idempotency mandatory). The cursor is therefore an efficiency
optimisation, not a correctness dependency — losing it only replays from a
smaller seq, and idempotency absorbs the repeats.

origin provenance (LLD-07 §3) is a mirror WRITE, not a remote lookup: the
target event carries `origin_node`/`origin_id` (two plain string fields — no new
core type is needed), so the mirrored record on the target points back at its
source. The target pack's `on event` subscriber creates the local record from
the emitted payload.

Config (LLD-07 §5): env and/or a JSON file (KALITA_BRIDGE_CONFIG). Secrets
(both node tokens) live here, in the worker — never in a pack DSL. Zero
dependencies: stdlib only.

  SOURCE_URL, SOURCE_TOKEN         source node + bearer for the tail actor
  TARGET_URL, TARGET_TOKEN         target node + bearer for the emit actor
  SOURCE_NODE_ID                   stable id of the source, half of the idem key
  KALITA_BRIDGE_CURSOR             cursor state file (default bridge_cursor.json)
  KALITA_BRIDGE_POLL_SEC           poll interval seconds (default 2)
  subscriptions: [ {                a declarative map, not code
     "event":        "site.LeadCaptured",   source event (canonical or short)
     "target_event": "LeadMirrored",        event to emit on the target
     "field_map":    {"name":"name", ...},  source payload field -> target field
     "origin_node_field": "origin_node",    (optional) target field for node id
     "origin_id_field":   "origin_id"       (optional) target field for source id
  } ]

LLD-08 closes the LLD-07 §8 core gap (see agent/kalita/QUESTIONS-LLD.md for
the history): `read_events {since, kinds:[event.emitted]}` is a node-wide,
O(new events) tail over the whole journal — not the old per-record
`query`+`read_journal` scan, which was O(records) per poll AND blind to
free-standing `emit_event` calls (empty subject, unreachable from any record's
per-record journal). Both limitations are gone: the bridge now sees every
event.emitted on the node, transition-emitted or free-standing alike.

LLD-21 PR-1 generalises this exact loop into `workers/lib/subscribe.py` and
refactors the bridge to sit on top of it — the bridge is the library's proof
of a second consumer (coverage_bot/KnowVault-sink come next). Nothing about
the bridge's observable behaviour changes: the delivery key is still
`bridge|<source_node_id>|<source_seq>` (Bridge picks
`subscription_id = "bridge|<source_node_id>"`, so the library's generic
`<subscription_id>|<source_seq>` key renders byte-for-byte identical to the
pre-refactor key — no idempotency-key format change on any already-running
bridge). The one behavioural IMPROVEMENT (§4, not exercised by any existing
test — see test_bridge.py's own docstring): a governed rejection from the
target (e.g. a poison event outside the BridgeEmitter role's grant) used to
wedge the cursor forever; now the subscriber library dead-letters it and
advances past it, same as every other library consumer.
"""
import json
import os
import sys
import time

_HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(_HERE, "..", "lib"))
from subscribe import (  # noqa: E402
    GovernedError, MCPClient, EventSource, FileCursor, Subscriber,
)

# Bridge's read-side transport is exactly subscribe.py's EventSource; kept
# under its historical name so `from bridge import MCPSource` (test_bridge.py)
# keeps resolving without the test file changing.
MCPSource = EventSource


# --------------------------------------------------------------------------- #
# Target: emit the mirrored event.
# --------------------------------------------------------------------------- #

class MCPTarget:
    """Emits a domain event on the target node via the emit_event MCP tool."""

    def __init__(self, client):
        self.client = client

    def emit(self, target_event, payload, idem_key):
        return self.client.call("emit_event", {
            "name": target_event, "payload": payload, "idempotency_key": idem_key})


# --------------------------------------------------------------------------- #
# Bridge core — transport-agnostic so tests inject fakes. Now a thin adapter
# over subscribe.Subscriber: the field-mapping/origin-stamping is bridge's own
# (only custom) code, handed to the library as `handler(fact, act)`.
# --------------------------------------------------------------------------- #

class Bridge:
    """The exactly-once loop of LLD-07 §2, built on `subscribe.Subscriber`
    (LLD-21 §2). `source` provides read_since, `target` provides emit,
    `cursor` persists last_seq. `subscriptions` and `source_node_id` come
    from config."""

    def __init__(self, source, target, cursor, subscriptions, source_node_id):
        self.source = source
        self.target = target
        self.cursor = cursor
        self.source_node_id = source_node_id
        # index subscriptions by BOTH canonical and short event name so a config
        # written either way resolves.
        self.by_event = {}
        for sub in subscriptions:
            ev = sub["event"]
            self.by_event[ev] = sub
            self.by_event[ev.rsplit(".", 1)[-1]] = sub
        # subscription_id chosen so the library's generic `<sub>|<seq>` key
        # renders exactly as the bridge's historical `bridge|<node>|<seq>` key
        # (no format change on an already-running bridge, see module docstring).
        self._subscriber = Subscriber(
            source=source, cursor=cursor,
            subscription_id=f"bridge|{source_node_id}",
            events=self._match,       # predicate: matched sub dict, or None
            handler=self._handle,
            dead_letter=None,        # bridge keeps its existing two roles for
                                      # PR-1 (no new emit grant introduced here);
                                      # wiring subscription.failed for bridge is
                                      # a governance question left to PR-2/3.
        )

    def _match(self, name):
        if name in self.by_event:
            return self.by_event[name]
        return self.by_event.get(name.rsplit(".", 1)[-1])

    def idem_key(self, seq):
        # deterministic from the source event (LLD-07 §2): a replay of the same
        # source seq produces the same key and collapses on the target. Kept
        # as a direct method (equals Act.idem_key() for the same seq) for any
        # caller that still reaches for it directly.
        return f"bridge|{self.source_node_id}|{seq}"

    def _target_payload(self, sub, fact):
        payload = {}
        for src_field, tgt_field in (sub.get("field_map") or {}).items():
            if src_field in fact["payload"]:
                payload[tgt_field] = fact["payload"][src_field]
        # origin provenance (LLD-07 §3): mirror-write two plain string fields.
        node_field = sub.get("origin_node_field", "origin_node")
        id_field = sub.get("origin_id_field", "origin_id")
        if node_field:
            payload[node_field] = self.source_node_id
        if id_field:
            payload[id_field] = fact["record_id"]
        return payload

    def _handle(self, fact, act):
        """The bridge's only custom code, handed to Subscriber as `handler`:
        map the fact to the target's payload shape and emit it under the
        library's deterministic key. `act.idem_key()` for this fact and
        subscription_id renders identically to the pre-refactor `idem_key()`."""
        sub = self._match(fact["name"])
        payload = self._target_payload(sub, fact)
        self.target.emit(sub["target_event"], payload, act.idem_key())

    def run_once(self):
        """One pull→deliver→advance pass. Returns the number of events handed
        to the target (matched facts only). The cursor advances AFTER each
        fact is resolved (delivered, or dead-lettered) so a crash mid-pass
        replays only unconfirmed (or idempotently-absorbed) events."""
        return self._subscriber.run_once()


# --------------------------------------------------------------------------- #
# Config + main loop.
# --------------------------------------------------------------------------- #

def load_config():
    cfg = {}
    path = os.environ.get("KALITA_BRIDGE_CONFIG")
    if path:
        with open(path, encoding="utf-8") as f:
            cfg = json.load(f)
    # env overrides / supplies the connection + secrets (never in a pack DSL)
    cfg.setdefault("source_url", os.environ.get("SOURCE_URL", "http://127.0.0.1:8101"))
    cfg.setdefault("source_token", os.environ.get("SOURCE_TOKEN", ""))
    cfg.setdefault("target_url", os.environ.get("TARGET_URL", "http://127.0.0.1:8102"))
    cfg.setdefault("target_token", os.environ.get("TARGET_TOKEN", ""))
    cfg.setdefault("source_node_id", os.environ.get("SOURCE_NODE_ID", "node-a"))
    cfg.setdefault("cursor", os.environ.get("KALITA_BRIDGE_CURSOR", "bridge_cursor.json"))
    cfg.setdefault("poll_sec", float(os.environ.get("KALITA_BRIDGE_POLL_SEC", "2")))
    cfg.setdefault("subscriptions", [])
    # env token overrides win even if the file set a value
    if os.environ.get("SOURCE_TOKEN"):
        cfg["source_token"] = os.environ["SOURCE_TOKEN"]
    if os.environ.get("TARGET_TOKEN"):
        cfg["target_token"] = os.environ["TARGET_TOKEN"]
    return cfg


def build(cfg):
    subs = cfg["subscriptions"]
    source = MCPSource(MCPClient(cfg["source_url"], cfg["source_token"]))
    target = MCPTarget(MCPClient(cfg["target_url"], cfg["target_token"]))
    cursor = FileCursor(cfg["cursor"])
    return Bridge(source, target, cursor, subs, cfg["source_node_id"])


def main():
    cfg = load_config()
    if not cfg["subscriptions"]:
        sys.exit("no subscriptions configured (KALITA_BRIDGE_CONFIG); nothing to bridge")
    if not cfg["source_token"] or not cfg["target_token"]:
        sys.exit("SOURCE_TOKEN and TARGET_TOKEN are required (secrets live in the worker config)")
    bridge = build(cfg)
    print(f"bridge up: {cfg['source_url']} ({cfg['source_node_id']}) -> {cfg['target_url']}; "
          f"{len(cfg['subscriptions'])} subscription(s), cursor {cfg['cursor']}", flush=True)
    if "--once" in sys.argv[1:]:
        # one pull->deliver->advance pass then exit — for scripted demos/tests.
        n = bridge.run_once()
        print(f"[bridge] delivered {n} event(s) (once)", flush=True)
        return
    while True:
        try:
            n = bridge.run_once()
            if n:
                print(f"[bridge] delivered {n} event(s)", flush=True)
        except GovernedError as ex:
            # a governed rejection from the SOURCE side itself (e.g. the read
            # actor's tail grant was revoked) — Subscriber already absorbs a
            # TARGET-side rejection internally (dead-letter + advance, §4);
            # this is a defensive fallback for the read path, which sits
            # outside handler() and so outside that internal handling.
            print(f"[bridge] rejected: {ex}", flush=True)
        time.sleep(cfg["poll_sec"])


if __name__ == "__main__":
    main()
