#!/usr/bin/env python3
"""Acceptance tests for the federation bridge (LLD-07 §7 tests 1-4, LLD-08 §5
tests 2/7 for the read_events transport).

No live stack (the worker-test convention): the source and target contracts are
modelled in-process by fakes that mirror the real MCP semantics —
  * FakeTarget.emit reproduces emit_event's mandatory idempotency (ByIdemKey):
    a repeat with the same idempotency_key collapses to the first fact and does
    NOT re-run the `on event` subscriber (the exact behaviour proven against the
    real engine in internal/mcp/emit_event_test.go: TestEmitIdempotent).
  * FakeTarget enforces the least-privilege grant of the BridgeEmitter role
    (emit only) the way the Permission Engine does.
  * FakeSource.read_since serves the abstract Bridge.source contract directly
    (Bridge core logic tests 1-4 below) — it does not go through MCPSource, so
    it is agnostic to the underlying transport (read_journal-scan before
    LLD-08, read_events after).
  * FakeReadEventsClient (below) models the read_events MCP tool itself, to
    test MCPSource's pagination and free-standing-emit mapping — the part of
    the bridge that DID change with LLD-08 (see agent/kalita/QUESTIONS-LLD.md
    for the closed core gap this replaced).

Run:  python -m pytest test_bridge.py -v
  or: python -m pytest workers/bridge/test_bridge.py
"""
import os
import sys

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

import pytest  # noqa: E402
from bridge import Bridge, GovernedError, MCPSource  # noqa: E402


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


# --------------------------------------------------------------------------- #
# MCPSource / read_events transport (LLD-08 §1/§4, §5 tests 2 and 7).
# --------------------------------------------------------------------------- #

class FakeReadEventsClient:
    """Models the read_events MCP tool: paginated, since-cursor, node-wide.
    `events` is the full ordered stream the source node would return (already
    scoped to the actor's tail grant, exactly as the real engine does BEFORE
    the tool ever runs — see internal/engine/read_events.go). `page_limit`
    caps how many events one call returns, forcing MCPSource to page even
    when it requests a larger limit, so the test proves the pagination loop
    itself, not just a single lucky call."""

    def __init__(self, events, page_limit=500):
        self.events = events
        self.page_limit = page_limit
        self.calls = []

    def call(self, name, arguments):
        assert name == "read_events", f"MCPSource must call read_events, called {name}"
        self.calls.append(dict(arguments))
        since = arguments.get("since", 0)
        limit = min(arguments.get("limit", 500), self.page_limit)
        # `since` is INCLUSIVE (LLD-08 §1: next_since = seq of the last event
        # examined + 1, and the client passes it straight back) — mirrors the
        # real engine's read_events (internal/engine/read_events.go).
        page = [e for e in self.events if e["seq"] >= since][:limit]
        next_since = page[-1]["seq"] + 1 if page else since
        return {"events": page, "next_since": next_since}


def _fact_event(seq, name, payload, entity="Lead", record_id="lead-1"):
    return {"seq": seq, "kind": "event.emitted",
            "subject": {"entity": entity, "record_id": record_id},
            "payload": {"name": name, "payload": payload}}


# Test 7 companion: MCPSource pages through read_events using next_since until
# the cursor stops advancing, draining the whole stream in seq order — the
# same guarantee LLD-08 test 5 (TestReadEventsPagination) proves at the Go
# layer, checked here at the transport the bridge actually drives.
def test_source_reads_via_read_events_and_paginates():
    events = [
        _fact_event(1, "site.LeadCaptured", {"name": "Ada"}, record_id="lead-1"),
        _fact_event(2, "site.LeadCaptured", {"name": "Grace"}, record_id="lead-2"),
        _fact_event(3, "site.LeadCaptured", {"name": "Karen"}, record_id="lead-3"),
    ]
    client = FakeReadEventsClient(events, page_limit=1)  # force 3+ pages
    source = MCPSource(client)

    facts = source.read_since(0)

    assert [f["seq"] for f in facts] == [1, 2, 3]
    assert [f["payload"]["name"] for f in facts] == ["Ada", "Grace", "Karen"]
    assert len(client.calls) >= 3, "page_limit=1 over 3 events must take multiple calls"
    assert client.calls[0]["kinds"] == ["event.emitted"]
    assert client.calls[0]["since"] == 1  # last_seq=0 (nothing delivered yet) -> since=1 (inclusive)


