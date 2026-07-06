#!/usr/bin/env python3
"""Tests for the datapulse exact-duplicate matcher (ENH-04), workers/
connectors/exact_match.py.

No live kalita node required: exact_match.mcp() is monkeypatched with an
in-process FakeNode that mimics just enough of the real node's `query` and
`create_record` MCP tools to prove the contract — same style as
workers/connectors/test_excel_ingest.py's FakeNode:
  - create_record dedups by idempotency_key (mirrors internal/eventstore
    MemStore.Append's byIdemKey short-circuit) so the idempotency test
    proves an actual zero-duplicate outcome, not merely "the same key was
    passed".
  - query returns {"records": [...]} paged by limit/offset and filtered by
    an exact-match `filter` map, mirroring internal/engine records.go's
    matchesFilter and internal/mcp/tools.go's `query` tool shape.
  - every call is logged (name, arguments) for the governance test.

Run:  python -m pytest test_exact_match.py -v
  or: python test_exact_match.py
"""
import os
import sys
import unittest
import uuid
from unittest import mock

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

# exact_match's module-level `TOKEN = get_token()` must not try to bootstrap
# against a real node at import time — see test_excel_ingest.py's identical
# comment for why setting KALITA_TOKEN (bootstrap.get_token()'s first, no-
# network check) is the robust fix here, not stubbing sys.modules["bootstrap"].
os.environ.setdefault("KALITA_TOKEN", "test-token")

import exact_match as em  # noqa: E402


# ===========================================================================
# FakeNode — an in-process stand-in for the MCP layer
# ===========================================================================

class FakeNode:
    """Mimics `query` (paged, filtered) and `create_record` (idempotency_key
    dedup) closely enough to prove the matcher's contract against a fake,
    not just "the same arguments were passed"."""

    def __init__(self, master_records=None):
        # entity -> {id: values}; master_records seeds MasterRecord directly
        # (this worker never creates MasterRecord, so tests seed it as
        # pre-existing data rather than going through excel_ingest).
        self.records = {"MasterRecord": dict(master_records or {})}
        self.idem = {}   # idempotency_key -> id
        self.calls = []  # (name, arguments) — the governance test's log

    def __call__(self, name, arguments):
        self.calls.append((name, dict(arguments)))
        if name == "query":
            return self._query(arguments)
        if name == "create_record":
            return self._create(arguments)
        raise AssertionError(f"unexpected mcp call: {name} {arguments}")

    def _query(self, args):
        entity = args["entity"]
        filt = args.get("filter") or {}
        offset = args.get("offset") or 0
        limit = args.get("limit")
        pool = self.records.get(entity, {})
        rows = [{"id": rid, "entity": entity, "values": values}
                for rid, values in pool.items()
                if all(values.get(k) == v for k, v in filt.items())]
        rows.sort(key=lambda r: r["id"])  # deterministic paging order
        page = rows[offset:offset + limit] if limit else rows[offset:]
        return {"records": page, "def_version": 1}, False

    def _create(self, args):
        entity = args["entity"]
        key = args.get("idempotency_key")
        if key and key in self.idem:
            rid = self.idem[key]
            return {"id": rid, "values": self.records[entity][rid]}, False
        rid = str(uuid.uuid4())
        self.records.setdefault(entity, {})[rid] = dict(args["values"])
        if key:
            self.idem[key] = rid
        return {"id": rid, "values": self.records[entity][rid]}, False

    def match_groups(self):
        return self.records.get("MatchGroup", {})


def _mr(name, email, extra=None):
    values = {"raw": {"name": name, "email": email}, "display_name": name, "status": "Draft"}
    if extra:
        values["raw"].update(extra)
    return values


KEY_COLUMNS = ["name", "email"]


# ===========================================================================
# (a) happy path — triple duplicate + pair + several uniques
# ===========================================================================

