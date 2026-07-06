#!/usr/bin/env python3
"""Tests for workers/agent_runner/runner.py's handle_legal — specifically
BUG-03 (I-3: "an error is an event, silent failures do not exist"): a
parse-fail or unusable model reply must NOT write a false "Low" risk and
must NOT act(review) to move the contract forward. No live stack required —
runner.mcp and runner.llm are monkeypatched.

Run:  python -m pytest test_runner.py -v
  or: python test_runner.py
"""
import json
import os
import sys
import unittest
from unittest import mock

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

import runner  # noqa: E402


def _recording_mcp(record_values, playbook_rules=None, create_finding_fails=False):
    """A stand-in for runner.mcp(token, name, args) -> (payload, is_error) that
    also records every call so a test can assert what did/did not happen."""
    calls = []

    def fake_mcp(token, name, args):
        calls.append((name, args))
        if name == "describe_entity":
            return {"fields": [{"name": k, "type": "string"} for k in record_values],
                    "workflow": {"field": "status", "transitions": []}}, False
        if name == "get_record":
            return {"values": dict(record_values)}, False
        if name == "query":
            return {"records": playbook_rules or []}, False
        if name == "create_record":
            if create_finding_fails:
                return {"error": "rejected"}, True
            return {"status": "done"}, False
        if name in ("update_record", "comment", "act"):
            return {"status": "applied"}, False
        return {}, False

    return fake_mcp, calls


class TestHandleLegalParseFailure(unittest.TestCase):
    """BUG-03 acceptance: garbage/unparseable model output must not be
    laundered into a false Low-risk, workflow-advancing review."""

    def _run(self, llm_reply, create_finding_fails=False):
        fake_mcp, calls = _recording_mcp(
            {"title": "MSA", "terms": "no cap"},
            playbook_rules=[{"id": "r1", "values": {"topic": "Liability",
                                                      "our_position": "cap at fees",
                                                      "severity": "High", "active": True}}],
            create_finding_fails=create_finding_fails)
        task = {"id": "t1", "entity": "Contract", "record_id": "c-1", "action": "review"}
        with mock.patch.object(runner, "LLM_URL", "http://fake-llm"), \
             mock.patch.object(runner, "llm", lambda messages, max_tokens=400: llm_reply), \
             mock.patch.object(runner, "mcp", fake_mcp):
            ok, msg = runner.handle_legal("tok", task)
        return ok, msg, calls

    def test_garbage_reply_fails_the_task_not_the_contract(self):
        ok, msg, calls = self._run("Sure! Here is my analysis of the contract: looks fine, no worries.")
        self.assertFalse(ok, "a garbage reply must not report success")
        self.assertIn("FAILED to parse", msg)

    def test_garbage_reply_never_writes_risk(self):
        _, _, calls = self._run("not json at all, just prose")
        for name, args in calls:
            if name == "update_record":
                self.fail("update_record must not be called on a parse failure, got %r" % (args,))

    def test_garbage_reply_never_advances_the_workflow(self):
        _, _, calls = self._run("not json at all, just prose")
        for name, args in calls:
            if name == "act":
                self.fail("act(review) must not be called on a parse failure, got %r" % (args,))

    def test_truncated_json_is_also_a_parse_failure(self):
        ok, msg, calls = self._run('[{"clause": "Liability", "risk": "High", "issue": "no cap"')
        self.assertFalse(ok)
        for name, _ in calls:
            self.assertNotIn(name, ("update_record", "act"))

    def test_non_list_json_is_a_parse_failure(self):
        ok, msg, calls = self._run('{"summary": "looks fine"}')
        self.assertFalse(ok)
        self.assertIn("FAILED to parse", msg)

    def test_all_findings_rejected_by_the_node_is_also_a_failure(self):
        # syntactically valid findings, but every create_record call is
        # rejected -- nothing trustworthy actually landed, so this must not
        # be reported as a clean, zero-finding, Low-risk review either.
        ok, msg, calls = self._run(
            '[{"clause": "Liability", "risk": "High", "issue": "no cap"}]',
            create_finding_fails=True)
        self.assertFalse(ok)
        for name, args in calls:
            if name == "update_record":
                self.fail("update_record must not be called when every finding write failed")
            if name == "act":
                self.fail("act(review) must not be called when every finding write failed")

    def test_genuinely_empty_findings_is_not_an_error(self):
        # a VALID, explicit empty array means "reviewed, nothing found" -- a
        # real Low-risk outcome, distinct from a parse failure (I-3).
        ok, msg, calls = self._run("[]")
        self.assertTrue(ok, msg)
        risk_writes = [a["values"] for n, a in calls if n == "update_record"]
        self.assertEqual(len(risk_writes), 1)
        self.assertEqual(risk_writes[0]["risk"], "Low")
        acts = [a for n, a in calls if n == "act"]
        self.assertEqual(len(acts), 1, "a clean review must still submit for the lawyer")

    def test_real_findings_still_set_risk_and_advance(self):
        ok, msg, calls = self._run(
            '[{"clause": "Liability", "risk": "High", "issue": "no cap", "redline": "add a cap"}]')
        self.assertTrue(ok, msg)
        risk_writes = [a["values"] for n, a in calls if n == "update_record"]
        self.assertEqual(risk_writes[0]["risk"], "High")
        finding_creates = [a for n, a in calls if n == "create_record" and a["entity"] == "Finding"]
        self.assertEqual(len(finding_creates), 1)
        acts = [a for n, a in calls if n == "act"]
        self.assertEqual(len(acts), 1)