# Test 2 (LLD-08 §5, closing the LLD-07 §8 gap): a free-standing emit_event
# (empty subject) is passed through by MCPSource with entity/record_id == "" —
# not dropped and not an error. Before LLD-08 this event was unreachable (the
# old transport walked read_journal per RECORD, and a free-standing emit has
# none); read_events makes it visible node-wide.
def test_source_sees_freestanding_emit():
    events = [
        _fact_event(1, "site.LeadCaptured", {"name": "Ada"}, record_id="lead-1"),
        _fact_event(2, "site.LeadCaptured", {"name": "Grace"}, entity="", record_id=""),
    ]
    source = MCPSource(FakeReadEventsClient(events))

    facts = source.read_since(0)

    assert len(facts) == 2
    freestanding = facts[1]
    assert freestanding["seq"] == 2
    assert freestanding["entity"] == "" and freestanding["record_id"] == ""
    assert freestanding["payload"] == {"name": "Grace"}


# Pagination must not stop early just because one page's VISIBLE events are
# empty — the underlying cursor can advance past a run of kinds this actor
# does not tail (kind-filtered server-side); only a next_since that stops
# moving means the stream is exhausted.
# Regression: the bridge's cursor convention is "last confirmed seq"
# (exclusive), but read_events' `since` is inclusive — read_since(last_seq)
# must not re-return the event AT exactly last_seq (already delivered) nor
# skip the one right after it.
def test_source_read_since_does_not_replay_or_skip_the_boundary_event():
    events = [
        _fact_event(5, "site.LeadCaptured", {"name": "Ada"}, record_id="lead-5"),
        _fact_event(6, "site.LeadCaptured", {"name": "Grace"}, record_id="lead-6"),
    ]
    source = MCPSource(FakeReadEventsClient(events))

    facts = source.read_since(5)  # seq 5 already delivered

    assert [f["seq"] for f in facts] == [6], "must return only the event AFTER last_seq, no replay, no gap"


def test_source_pagination_continues_through_filtered_pages():
    class SkippyClient:
        def call(self, name, arguments):
            since = arguments["since"]
            if since == 1:  # read_since(0) -> first call asks since=1 (inclusive)
                return {"events": [], "next_since": 50}  # a run of non-event.emitted kinds
            if since == 50:
                return {"events": [_fact_event(51, "site.LeadCaptured", {"name": "Ada"})],
                        "next_since": 52}
            return {"events": [], "next_since": since}  # exhausted: cursor stops advancing

    facts = MCPSource(SkippyClient()).read_since(0)

    assert [f["seq"] for f in facts] == [51]


# --------------------------------------------------------------------------- #
# LLD-21 PR-1 — bridge as subscribe.py's second consumer (test 4:
# TestBridgeOnHarnessUnchanged). Every test ABOVE this line is unmodified and
# still green on the Subscriber-backed Bridge — that IS the regression proof.
# This test proves the one thing that changed for the better (§4): a governed
# (permission-denied) rejection from the target no longer wedges the cursor
# forever; it is absorbed (dead-lettered — best-effort, no emit grant wired
# for bridge itself yet, see bridge.py's Bridge.__init__ comment) and the
# cursor advances past it, so the NEXT fact in the stream still delivers.
# --------------------------------------------------------------------------- #

def test_bridge_governed_rejection_no_longer_wedges_the_cursor():
    target = FakeTarget(allowed_events={"LeadMirrored"})
    poison_fact = {
        "seq": 7, "name": "site.LeadCaptured", "record_id": "lead-1",
        "payload": {"name": "Ada Lovelace"},
    }
    ok_fact = {
        "seq": 8, "name": "site.Signup", "record_id": "lead-2",
        "payload": {"name": "Grace Hopper"},
    }
    subs = [
        # poison: target_event NOT in the role's grant (PERMISSION_DENIED)
        {"event": "site.LeadCaptured", "target_event": "NotGranted", "field_map": {"name": "name"}},
        {"event": "site.Signup", "target_event": "LeadMirrored", "field_map": {"name": "name"}},
    ]
    source = FakeSource([poison_fact, ok_fact])
    bridge = make_bridge(source, target, MemCursor())
    bridge.by_event = {}  # rebuild the match table for this test's two subs
    for sub in subs:
        bridge.by_event[sub["event"]] = sub
        bridge.by_event[sub["event"].rsplit(".", 1)[-1]] = sub

    delivered = bridge.run_once()  # must NOT raise — the poison event is absorbed

    assert delivered == 2, "both facts were handed to the target (one dead-lettered, one delivered)"
    assert len(target.mirrors) == 1, "only the granted event actually produced an effect"
    assert target.mirrors[0]["name"] == "Grace Hopper"
    assert bridge.cursor.load() == 8, "cursor advanced past the poison AND the good event"


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-v"]))