class TestHappyPath(unittest.TestCase):
    def test_triple_and_pair_groups_no_singletons(self):
        master = {
            # triple duplicate — exact same name+email
            "t1": _mr("Acme LLC", "ops@acme.example"),
            "t2": _mr("Acme LLC", "ops@acme.example"),
            "t3": _mr("Acme LLC", "ops@acme.example"),
            # pair duplicate
            "p1": _mr("Globex Inc", "hello@globex.example"),
            "p2": _mr("Globex Inc", "hello@globex.example"),
            # singletons — must NOT produce any group
            "s1": _mr("Initech", "info@initech.example"),
            "s2": _mr("Umbrella Corp", "contact@umbrella.example"),
        }
        node = FakeNode(master)
        with mock.patch.object(em, "mcp", node):
            result = em.run(KEY_COLUMNS)

        self.assertEqual(result["records_scanned"], 7)
        self.assertEqual(result["groups_created"], 2, "exactly one group of 3 and one group of 2")

        groups = list(node.match_groups().values())
        self.assertEqual(len(groups), 2)

        by_size = sorted(groups, key=lambda g: len(g["records"]))
        pair, triple = by_size[0], by_size[1]

        self.assertEqual(sorted(pair["records"]), sorted(["p1", "p2"]))
        self.assertEqual(pair["score"], 1.0)
        self.assertNotIn("status", pair, "status must be left to the model default (New)")

        self.assertEqual(sorted(triple["records"]), sorted(["t1", "t2", "t3"]))
        self.assertEqual(triple["score"], 1.0)
        self.assertNotIn("status", triple)

        # no group contains any singleton id
        grouped_ids = {rid for g in groups for rid in g["records"]}
        self.assertNotIn("s1", grouped_ids)
        self.assertNotIn("s2", grouped_ids)


# ===========================================================================
# (b) idempotency — rerun over unchanged data, zero duplicate MatchGroups
# ===========================================================================

class TestIdempotency(unittest.TestCase):
    def test_rerun_creates_zero_duplicate_match_groups(self):
        master = {
            "t1": _mr("Acme LLC", "ops@acme.example"),
            "t2": _mr("Acme LLC", "ops@acme.example"),
            "t3": _mr("Acme LLC", "ops@acme.example"),
            "p1": _mr("Globex Inc", "hello@globex.example"),
            "p2": _mr("Globex Inc", "hello@globex.example"),
            "s1": _mr("Initech", "info@initech.example"),
        }
        node = FakeNode(master)  # ONE fake node/journal shared across both runs
        with mock.patch.object(em, "mcp", node):
            first = em.run(KEY_COLUMNS)
            groups_after_first = set(node.match_groups())
            second = em.run(KEY_COLUMNS)
            groups_after_second = set(node.match_groups())

        self.assertEqual(first["groups_created"], 2)
        self.assertEqual(second["groups_created"], 2, "second run still PROPOSES 2 groups (calls made)")
        self.assertEqual(groups_after_first, groups_after_second,
                          "re-running over unchanged data must create ZERO new MatchGroup ids")
        self.assertEqual(len(node.match_groups()), 2,
                          "two runs over identical data must still total exactly 2 MatchGroups")
        self.assertEqual(sorted(first["group_ids"]), sorted(second["group_ids"]),
                          "the second run must dedup to the SAME group ids, not register new ones")

        create_calls = [a for n, a in node.calls if n == "create_record"]
        self.assertEqual(len(create_calls), 4,
                          "2 runs x 2 create_record attempts = 4 calls total, even though only 2 "
                          "MatchGroups actually exist -- proves the dedup happened at create time")


# ===========================================================================
# (c) governance — never touches what MatchAgent isn't granted
# ===========================================================================

