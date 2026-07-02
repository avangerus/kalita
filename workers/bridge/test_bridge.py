#!/usr/bin/env python3
"""Acceptance tests for the federation bridge (LLD-07 §7, tests 1-4).

No live stack (the worker-test convention): the source and target contracts are
modelled in-process by fakes that mirror the real MCP semantics —
  * FakeTarget.emit reproduces emit_event's mandatory idempotency (ByIdemKey):
    a repeat with the same idempotency_key collapses to the first fact and does
    NOT re-run the `on event` subscriber (the exact behaviour proven against the
    real engine in internal/mcp/emit_event_test.go: TestEmitIdempotent).
  * FakeTarget enforces the least-privilege grant of the BridgeEmitter role
    (emit only) the way the Permission Engine does.
  * FakeSource.read_since serves the read_journal-since contract the bridge
    needs (the real per-record read_journal + node-wide-since gap is recorded in
    agent/kalita/QUESTIONS-LLD.md; the source shape here is the contract, not a
    workaround of it).

Run:  python -m pytest test_bridge.py -v
  or: python -m pytest workers/bridge/test_bridge.py
"""
import os
import sys

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

import pytest  # noqa: E402
from bridge import Bridge, GovernedError  # noqa: E402


# --------------------------------------------------------------------------- #
# Fakes that model the real node contracts.
# --------------------------------------------------------------------------- #

class FakeSource:
    """Holds source event.emitted facts; serves read_since like the bridge's
    MCPSource would. Facts are (seq, name, payload, record_id)."""

    def __init__(self, facts):
        self._facts = facts

    def read_since(self, last_seq):
        return sorted(
            ({"seq": f["seq"], "name": f["name"], "payload": f["payload"],
              "entity": f.get("entity", "Lead"), "record_id": f["record_id"]}
             for f in self._facts if f["seq"] > last_seq),
            key=lambda f: f["seq"])


class FakeTarget:
    """Models the target node's emit_event: least-privilege grant check +
    mandatory idempotency + the `on event` subscriber that writes the mirror
    record with origin. `mirrors` is the resulting record table."""

    def __init__(self, allowed_events):
        self.allowed_events = set(allowed_events)
        self._by_key = {}      # idempotency_key -> emitted fact (ByIdemKey)
        self.mirrors = []      # records created by the on-event subscriber
        self._seq = 0
        self.emit_calls = 0

    def emit(self, target_event, payload, idem_key):
        self.emit_calls += 1
        # least privilege (LLD-07 §7 test 4): the BridgeEmitter role may emit
        # ONLY its granted events; anything else is PERMISSION_DENIED.
        if target_event not in self.allowed_events:
            raise GovernedError("PERMISSION_DENIED",
                                f"role BridgeEmitter may not emit {target_event}")
        # mandatory idempotency (LLD-07 §2): a repeat key returns the prior fact
        # and does NOT re-dispatch the subscriber (no duplicate mirror).
        if idem_key in self._by_key:
            return self._by_key[idem_key]
        self._seq += 1
        fact = {"name": target_event, "payload": payload, "seq": self._seq}
        self._by_key[idem_key] = fact
        # subscriber: `on event LeadMirrored: create MirroredLead(... origin ...)`
        self.mirrors.append(dict(payload))
        return fact

    def call(self, tool, arguments):
        """A generic tool call the way an MCP client would issue it — used by
        the least-privilege test to prove a non-emit verb is denied."""
        if tool == "emit_event":
            return self.emit(arguments["name"], arguments.get("payload", {}),
                             arguments["idempotency_key"])
        raise GovernedError("PERMISSION_DENIED",
                            f"role BridgeEmitter may not call {tool}")


class MemCursor:
    """In-memory cursor with an optional one-shot failure injected AFTER a set
    seq is reached, to simulate a crash between delivery and persistence."""

    def __init__(self):
        self.seq = 0
        self.fail_on_save_once = False

    def load(self):
        return self.seq

    def save(self, seq):
        if self.fail_on_save_once:
            self.fail_on_save_once = False
            raise RuntimeError("simulated crash after delivery, before cursor persist")
        self.seq = seq


