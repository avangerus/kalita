#!/usr/bin/env python3
"""Acceptance tests for the durable subscriber library (LLD-21 §8 tests 1-3).
Test 4 (TestBridgeOnHarnessUnchanged) lives in workers/bridge/test_bridge.py —
the existing bridge suite passing unchanged IS the regression proof, plus one
additive test there wiring the new dead-letter-and-advance behaviour through
Bridge specifically.

No live stack (the worker-test convention, same as workers/bridge/test_bridge.py):
source/cursor/dead-letter are modelled by in-process fakes that mirror the real
MCP contracts (mandatory idempotency via a key->result dict, exactly like
FakeTarget in test_bridge.py).

Run:  python -m pytest test_subscribe.py -v
  or: python -m pytest workers/lib/test_subscribe.py
"""
import os
import sys

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

import pytest  # noqa: E402
from subscribe import Act, DeadLetter, GovernedError, Subscriber  # noqa: E402


# --------------------------------------------------------------------------- #
# Fakes.
# --------------------------------------------------------------------------- #

class FakeSource:
    """Serves read_since like subscribe.EventSource would, from a fixed list
    of facts: {seq, name, payload, entity, record_id}."""

    def __init__(self, facts):
        self._facts = facts

    def read_since(self, last_seq):
        return sorted((f for f in self._facts if f["seq"] > last_seq),
                     key=lambda f: f["seq"])


class MemCursor:
    """In-memory cursor with an optional one-shot failure injected AFTER a
    save is requested, to simulate a crash between the handler's action and
    cursor persistence."""

    def __init__(self):
        self.seq = 0
        self.fail_on_save_once = False

    def load(self):
        return self.seq

    def save(self, seq):
        if self.fail_on_save_once:
            self.fail_on_save_once = False
            raise RuntimeError("simulated crash after handling, before cursor persist")
        self.seq = seq


class IdemStore:
    """Models a node's mandatory idempotency (ByIdemKey, LLD-01 §5): the same
    key applied twice produces the effect ONCE. `effects` is the resulting
    ledger of seqs that actually took effect (for asserting exactly-once)."""

    def __init__(self):
        self.seen = set()
        self.effects = []

    def apply(self, key, seq):
        if key in self.seen:
            return False  # collapsed — no new effect
        self.seen.add(key)
        self.effects.append(seq)
        return True


def make_fact(seq, name="Demo.Thing", payload=None):
    return {"seq": seq, "name": name, "payload": payload or {}, "entity": "", "record_id": ""}


def fake_sleep_recorder():
    calls = []

    def sleep(sec):
        calls.append(sec)

    return sleep, calls


# --------------------------------------------------------------------------- #
# Test 1 — TestSubscribeExactlyOnceEffect
# --------------------------------------------------------------------------- #

def test_subscribe_exactly_once_effect():
    fact = make_fact(seq=7)
    source = FakeSource([fact])
    cursor = MemCursor()
    store = IdemStore()

    def handler(f, act):
        store.apply(act.idem_key(), f["seq"])

    sub = Subscriber(source, cursor, subscription_id="sub-1", events=None, handler=handler)

    # crash: handler runs (effect applied) but cursor.save raises before persisting
    cursor.fail_on_save_once = True
    with pytest.raises(RuntimeError):
        sub.run_once()
    assert store.effects == [7], "the effect happened exactly once despite the crash"
    assert cursor.load() == 0, "cursor never advanced past the crash"

    # restart: re-read from cursor=0, handler runs again for seq 7, but the
    # deterministic key collapses the duplicate — no second effect.
    sub.run_once()
    assert store.effects == [7], "restart-replay must not produce a second effect"
    assert cursor.load() == 7, "cursor now advances since the fact resolved cleanly"

    # cursor reset to 0 entirely (loss, not just a crash) -> reprocessing from
    # scratch on a FRESH Subscriber instance still must not double the effect.
    cursor2 = MemCursor()
    sub2 = Subscriber(source, cursor2, subscription_id="sub-1", events=None, handler=handler)
    sub2.run_once()
    assert store.effects == [7], "cursor loss + full replay must not create a second effect"
    assert cursor2.load() == 7


# --------------------------------------------------------------------------- #
# Test 2 — TestSubscribeTransientRetries
# --------------------------------------------------------------------------- #

def test_subscribe_transient_retries():
    fact = make_fact(seq=3)
    source = FakeSource([fact])
    cursor = MemCursor()
    sleep, sleep_calls = fake_sleep_recorder()

    attempts = {"n": 0}

    def handler(f, act):
        attempts["n"] += 1
        if attempts["n"] < 3:
            raise ConnectionError("node temporarily unavailable")
        # third attempt: the node recovered, delivery succeeds

    sub = Subscriber(source, cursor, subscription_id="sub-2", events=None,
                     handler=handler, retry_sleep_sec=5, sleep=sleep)

    handled = sub.run_once()

    assert attempts["n"] == 3, "handler retried the SAME fact until it succeeded"
    assert sleep_calls == [5, 5], "backed off between the two failed attempts, none after success"
    assert cursor.load() == 3, "cursor advances once delivery finally succeeds"
    assert handled == 1