class TestGovernance(unittest.TestCase):
    def test_never_writes_forbidden_entities_or_calls(self):
        master = {
            "t1": _mr("Acme LLC", "ops@acme.example"),
            "t2": _mr("Acme LLC", "ops@acme.example"),
            "p1": _mr("Globex Inc", "hello@globex.example"),
            "p2": _mr("Globex Inc", "hello@globex.example"),
            "s1": _mr("Initech", "info@initech.example"),
        }
        node = FakeNode(master)
        with mock.patch.object(em, "mcp", node):
            em.run(KEY_COLUMNS)

        self.assertTrue(node.calls, "no calls captured -- test fixture is broken")

        allowed_tools = {"query", "create_record"}
        for name, args in node.calls:
            self.assertIn(name, allowed_tools, f"unexpected MCP tool call: {name}")
            if name == "create_record":
                self.assertEqual(args.get("entity"), "MatchGroup",
                                  "MatchAgent must only ever create MatchGroup")
                self.assertNotEqual(args.get("entity"), "StewardshipTask",
                                     "MatchAgent holds no grant on StewardshipTask at all")
                self.assertIn("basis", args)
                self.assertIn("idempotency_key", args)
                self.assertTrue(8 <= len(args["idempotency_key"]) <= 128)
                self.assertNotIn("status", args["values"],
                                  "must never set MatchGroup.status -- leave the New default")

        # no update_record and no delete_record calls of any kind
        tool_names = {n for n, _ in node.calls}
        self.assertNotIn("update_record", tool_names,
                          "MatchAgent must never update a record (esp. MasterRecord)")
        self.assertNotIn("delete_record", tool_names, "MatchAgent must never delete")


# ===========================================================================
# (d) normalization edge — trim/case/whitespace collapse, and a real difference
# ===========================================================================

class TestNormalizationEdge(unittest.TestCase):
    def test_trim_case_whitespace_collapse_join_same_group(self):
        master = {
            "a": _mr("Acme LLC", "OPS@Acme.example"),
            "b": _mr("  acme   llc  ", "ops@acme.example"),  # only formatting differs
        }
        node = FakeNode(master)
        with mock.patch.object(em, "mcp", node):
            result = em.run(KEY_COLUMNS)

        self.assertEqual(result["groups_created"], 1)
        [group] = node.match_groups().values()
        self.assertEqual(sorted(group["records"]), ["a", "b"])

    def test_semantically_different_values_do_not_group(self):
        master = {
            "a": _mr("Acme LLC", "ops@acme.example"),
            "b": _mr("Acme Corporation", "ops@acme.example"),  # a real difference, not formatting
        }
        node = FakeNode(master)
        with mock.patch.object(em, "mcp", node):
            result = em.run(KEY_COLUMNS)

        self.assertEqual(result["groups_created"], 0, "different names must NOT be treated as duplicates")
        self.assertEqual(len(node.match_groups()), 0)


# ===========================================================================
# bonus — missing-key-column exclusion, and Source-scoped reads
# ===========================================================================

class TestMissingKeyColumnAndScoping(unittest.TestCase):
    def test_record_missing_a_key_column_is_excluded_not_grouped_on_absence(self):
        master = {
            # both missing "email" entirely -- must NOT be treated as a match
            # on shared absence.
            "a": {"raw": {"name": "Acme LLC"}},
            "b": {"raw": {"name": "Acme LLC"}},
        }
        node = FakeNode(master)
        with mock.patch.object(em, "mcp", node):
            result = em.run(KEY_COLUMNS)

        self.assertEqual(result["groups_created"], 0)

    def test_source_filter_scopes_the_query_call(self):
        master = {
            "a": {"raw": {"name": "Acme LLC", "email": "ops@acme.example"}, "source": "src-1"},
            "b": {"raw": {"name": "Acme LLC", "email": "ops@acme.example"}, "source": "src-1"},
            "c": {"raw": {"name": "Acme LLC", "email": "ops@acme.example"}, "source": "src-2"},
        }
        node = FakeNode(master)
        with mock.patch.object(em, "mcp", node):
            result = em.run(KEY_COLUMNS, source_id="src-1")

        self.assertEqual(result["records_scanned"], 2, "must only scan the scoped Source's records")
        query_calls = [a for n, a in node.calls if n == "query"]
        self.assertTrue(all(a.get("filter") == {"source": "src-1"} for a in query_calls))


if __name__ == "__main__":
    unittest.main(verbosity=2)
