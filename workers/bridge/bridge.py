#!/usr/bin/env python3
"""Federation bridge (LLD-07) — a Worker (T8), NOT core.

A bridge has TWO identities: a least-privilege governed actor on the SOURCE
node (read + read_journal on the subscribed entities) and a least-privilege
governed actor on the TARGET node (`emit [Name]` only). It reads
`event.emitted` facts from the source journal and re-emits them on the target
via the `emit_event` MCP tool. Neither node knows the other exists — only the
bridge does (the perimeter boundary stays explicit). No sync-RPC between nodes
(HLD §8a): the source is pulled, the target is written; the source never waits
for the target.

exactly-once (LLD-07 §2) = at-least-once + idempotent key. The delivery key is
`bridge|<source_node_id>|<source_seq>` — deterministic from the source event.
If the bridge crashes between delivering and advancing its cursor, the retry
re-emits with the SAME key and the target's `emit_event` ByIdemKey collapses
the duplicate (PR-5 made MCP idempotency mandatory). The cursor is therefore an
efficiency optimisation, not a correctness dependency — losing it only replays
from a smaller seq, and idempotency absorbs the repeats.

origin provenance (LLD-07 §3) is a mirror WRITE, not a remote lookup: the
target event carries `origin_node`/`origin_id` (two plain string fields — no new
core type is needed), so the mirrored record on the target points back at its
source. The target pack's `on event` subscriber creates the local record from
the emitted payload.

Config (LLD-07 §5): env and/or a JSON file (KALITA_BRIDGE_CONFIG). Secrets
(both node tokens) live here, in the worker — never in a pack DSL. Zero
dependencies: stdlib only.

  SOURCE_URL, SOURCE_TOKEN         source node + bearer for the read actor
  TARGET_URL, TARGET_TOKEN         target node + bearer for the emit actor
  SOURCE_NODE_ID                   stable id of the source, half of the idem key
  KALITA_BRIDGE_CURSOR             cursor state file (default bridge_cursor.json)
  KALITA_BRIDGE_POLL_SEC           poll interval seconds (default 2)
  subscriptions: [ {                a declarative map, not code
     "event":        "site.LeadCaptured",   source event (canonical or short)
     "target_event": "LeadMirrored",        event to emit on the target
     "source_entity":"Lead",                entity whose journal carries it
     "field_map":    {"name":"name", ...},  source payload field -> target field
     "origin_node_field": "origin_node",    (optional) target field for node id
     "origin_id_field":   "origin_id"       (optional) target field for source id
  } ]

read_journal caveat (LLD-07 §8, see agent/kalita/QUESTIONS-LLD.md): the MCP
`read_journal` tool is per-record (entity+id) and has no node-wide `since=seq`
stream, so a bridge cannot pull the whole journal in one governed call today.
This worker works AROUND that with the existing governed tools — `query` the
subscribed entity, then `read_journal` each record and keep the `event.emitted`
entries whose global seq is past the cursor. That is correct for the demo
(transition-emitted events carry a record subject) but O(records) per poll; the
efficient path needs a node-wide journal-since read, which is a CORE change
reserved for Fable review, not made blindly here.
"""
import json
import os
import sys
import time
import urllib.request
import urllib.error

# Retry/backoff mirrors workers/bootstrap.py exactly (same strategy + constants):
# a transient network failure (URLError/timeout) is retried on a fixed sleep; a
# governed HTTP error is NOT retried (the node has spoken — surface it). We do
# not invent a new backoff curve here.
RETRY_ATTEMPTS = 30
RETRY_SLEEP_SEC = 2


class GovernedError(Exception):
    """The node rejected the call on its merits (permission/validation). Not
    transient — retrying the same request cannot fix it, so it is never retried
    and the cursor is never advanced past the offending event."""

    def __init__(self, code, message):
        super().__init__(f"{code}: {message}")
        self.code = code
        self.message = message


# --------------------------------------------------------------------------- #
# MCP transport (stdlib urllib), shaped exactly like workers/connectors/inbound.
# --------------------------------------------------------------------------- #