def test_subscribe_transient_does_not_advance_cursor_while_failing():
    """Cursor must stay untouched for as long as the fact keeps failing
    transiently — the cursor is never a stand-in for confirmation."""
    fact = make_fact(seq=9)
    source = FakeSource([fact])
    cursor = MemCursor()
    sleep, sleep_calls = fake_sleep_recorder()

    def always_fails(f, act):
        raise TimeoutError("node still unavailable")

    sub = Subscriber(source, cursor, subscription_id="sub-2b", events=None,
                     handler=always_fails, retry_sleep_sec=1, sleep=sleep)

    # bound the retry loop for the test: stop it after a few iterations by
    # having the injected sleep raise once it's been called enough times.
    def bounded_sleep(sec):
        sleep(sec)
        if len(sleep_calls) >= 4:
            raise KeyboardInterrupt("test bound: stop the infinite retry")

    sub.sleep = bounded_sleep
    with pytest.raises(KeyboardInterrupt):
        sub.run_once()
    assert cursor.load() == 0, "never advances while the fact keeps failing transiently"
    assert len(sleep_calls) == 4


# --------------------------------------------------------------------------- #
# Test 3 — TestSubscribeGovernedDeadLetters
# --------------------------------------------------------------------------- #

class FakeDeadLetterClient:
    """Models the emit_event MCP call DeadLetter.report() makes."""

    def __init__(self):
        self.calls = []

    def call(self, name, arguments):
        assert name == "emit_event"
        self.calls.append(arguments)
        return {"name": arguments["name"], "payload": arguments["payload"], "seq": len(self.calls)}


def test_subscribe_governed_dead_letters():
    poison = make_fact(seq=5, name="Poison.Thing")
    ok = make_fact(seq=6, name="Ok.Thing")
    source = FakeSource([poison, ok])
    cursor = MemCursor()
    dl_client = FakeDeadLetterClient()
    dead_letter = DeadLetter(dl_client)

    handler_calls = []

    def handler(f, act):
        handler_calls.append(f["seq"])
        if f["name"] == "Poison.Thing":
            raise GovernedError("PERMISSION_DENIED", "actor may not act on Poison.Thing")
        # Ok.Thing succeeds

    sub = Subscriber(source, cursor, subscription_id="sub-3", events=None,
                     handler=handler, dead_letter=dead_letter)

    handled = sub.run_once()

    # the poison fact was handed to handler exactly once — NOT retried forever
    # (unlike a transient failure) — and the pipeline still reached the next fact.
    assert handler_calls == [5, 6]
    assert handled == 2

    # cursor advanced PAST the poison fact (§4, I-3: no silent permanent stall)
    # all the way to the last fact.
    assert cursor.load() == 6

    # exactly one dead-letter emitted, carrying subscription/seq/error, with
    # its own idempotent key.
    assert len(dl_client.calls) == 1
    call = dl_client.calls[0]
    assert call["name"] == "subscription.failed"
    assert call["payload"]["subscription_id"] == "sub-3"
    assert call["payload"]["source_seq"] == 5
    assert call["payload"]["error"]["code"] == "PERMISSION_DENIED"
    assert call["idempotency_key"] == "sub-3|5|subscription.failed"


def test_subscribe_dead_letter_failure_still_advances_cursor():
    """If reporting the dead-letter ITSELF fails (e.g. no emit grant for
    subscription.failed yet — a real gap until PR-2/3 wires governance for
    it), the cursor must still advance past the poison fact. The dead-letter
    is a best-effort audit signal, not the mechanism that unsticks the
    pipeline (that's the cursor advance itself, §4)."""
    poison = make_fact(seq=1, name="Poison.Thing")
    source = FakeSource([poison])
    cursor = MemCursor()

    class BrokenDeadLetterClient:
        def call(self, name, arguments):
            raise GovernedError("PERMISSION_DENIED", "actor may not emit subscription.failed")

    dead_letter = DeadLetter(BrokenDeadLetterClient())

    def handler(f, act):
        raise GovernedError("VALIDATION_FAILED", "bad record")

    sub = Subscriber(source, cursor, subscription_id="sub-3b", events=None,
                     handler=handler, dead_letter=dead_letter)

    handled = sub.run_once()

    assert handled == 1
    assert cursor.load() == 1, "cursor advances even though the dead-letter emission itself failed"


# --------------------------------------------------------------------------- #
# events filter: name-set / predicate forms, and Act key derivation.
# --------------------------------------------------------------------------- #

def test_events_filter_by_name_set_matches_canonical_and_short_form():
    facts = [make_fact(1, name="pack.Wanted"), make_fact(2, name="Unwanted")]
    source = FakeSource(facts)
    cursor = MemCursor()
    seen = []

    def handler(f, act):
        seen.append(f["seq"])

    sub = Subscriber(source, cursor, subscription_id="sub-4", events={"Wanted"}, handler=handler)
    handled = sub.run_once()

    assert seen == [1], "matched by the short suffix after the last '.'"
    assert handled == 1
    assert cursor.load() == 2, "the unwanted fact still advances the cursor (seen, not reprocessed)"


def test_act_idem_key_namespaces_by_subscription_and_step():
    act = Act("sub-5", 42)
    assert act.idem_key() == "sub-5|42"
    assert act.idem_key("step-a") == "sub-5|42|step-a"
    assert act.idem_key("step-b") == "sub-5|42|step-b"


if __name__ == "__main__":
    sys.exit(pytest.main([__file__, "-v"]))
