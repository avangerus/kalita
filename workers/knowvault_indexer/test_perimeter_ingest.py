#!/usr/bin/env python3
"""Tests for the KnowVault indexer's perimeter tripwire on ingest (TECHDEBT
"Безопасность LLM-периметра" (a)/(b)): workers/perimeter/detector.score() now
runs on every file's extracted text before chunking/embedding; a flagged file
gets (1) an internal comment on the Source (the same H4 layer-3 pattern
workers/agent_runner/runner.py's handle_legal already uses for Contract.terms)
and (2) a flag_ingest call that journals the core ingest.flagged event itself
-- reusing the honeypot corpus (testdata/honeypot/atk), same as
workers/agent_runner/test_runner.py::TestHandleLegalPerimeter does for legal.

No live stack required -- indexer.tool/settings/reconcile_collection/
indexed_fingerprints/embed/ensure_collection/http are all monkeypatched,
mirroring test_bugs_p3.py's TestBug08 harness.

Run:  python -m pytest test_perimeter_ingest.py -v
  or: python test_perimeter_ingest.py
"""
import os
import shutil
import sys
import tempfile
import unittest
from unittest import mock

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

_REPO_ROOT = os.path.normpath(os.path.join(_HERE, "..", ".."))
_HONEYPOT_ATK = os.path.join(_REPO_ROOT, "testdata", "honeypot", "atk")


# ---------------------------------------------------------------------------
# bootstrap stub (mirrors test_bugs_p1.py / test_bugs_p2.py / test_bugs_p3.py)
# ---------------------------------------------------------------------------

class _FakeBootstrap:
    def get_token(self):
        return "test-token"

    def __getattr__(self, name):
        return lambda *a, **kw: "test-token"


sys.modules.setdefault("bootstrap", _FakeBootstrap())

import indexer as idx  # noqa: E402


def _run_sync(tmpdir, tid="tid-perimeter"):
    """Run sync_source over *tmpdir* with everything but tool() mocked out
    (exactly like test_bugs_p3.py's TestBug08 harness) and return the
    (name, args) pairs recorded for every tool() call the run made."""
    calls = []

    def mock_tool(name, args=None):
        calls.append((name, dict(args or {})))
        return {}

    src = {"values": {"workspace": "ws-perimeter", "kind": "Files", "path": tmpdir}}
    with mock.patch.object(idx, "tool", mock_tool), \
         mock.patch.object(idx, "settings", return_value=("text-embedding-bge-m3", 512)), \
         mock.patch.object(idx, "reconcile_collection", return_value=False), \
         mock.patch.object(idx, "indexed_fingerprints", return_value={}), \
         mock.patch.object(idx, "embed", return_value=[[0.1] * 8]), \
         mock.patch.object(idx, "ensure_collection"), \
         mock.patch.object(idx, "http", return_value={}):
        idx.sync_source("sid-perimeter", src, {"type": "task", "id": tid}, tid)
    return calls