class MCPClient:
    """One kalita node reached over its /mcp endpoint under one bearer token."""

    def __init__(self, url, token):
        self.url = url.rstrip("/")
        self.token = token

    def call(self, name, arguments):
        """Invoke one MCP tool. Returns the decoded result payload. Retries a
        transient network failure (bootstrap.py strategy); raises GovernedError
        when the node itself reports the call as an error."""
        body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/call",
                           "params": {"name": name, "arguments": arguments}}).encode()
        last_err = None
        for attempt in range(RETRY_ATTEMPTS):
            try:
                req = urllib.request.Request(self.url + "/mcp", data=body, headers={
                    "Authorization": "Bearer " + self.token, "Content-Type": "application/json"})
                with urllib.request.urlopen(req, timeout=30) as resp:
                    out = json.load(resp)
                break
            except (urllib.error.URLError, TimeoutError) as ex:
                last_err = ex
                if attempt < RETRY_ATTEMPTS - 1:
                    time.sleep(RETRY_SLEEP_SEC)
        else:
            raise last_err
        res = out.get("result", {})
        content = res.get("content", [])
        payload = json.loads(content[0]["text"]) if content else {}
        if res.get("isError"):
            raise GovernedError(payload.get("code", "REJECTED"),
                                payload.get("message", "call rejected"))
        return payload


# --------------------------------------------------------------------------- #
# Source: pull event.emitted facts since a cursor.
# --------------------------------------------------------------------------- #

class MCPSource:
    """Reads `event.emitted` facts from a source node past a cursor, using only
    the governed read tools available today (see the read_journal caveat in the
    module docstring). `subscribed` is the set of source entities to scan."""

    def __init__(self, client, subscribed_entities):
        self.client = client
        self.entities = sorted(set(subscribed_entities))

    def read_since(self, last_seq):
        """Return event.emitted facts with seq > last_seq, sorted by seq asc.
        Each fact: {seq, name (canonical), payload, entity, record_id}."""
        facts = []
        for entity in self.entities:
            res = self.client.call("query", {"entity": entity, "limit": 10000})
            for rec in res.get("records", []):
                rid = rec.get("id")
                if not rid:
                    continue
                jr = self.client.call("read_journal", {"entity": entity, "id": rid})
                for ev in jr.get("events", []):
                    if ev.get("kind") != "event.emitted":
                        continue
                    seq = ev.get("seq", 0)
                    if seq <= last_seq:
                        continue
                    p = ev.get("payload") or {}
                    facts.append({"seq": seq, "name": p.get("name", ""),
                                  "payload": p.get("payload") or {},
                                  "entity": entity, "record_id": rid})
        facts.sort(key=lambda f: f["seq"])
        return facts


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
# Cursor: persistent last-delivered source seq.
# --------------------------------------------------------------------------- #

class FileCursor:
    """last_seq persisted to a JSON file. Advanced ONLY after a delivery is
    confirmed, so a crash before persistence replays (idempotency absorbs it)."""

    def __init__(self, path):
        self.path = path

    def load(self):
        try:
            with open(self.path, encoding="utf-8") as f:
                return int(json.load(f).get("last_seq", 0))
        except (FileNotFoundError, ValueError):
            return 0

    def save(self, seq):
        tmp = self.path + ".tmp"
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump({"last_seq": int(seq)}, f)
        os.replace(tmp, self.path)


# --------------------------------------------------------------------------- #
# Bridge core — transport-agnostic so tests inject fakes.
# --------------------------------------------------------------------------- #

class Bridge:
    """The exactly-once loop of LLD-07 §2. `source` provides read_since,
    `target` provides emit, `cursor` persists last_seq. `subscriptions` and
    `source_node_id` come from config."""

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

    def _match(self, name):
        if name in self.by_event:
            return self.by_event[name]
        return self.by_event.get(name.rsplit(".", 1)[-1])

    def idem_key(self, seq):
        # deterministic from the source event (LLD-07 §2): a replay of the same
        # source seq produces the same key and collapses on the target.
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

    def run_once(self):
        """One pull→deliver→advance pass. Returns the number of events
        delivered. The cursor advances AFTER each confirmed delivery so a crash
        mid-pass replays only undelivered (or idempotently-absorbed) events."""
        last = self.cursor.load()
        delivered = 0
        for fact in self.source.read_since(last):
            sub = self._match(fact["name"])
            if sub is None:
                # not subscribed — still advance past it so we do not rescan it
                # forever (the cursor tracks "seen", the filter tracks "wanted").
                self.cursor.save(fact["seq"])
                continue
            payload = self._target_payload(sub, fact)
            key = self.idem_key(fact["seq"])
            self.target.emit(sub["target_event"], payload, key)
            # delivery confirmed (emit returned without GovernedError) — only
            # now is it safe to advance the cursor past this seq.
            self.cursor.save(fact["seq"])
            delivered += 1
        return delivered


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
    source_entities = [s.get("source_entity", "Lead") for s in subs]
    source = MCPSource(MCPClient(cfg["source_url"], cfg["source_token"]), source_entities)
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
            # a poison event: the target rejected it on its merits. Do not
            # advance past it; log and let the next poll retry (at-least-once).
            print(f"[bridge] delivery rejected: {ex}", flush=True)
        time.sleep(cfg["poll_sec"])


if __name__ == "__main__":
    main()
