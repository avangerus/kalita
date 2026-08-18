#!/usr/bin/env python3
"""test_walker_golden.py — the Python half of the differential walker↔engine
contract. The SAME corpus (walker_golden.json) is evaluated by the engine's
real evalWhere in internal/engine/walker_golden_test.go; this test runs it
through synthesizer.eval_guard_subset. Divergence fails one of the two suites
— the walker can never silently drift from the guard semantics.
"""
import json
import os
import sys

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)
import synthesizer  # noqa: E402

CORPUS = os.path.join(_HERE, "walker_golden.json")


def _load_cases():
    with open(CORPUS, "r", encoding="utf-8") as f:
        corpus = json.load(f)
    assert corpus["cases"], "corpus must not be empty"
    return corpus["cases"]


def test_walker_corpus_matches_want():
    for i, c in enumerate(_load_cases()):
        got = synthesizer.eval_guard_subset(c["expr"], c["record"])
        assert got == c["want"], (
            "case %d: eval(%r, %r) = %r, want %r"
            % (i, c["expr"], c["record"], got, c["want"]))


def test_walker_returns_none_outside_subset():
    # computed/aggregate/path constructs are OUTSIDE the subset: the walker
    # must say "unknown", never guess.
    assert synthesizer.eval_guard_subset("amount > count(Other where x = $self)", {"amount": 1}) is None
    assert synthesizer.eval_guard_subset("project.owner = $me", {"amount": 1}) is None
    assert synthesizer.eval_guard_subset("days_since(created_at) > 3", {"amount": 1}) is None
    assert synthesizer.eval_guard_subset("amount in [1, 2]", {"amount": 1}) is None
    assert synthesizer.eval_guard_subset("amount >", {"amount": 1}) is None


def test_walker_routes_live_records_through_generated_guards():
    spec = synthesizer.build_ap_spec({
        "approval_authority": "director",
        "amount_thresholds": "up to 100k manager, up to 1M CFO, above CEO",
    })
    traces = synthesizer.walk_ladder(spec, [
        {"id": "i1", "values": {"amount": 80000}},
        {"id": "i2", "values": {"amount": 500000}},
        {"id": "i3", "values": {"amount": 1500000}},
    ])
    assert [t["tier_role"] for t in traces] == ["Manager", "CFO", "CEO"], traces
    assert all(t["ok"] for t in traces)


def test_walker_unknown_when_guard_outside_subset():
    spec = synthesizer.build_ap_spec({"approval_authority": "director"})
    # doctor the generated guard to an out-of-subset expression: the walker
    # must mark the route unknown, not pass it
    for wf in spec["workflows"]:
        for t in wf["transitions"]:
            if t.get("requires_approval"):
                t["when"] = "amount > count(Finding where x = $self)"
    traces = synthesizer.walk_ladder(spec, [{"id": "i1", "values": {"amount": 100}}])
    assert traces[0]["ok"] is False and traces[0]["tier_role"] is None, traces