LEAD_CAPTURED = {
    "seq": 7, "name": "site.LeadCaptured", "record_id": "lead-1",
    "payload": {"name": "Ada Lovelace", "company": "Analytical Engines", "email": "ada@ae.io"},
}

SUBSCRIPTIONS = [{
    "event": "site.LeadCaptured",
    "target_event": "LeadMirrored",
    "source_entity": "Lead",
    "field_map": {"name": "name", "company": "company", "email": "email"},
}]


def make_bridge(source, target, cursor):
    return Bridge(source, target, cursor, SUBSCRIPTIONS, source_node_id="node-a")


# --------------------------------------------------------------------------- #
# Test 1 — deliver once + origin correctly stamped.
# --------------------------------------------------------------------------- #

def test_bridge_delivers_once_with_origin():
    target = FakeTarget(allowed_events={"LeadMirrored"})
    bridge = make_bridge(FakeSource([LEAD_CAPTURED]), target, MemCursor())

    delivered = bridge.run_once()

    assert delivered == 1
    assert len(target.mirrors) == 1
    m = target.mirrors[0]
    assert m["name"] == "Ada Lovelace"
    assert m["company"] == "Analytical Engines"
    assert m["email"] == "ada@ae.io"
    # origin = {source node, source record id} (LLD-07 §3)
    assert m["origin_node"] == "node-a"
    assert m["origin_id"] == "lead-1"


# --------------------------------------------------------------------------- #
# Test 2 — replaying the source from seq=0 does not duplicate.
# --------------------------------------------------------------------------- #

def test_bridge_exactly_once_on_replay():
    target = FakeTarget(allowed_events={"LeadMirrored"})
    source = FakeSource([LEAD_CAPTURED])

    # first pass delivers
    make_bridge(source, target, MemCursor()).run_once()
    assert len(target.mirrors) == 1

    # a SECOND bridge instance with a FRESH cursor (seq=0) re-reads the whole
    # source journal — the deterministic idem key collapses the duplicate.
    make_bridge(source, target, MemCursor()).run_once()
    assert len(target.mirrors) == 1, "replay from seq=0 must not create a second mirror"
    assert target.emit_calls == 2, "the bridge did re-attempt delivery (idempotency, not skipping)"


# --------------------------------------------------------------------------- #
# Test 3 — crash before the cursor advances does not duplicate on restart.
# --------------------------------------------------------------------------- #

def test_bridge_crash_before_cursor_advance():
    target = FakeTarget(allowed_events={"LeadMirrored"})
    source = FakeSource([LEAD_CAPTURED])
    cursor = MemCursor()
    cursor.fail_on_save_once = True  # crash right after the emit, before persist

    with pytest.raises(RuntimeError):
        make_bridge(source, target, cursor).run_once()

    # delivery happened, but the cursor never advanced (still 0)
    assert len(target.mirrors) == 1
    assert cursor.load() == 0

    # restart: the bridge re-reads from 0 and re-delivers with the SAME key —
    # no duplicate mirror on the target.
    make_bridge(source, target, cursor).run_once()
    assert len(target.mirrors) == 1, "restart after crash must not double-deliver"
    assert cursor.load() == LEAD_CAPTURED["seq"], "cursor advances once delivery is confirmed"


# --------------------------------------------------------------------------- #
# Test 4 — least privilege: the bridge emit-actor may ONLY emit its event.
# --------------------------------------------------------------------------- #

def test_bridge_least_privilege():
    target = FakeTarget(allowed_events={"LeadMirrored"})

    # the granted emit succeeds
    assert target.call("emit_event", {
        "name": "LeadMirrored", "payload": {"name": "x"}, "idempotency_key": "bridge|node-a|1"})

    # any other verb (create/query/update/act) is denied by the Permission Engine
    for tool in ("create_record", "query", "update_record", "act", "read_journal"):
        with pytest.raises(GovernedError) as ei:
            target.call(tool, {"entity": "MirroredLead"})
        assert ei.value.code == "PERMISSION_DENIED"

    # emitting a NON-granted event is denied too
    with pytest.raises(GovernedError) as ei:
        target.call("emit_event", {
            "name": "SomethingElse", "payload": {}, "idempotency_key": "bridge|node-a|2"})
    assert ei.value.code == "PERMISSION_DENIED"


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-v"]))