class TestPerimeterIngestFlagsHoneypot(unittest.TestCase):
    """TECHDEBT (a): a poisoned document ingested by KnowVault must be
    flagged -- via honeypot fixtures, not a bespoke string, same discipline
    as TestHandleLegalPerimeter."""

    _HONEYPOT_FILE = "atk_kb_en_direct_07.txt"

    def _stage_honeypot(self, tmpdir):
        dst = os.path.join(tmpdir, self._HONEYPOT_FILE)
        shutil.copyfile(os.path.join(_HONEYPOT_ATK, self._HONEYPOT_FILE), dst)
        return dst

    def test_honeypot_document_triggers_flag_ingest(self):
        tmpdir = tempfile.mkdtemp()
        self._stage_honeypot(tmpdir)
        calls = _run_sync(tmpdir)

        flag_calls = [a for n, a in calls if n == "flag_ingest"]
        self.assertEqual(len(flag_calls), 1,
                          f"honeypot document must trigger flag_ingest exactly once, got {calls!r}")
        args = flag_calls[0]
        self.assertEqual(args["entity"], "Source")
        self.assertEqual(args["id"], "sid-perimeter")
        self.assertEqual(args["source"], self._HONEYPOT_FILE)
        self.assertGreaterEqual(args["score"], idx.perimeter.THRESHOLD)
        self.assertTrue(args["reasons"], "flag_ingest must carry the detector's reason tags")
        self.assertIn("idempotency_key", args)
        self.assertEqual(args["basis"], {"type": "task", "id": "tid-perimeter"})

    def test_honeypot_document_also_gets_a_visible_internal_comment(self):
        # (b) is the NEW journal-level signal; the pre-existing legal pattern
        # (an internal comment, visible without any new tooling) must still
        # fire too -- the two are complementary, not a replacement.
        tmpdir = tempfile.mkdtemp()
        self._stage_honeypot(tmpdir)
        calls = _run_sync(tmpdir)

        comment_calls = [a for n, a in calls if n == "comment" and a.get("internal")
                         and "perimeter" in a.get("body", "")]
        self.assertEqual(len(comment_calls), 1,
                          f"honeypot document must get exactly one internal perimeter comment, got {calls!r}")
        self.assertIn("possible prompt injection", comment_calls[0]["body"])
        self.assertIn(self._HONEYPOT_FILE, comment_calls[0]["body"])

    def test_honeypot_document_is_still_indexed_tripwire_not_filter(self):
        # the detector NEVER blocks ingestion -- the document is indexed
        # (embedded/upserted/progress-reported) exactly as if it were clean;
        # the flag is only ever an additional, visible signal.
        tmpdir = tempfile.mkdtemp()
        self._stage_honeypot(tmpdir)
        calls = _run_sync(tmpdir)

        self.assertTrue(any(n == "report_progress" and self._HONEYPOT_FILE in a.get("note", "")
                            for n, a in calls),
                        f"the flagged document must still be processed/reported, not skipped: {calls!r}")
        self.assertTrue(any(n == "update_record" for n, _ in calls),
                        "a changed source (the new honeypot file) must still update Source.documents")


class TestPerimeterIngestCleanDocumentNotFlagged(unittest.TestCase):
    """TECHDEBT (a) false-positive control: an ordinary business document
    must not be flagged, mirroring TestHandleLegalPerimeter's clean case."""

    def test_clean_document_triggers_no_flag_and_no_comment(self):
        tmpdir = tempfile.mkdtemp()
        path = os.path.join(tmpdir, "clean.txt")
        with open(path, "w", encoding="utf-8") as f:
            f.write("Quarterly revenue grew 12% year over year, driven by growth in the "
                     "EMEA region and a new enterprise pricing tier launched in March.")
        calls = _run_sync(tmpdir)

        self.assertEqual([a for n, a in calls if n == "flag_ingest"], [],
                          f"a clean document must not trigger flag_ingest, got {calls!r}")
        self.assertEqual([a for n, a in calls if n == "comment"], [],
                          f"a clean document must not trigger a perimeter comment, got {calls!r}")
        # still indexed, same as any other clean file
        self.assertTrue(any(n == "report_progress" for n, _ in calls))


class TestPerimeterIngestDetectorItself(unittest.TestCase):
    """Ties the wiring straight back to the shared H4/LLD-01 §6 corpus
    contract, independent of the sync_source plumbing above -- mirrors
    TestHandleLegalPerimeter.test_perimeter_detector_itself_flags_the_honeypot_fixture."""

    def test_perimeter_detector_flags_several_honeypot_fixtures(self):
        sample = ["atk_kb_en_direct_07.txt", "atk_kb_en_obfuscated_16.txt", "atk_kb_ru_direct_08.txt"]
        for name in sample:
            with open(os.path.join(_HONEYPOT_ATK, name), encoding="utf-8") as f:
                text = f.read()
            score, reasons = idx.perimeter.score(text)
            self.assertGreaterEqual(score, idx.perimeter.THRESHOLD,
                                     f"{name}: expected score>=THRESHOLD, got {score} ({reasons})")


if __name__ == "__main__":
    unittest.main(verbosity=2)