class TestHandleLegalPerimeter(unittest.TestCase):
    """DEMO-0 §3 test 2 / H4 layer 3: a poisoned contract (injection in
    `terms`) must be flagged by the perimeter detector -- reusing the
    honeypot corpus, not a bespoke string (LLD-01 §6).

    N4 depth gap A: the flag must now land on BOTH channels -- the internal
    comment (pre-existing) AND the core ingest.flagged event via flag_ingest
    (new), exactly like workers/knowvault_indexer/indexer.py's ingest path
    (see test_perimeter_ingest.py's TestPerimeterIngestFlagsHoneypot, the
    sibling of this test class)."""

    _REPO_ROOT = os.path.normpath(os.path.join(_HERE, "..", ".."))
    _POISONED_CONTRACT = os.path.join(_REPO_ROOT, "testdata", "honeypot", "atk",
                                       "atk_contract_en_direct_01.txt")

    def _poisoned_terms(self):
        with open(self._POISONED_CONTRACT, encoding="utf-8") as f:
            return f.read()

    def test_poisoned_terms_are_flagged_as_an_internal_comment(self):
        terms = self._poisoned_terms()
        fake_mcp, calls = _recording_mcp({"title": "MSA", "terms": terms})
        task = {"id": "t1", "entity": "Contract", "record_id": "c-1", "action": "review"}
        with mock.patch.object(runner, "LLM_URL", "http://fake-llm"), \
             mock.patch.object(runner, "llm", lambda messages, max_tokens=400: "[]"), \
             mock.patch.object(runner, "mcp", fake_mcp):
            ok, msg = runner.handle_legal("tok", task)
        self.assertTrue(ok, msg)
        flags = [a for n, a in calls if n == "comment" and a.get("internal")
                  and "perimeter" in a.get("body", "")]
        self.assertEqual(len(flags), 1, "the poisoned contract must be flagged exactly once, got %r" % (calls,))
        self.assertIn("possible prompt injection", flags[0]["body"])

    def test_poisoned_terms_also_journal_ingest_flagged_via_flag_ingest(self):
        # N4 depth gap A (new): the comment above is not enough on its own --
        # the SAME detection must ALSO journal the core event, exactly as
        # KnowVault's ingest path does for Source, so the signal survives in
        # the journal/audit trail, not only in the comment thread.
        terms = self._poisoned_terms()
        fake_mcp, calls = _recording_mcp({"title": "MSA", "terms": terms})
        task = {"id": "t3", "entity": "Contract", "record_id": "c-3", "action": "review"}
        with mock.patch.object(runner, "LLM_URL", "http://fake-llm"), \
             mock.patch.object(runner, "llm", lambda messages, max_tokens=400: "[]"), \
             mock.patch.object(runner, "mcp", fake_mcp):
            ok, msg = runner.handle_legal("tok", task)
        self.assertTrue(ok, msg)
        flag_calls = [a for n, a in calls if n == "flag_ingest"]
        self.assertEqual(len(flag_calls), 1,
                         "the poisoned contract must trigger flag_ingest exactly once, got %r" % (calls,))
        args = flag_calls[0]
        self.assertEqual(args["entity"], "Contract")
        self.assertEqual(args["id"], "c-3")
        self.assertEqual(args["source"], "terms")
        self.assertGreaterEqual(args["score"], runner.perimeter.THRESHOLD)
        self.assertTrue(args["reasons"], "flag_ingest must carry the detector's reason tags")
        self.assertIn("idempotency_key", args)
        self.assertEqual(args["basis"], {"type": "task", "id": "t3"})
        # both channels fire, and with DIFFERENT idempotency keys -- reusing
        # the comment's key for flag_ingest would collide in the Store's
        # global idempotency dedup and silently swallow one of the two events.
        comment_calls = [a for n, a in calls if n == "comment" and a.get("internal")]
        self.assertEqual(len(comment_calls), 1)
        self.assertNotEqual(comment_calls[0]["idempotency_key"], args["idempotency_key"])

    def test_clean_terms_are_not_flagged(self):
        fake_mcp, calls = _recording_mcp(
            {"title": "MSA", "terms": "Liability capped at one month of fees."})
        task = {"id": "t2", "entity": "Contract", "record_id": "c-2", "action": "review"}
        with mock.patch.object(runner, "LLM_URL", "http://fake-llm"), \
             mock.patch.object(runner, "llm", lambda messages, max_tokens=400: "[]"), \
             mock.patch.object(runner, "mcp", fake_mcp):
            runner.handle_legal("tok", task)
        flags = [a for n, a in calls if n == "comment"]
        self.assertEqual(flags, [], "clean terms must not trigger a perimeter comment")
        self.assertEqual([a for n, a in calls if n == "flag_ingest"], [],
                         "clean terms must not trigger flag_ingest either")

    def test_perimeter_detector_itself_flags_the_honeypot_fixture(self):
        # the detector's own opinion, independent of the runner wiring above --
        # ties this straight back to the H4/LLD-01 §6 corpus contract.
        score, reasons = runner.perimeter.score(self._poisoned_terms())
        self.assertGreaterEqual(score, runner.perimeter.THRESHOLD, reasons)


if __name__ == "__main__":
    unittest.main(verbosity=2)
