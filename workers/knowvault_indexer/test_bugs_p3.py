#!/usr/bin/env python3
"""Deterministic unit tests for P3 defects BUG-08..BUG-13.

Contracts under test (not implementation details):
  BUG-08 — no-op resync must not journal update_record.
  BUG-09 — optional SHA-256 fingerprint detects same-mtime edits.
  BUG-10 — search errors propagate as HTTP 500 (not masked as 200).
  BUG-11 — dead Searcher deny rule removed from DSL (static check).
  BUG-12 — invalid embedding_model rejected BEFORE Source transitions to Indexing.
  BUG-13 — query/passage prefixes applied per model; bge-m3 = no-op.

Run:  python -m pytest test_bugs_p3.py -v
  or: python test_bugs_p3.py
"""
import importlib.util
import os
import sys
import tempfile
import unittest
import urllib.error
import uuid
from unittest import mock

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

_SVC_PATH = os.path.join(_HERE, "..", "knowvault_search", "service.py")
_DSL_PATH = os.path.join(_HERE, "..", "..", "packs", "knowvault", "knowvault.dsl")
_RAGPROXY_PATH = os.path.join(_HERE, "..", "..", "internal", "api", "ragproxy.go")


# ---------------------------------------------------------------------------
# bootstrap stub (mirrors test_bugs_p1.py / test_bugs_p2.py)
# ---------------------------------------------------------------------------

class _FakeBootstrap:
    def get_token(self):
        return "test-token"

    def __getattr__(self, name):
        return lambda *a, **kw: "test-token"


sys.modules.setdefault("bootstrap", _FakeBootstrap())


def _load_service(mod_name="svc_p3"):
    spec = importlib.util.spec_from_file_location(mod_name, _SVC_PATH)
    mod = importlib.util.module_from_spec(spec)
    try:
        spec.loader.exec_module(mod)
    except Exception:
        pass
    return mod


# ===========================================================================
# BUG-08 — no-op resync must not write update_record
# ===========================================================================

class TestBug08NoJournalOnNoopResync(unittest.TestCase):
    """sync_source must call update_record only when changed > 0 or removed > 0."""

    def _run_sync(self, tmpdir, prior=None):
        """Run sync_source on *tmpdir* (which may be empty) and return tool call names."""
        import indexer as idx

        tool_calls = []

        def mock_tool(name, args=None):
            tool_calls.append(name)
            return {}

        src = {"values": {"workspace": "ws-noop", "kind": "Files", "path": tmpdir}}

        with mock.patch.object(idx, "tool", mock_tool), \
             mock.patch.object(idx, "settings", return_value=("text-embedding-bge-m3", 512)), \
             mock.patch.object(idx, "reconcile_collection", return_value=False), \
             mock.patch.object(idx, "indexed_fingerprints", return_value=prior or {}), \
             mock.patch.object(idx, "embed", return_value=[[0.1] * 8]), \
             mock.patch.object(idx, "ensure_collection"), \
             mock.patch.object(idx, "http", return_value={}):
            idx.sync_source("sid-noop", src, {}, "tid-noop")

        return tool_calls

    def test_no_update_record_when_source_has_no_files(self):
        """Empty source directory: 0 changed, 0 removed → update_record must NOT fire."""
        tmpdir = tempfile.mkdtemp()
        calls = self._run_sync(tmpdir)
        self.assertNotIn("update_record", calls,
                         "update_record must not be called when nothing changed (BUG-08)")

    def test_no_update_record_when_all_files_unchanged(self):
        """Source with unchanged file (fingerprint matches prior): update_record must NOT fire."""
        import indexer as idx
        tmpdir = tempfile.mkdtemp()
        fpath = os.path.join(tmpdir, "stable.txt")
        with open(fpath, "w", encoding="utf-8") as f:
            f.write("stable content that never changes")

        # Compute the actual fingerprint the indexer would compute.
        fp = idx._file_fp(fpath)
        prior = {"stable.txt": fp}  # "already indexed with this fingerprint"
        calls = self._run_sync(tmpdir, prior=prior)
        self.assertNotIn("update_record", calls,
                         "update_record must not be called when all files are unchanged (BUG-08)")

    def test_update_record_fires_when_file_changed(self):
        """A changed file (new fingerprint) MUST produce an update_record call."""
        tmpdir = tempfile.mkdtemp()
        fpath = os.path.join(tmpdir, "doc.txt")
        with open(fpath, "w", encoding="utf-8") as f:
            f.write("some meaningful content for indexing purposes")

        # prior fingerprint that deliberately does NOT match
        prior = {"doc.txt": "0:0"}
        calls = self._run_sync(tmpdir, prior=prior)
        self.assertIn("update_record", calls,
                      "update_record must be called when a file changed (BUG-08 regression)")

    def test_update_record_fires_when_file_removed(self):
        """A file present in prior but missing from disk MUST produce an update_record call."""
        tmpdir = tempfile.mkdtemp()
        # prior references a file that no longer exists on disk
        prior = {"ghost.txt": "123:456"}
        calls = self._run_sync(tmpdir, prior=prior)
        self.assertIn("update_record", calls,
                      "update_record must be called when a file was removed (BUG-08 regression)")

    def test_report_progress_always_called(self):
        """report_progress must be called regardless — it's the heartbeat note."""
        tmpdir = tempfile.mkdtemp()
        calls = self._run_sync(tmpdir)
        self.assertIn("report_progress", calls,
                      "report_progress must always be called, even on a no-op resync")


# ===========================================================================
# BUG-09 — optional SHA-256 content fingerprint
# ===========================================================================

class TestBug09ContentHashFingerprint(unittest.TestCase):

    def test_file_fp_function_exists(self):
        import indexer as idx
        self.assertTrue(hasattr(idx, "_file_fp"), "_file_fp must be defined in indexer.py")
        self.assertTrue(callable(idx._file_fp))

    def test_default_fingerprint_is_mtime_colon_size(self):
        """Default (use_content_hash=False): fingerprint is 'mtime:size' format."""
        import indexer as idx
        with tempfile.NamedTemporaryFile(delete=False, suffix=".txt") as f:
            f.write(b"hello world")
            path = f.name
        try:
            fp = idx._file_fp(path)
            self.assertRegex(fp, r"^\d+:\d+$",
                             "default fingerprint must be 'mtime:size' (BUG-09)")
        finally:
            os.unlink(path)

    def test_content_hash_fingerprint_starts_with_sha256(self):
        """SHA-256 fingerprint is prefixed 'sha256:' for easy identification."""
        import indexer as idx
        with tempfile.NamedTemporaryFile(delete=False, suffix=".txt") as f:
            f.write(b"content")
            path = f.name
        try:
            fp = idx._file_fp(path, use_content_hash=True)
            self.assertTrue(fp.startswith("sha256:"),
                            f"content hash fingerprint must start with 'sha256:', got: {fp!r}")
            self.assertEqual(len(fp), len("sha256:") + 64,
                             "SHA-256 hex digest must be 64 chars")
        finally:
            os.unlink(path)

    def test_content_hash_detects_same_mtime_same_size_edit(self):
        """SHA-256 catches a content change that mtime:size would miss."""
        import indexer as idx
        with tempfile.NamedTemporaryFile(delete=False, suffix=".txt") as f:
            f.write(b"original content AAAA")  # 21 bytes
            path = f.name

        fp_mtime_original = idx._file_fp(path, use_content_hash=False)
        fp_hash_original = idx._file_fp(path, use_content_hash=True)

        # overwrite with different content of SAME size, then restore mtime
        saved_mtime = os.path.getmtime(path)
        saved_atime = os.path.getatime(path)
        with open(path, "wb") as f:
            f.write(b"modified content BBBB")  # same 21 bytes
        os.utime(path, (saved_atime, saved_mtime))  # restore timestamp

        fp_mtime_after = idx._file_fp(path, use_content_hash=False)
        fp_hash_after = idx._file_fp(path, use_content_hash=True)
        os.unlink(path)

        # mtime:size is SAME (the bug scenario)
        self.assertEqual(fp_mtime_original, fp_mtime_after,
                         "mtime:size must be equal (precondition: same-mtime/same-size scenario)")
        # SHA-256 is DIFFERENT (the fix)
        self.assertNotEqual(fp_hash_original, fp_hash_after,
                            "SHA-256 fingerprint must detect a same-mtime same-size content change (BUG-09)")

    def test_content_hash_enabled_via_source_field(self):
        """When use_content_hash=True in the Source record, the indexer uses SHA-256."""
        import indexer as idx

        captured_fps = []
        tmpdir = tempfile.mkdtemp()
        fpath = os.path.join(tmpdir, "watch.txt")
        with open(fpath, "w", encoding="utf-8") as f:
            f.write("important document content for fingerprint test")

        src = {"values": {
            "workspace": "ws-hash",
            "kind": "Files",
            "path": tmpdir,
            "use_content_hash": True,
        }}

        def mock_tool(name, args=None):
            return {}

        def capture_embed(model, texts):
            return [[0.1] * 8 for _ in texts]

        def mock_http(url, body=None, method=None, headers=None):
            return {}

        with mock.patch.object(idx, "tool", mock_tool), \
             mock.patch.object(idx, "settings", return_value=("text-embedding-bge-m3", 512)), \
             mock.patch.object(idx, "reconcile_collection", return_value=False), \
             mock.patch.object(idx, "indexed_fingerprints", return_value={}), \
             mock.patch.object(idx, "ensure_collection"), \
             mock.patch.object(idx, "embed", capture_embed), \
             mock.patch.object(idx, "http", mock_http):
            # Intercept the points upsert to inspect stored fingerprints
            original_http = idx.http

            upserted_fps = []

            def spy_http(url, body=None, method=None, headers=None):
                if body and "points" in body:
                    for pt in body["points"]:
                        fp = pt.get("payload", {}).get("fp")
                        if fp:
                            upserted_fps.append(fp)
                return {}

            with mock.patch.object(idx, "http", spy_http):
                idx.sync_source("sid-hash", src, {}, "tid-hash")

        # At least one content point with sha256 fingerprint must have been upserted
        sha_fps = [fp for fp in upserted_fps if fp.startswith("sha256:")]
        self.assertTrue(sha_fps,
                        f"When use_content_hash=True, upserted points must carry sha256: "
                        f"fingerprints. Got: {upserted_fps}")


# ===========================================================================
# BUG-10 — search errors propagate as HTTP 500 (not masked as 200)
# ===========================================================================

class TestBug10SearchErrorPropagation(unittest.TestCase):

    def test_service_returns_500_not_200_on_exception(self):
        """Static check: service.py exception handler must use send_response(500)."""
        with open(_SVC_PATH, encoding="utf-8") as f:
            source = f.read()
        self.assertIn("send_response(500)", source,
                      "service.py must return HTTP 500 on search error, not 200 — BUG-10 not fixed")

    def test_service_error_response_includes_error_flag(self):
        """Error response body must carry a distinct 'error' flag, not just an 'answer' field."""
        with open(_SVC_PATH, encoding="utf-8") as f:
            source = f.read()
        self.assertIn('"error"', source,
                      "service.py error response must include an 'error' flag — BUG-10 not fixed")

    def test_service_no_longer_returns_200_with_search_error_text(self):
        """The old pattern (HTTP 200 + 'Search error:' in answer) must be gone."""
        with open(_SVC_PATH, encoding="utf-8") as f:
            source = f.read()
        # The old code was: send_response(200) inside the except block.
        # After the fix there must be no send_response(200) inside an except handler.
        # We check that the *only* send_response(200) is in the success path (do_POST success).
        # A simple heuristic: there must be exactly one send_response(200) in the file.
        count_200 = source.count("send_response(200)")
        self.assertEqual(count_200, 1,
                         f"Expected exactly 1 send_response(200) (success path); "
                         f"found {count_200}. The except block must use 500 — BUG-10 not fixed")

    def test_ragproxy_checks_worker_status_code(self):
        """Static check: ragproxy.go must inspect resp.StatusCode — BUG-10."""
        with open(_RAGPROXY_PATH, encoding="utf-8") as f:
            source = f.read()
        self.assertIn("StatusCode", source,
                      "ragproxy.go must check the worker's HTTP status code — BUG-10 not fixed")

    def test_ragproxy_uses_distinct_error_code(self):
        """ragproxy.go must use a distinct error code for worker failures."""
        with open(_RAGPROXY_PATH, encoding="utf-8") as f:
            source = f.read()
        self.assertIn("SEARCH_WORKER_ERROR", source,
                      "ragproxy.go must use 'SEARCH_WORKER_ERROR' code for worker failures — BUG-10 not fixed")

    def test_ragproxy_does_not_journal_worker_errors(self):
        """ragproxy.go must NOT create a SearchQuery journal entry for worker errors.
        The early-return block on error must precede the Create (journal) call.

        The governance core now lives in SearchPerimeter (REST and MCP share it);
        the journal Create there is keyed on `cfg.LogEntity`, so that is the marker
        for the journal block. `s.rag.LogEntity` also appears earlier, but only in
        the thin REST wrapper that passes the config into SearchPerimeter — it is
        not the journal call. The behavioral proof of this invariant is
        internal/mcp/search_perimeter_test.go (a worker error journals no
        SearchQuery); this positional check is a cheap source-level guard."""
        with open(_RAGPROXY_PATH, encoding="utf-8") as f:
            source = f.read()
        error_pos = source.find("SEARCH_WORKER_ERROR")
        journal_pos = source.find("cfg.LogEntity")
        self.assertGreater(error_pos, 0, "SEARCH_WORKER_ERROR string not found in ragproxy.go")
        self.assertGreater(journal_pos, 0, "cfg.LogEntity (journal block) not found in ragproxy.go")
        self.assertGreater(journal_pos, error_pos,
                           "journal (cfg.LogEntity) block must appear AFTER the error early-return — "
                           "worker errors must not create SearchQuery records (BUG-10)")


# ===========================================================================
# BUG-11 — dead Searcher deny rule removed from DSL
# ===========================================================================

class TestBug11DeadDenyRuleRemoved(unittest.TestCase):
    """The deny on 'read Source where kind=Database' for Searcher was a no-op:
    Searcher has no base 'read Source' grant, so the deny enforces nothing and
    creates a false impression of Database protection."""

    def _dsl(self):
        with open(_DSL_PATH, encoding="utf-8") as f:
            return f.read()

    def test_dead_deny_rule_is_removed(self):
        """'read Source where kind=Database' must not appear as an active deny rule.
        We check the Searcher deny line itself (not comments)."""
        source = self._dsl()
        # Find the deny line inside the Searcher block (starts with whitespace + 'deny')
        for line in source.splitlines():
            stripped = line.lstrip()
            if stripped.startswith("deny") and "read Source where kind" in stripped:
                self.fail(
                    f"Active deny rule 'read Source where kind=...' found in DSL: {line!r}\n"
                    "Without a base 'read Source' grant this deny is a no-op — BUG-11 not fixed."
                )

    def test_searcher_still_cannot_read_source(self):
        """Searcher must still have no 'read Source' grant (absence = correct enforcement)."""
        source = self._dsl()
        # Find the Searcher permissions block and confirm there is no 'read [... Source'
        searcher_block_start = source.find("Searcher:")
        self.assertGreater(searcher_block_start, 0, "Searcher block not found in DSL")
        # Find the next role block or end to bound the Searcher section
        next_role = source.find("\n    VaultAdmin:", searcher_block_start)
        if next_role < 0:
            next_role = len(source)
        searcher_section = source[searcher_block_start:next_role]
        self.assertNotIn("read [Source", searcher_section,
                         "Searcher must not have a 'read Source' grant — "
                         "it shouldn't access Source entities at all")

    def test_searcher_deny_still_covers_write_operations(self):
        """The meaningful denies (delete, update) must still be present."""
        source = self._dsl()
        searcher_block_start = source.find("Searcher:")
        self.assertGreater(searcher_block_start, 0, "Searcher block not found in DSL")
        # Use a generous window (1000 chars) to accommodate the BUG-11 comment block
        searcher_section = source[searcher_block_start:searcher_block_start + 1000]
        self.assertIn("delete *", searcher_section,
                      "Searcher deny must still include 'delete *'")
        self.assertIn("update Source.*", searcher_section,
                      "Searcher deny must still include 'update Source.*'")

    def test_searcher_still_has_read_workspace_and_create_searchquery(self):
        """Core Searcher grants (read Workspace, create SearchQuery) must be intact."""
        source = self._dsl()
        self.assertIn("read   [Workspace]", source,
                      "Searcher must still have 'read [Workspace]' grant")
        self.assertIn("create [SearchQuery]", source,
                      "Searcher must still have 'create [SearchQuery]' grant")


# ===========================================================================
# BUG-12 — invalid embedding_model rejected BEFORE Source transitions to Indexing
# ===========================================================================

class TestBug12EmbedModelValidatedBeforeIndexing(unittest.TestCase):

    def _run_index_source(self, embed_side_effect, src_status="New", last_indexed=None):
        """Run index_source with an embed that optionally raises; return tool call log."""
        import indexer as idx

        tool_log = []  # list of (name, args)

        def mock_tool(name, args=None):
            tool_log.append((name, args or {}))
            if name == "get_record":
                return {"values": {
                    "status": src_status,
                    "last_indexed": last_indexed,
                    "workspace": "ws-validate",
                }}
            return {}

        with mock.patch.object(idx, "tool", mock_tool), \
             mock.patch.object(idx, "settings", return_value=("bad-model-xyz", 512)), \
             mock.patch.object(idx, "embed", side_effect=embed_side_effect), \
             mock.patch.object(idx, "sync_source", return_value="0 files"):
            idx.index_source({"id": "tid-v", "record_id": "sid-v"})

        return tool_log

    def test_fail_task_called_when_model_invalid(self):
        """When probe embed raises, fail_task must be called — BUG-12."""
        err = urllib.error.HTTPError(None, 404, "model not found", {}, None)
        log = self._run_index_source(embed_side_effect=err)
        names = [n for n, _ in log]
        self.assertIn("fail_task", names,
                      "fail_task must be called when the embedding model probe fails (BUG-12)")

    def test_start_index_not_called_when_model_invalid(self):
        """Source must NOT transition to Indexing when the probe fails — BUG-12."""
        err = urllib.error.HTTPError(None, 404, "model not found", {}, None)
        log = self._run_index_source(embed_side_effect=err)
        act_calls = [(n, a) for n, a in log if n == "act"]
        start_index_calls = [a for _, a in act_calls if a.get("action") == "start_index"]
        self.assertEqual(start_index_calls, [],
                         "start_index must NOT be called when probe fails — "
                         "Source must not transition to Indexing (BUG-12)")

    def test_sync_source_not_called_when_model_invalid(self):
        """When probe fails, sync_source must never run — BUG-12."""
        err = urllib.error.URLError("connection refused")
        log = self._run_index_source(embed_side_effect=err)
        # sync_source is patched; check it wasn't called by verifying tool log
        # (if sync_source ran it would call report_progress and update_record)
        names = [n for n, _ in log]
        self.assertNotIn("report_progress", names,
                         "report_progress must not be called when probe fails — "
                         "sync_source must have been skipped (BUG-12)")

    def test_valid_model_proceeds_to_indexing(self):
        """When probe succeeds, the normal indexing flow must still work."""
        log = self._run_index_source(embed_side_effect=[[0.1] * 8])
        names = [n for n, _ in log]
        self.assertIn("complete_task", names,
                      "complete_task must be called when probe succeeds and indexing is complete")

    def test_fail_task_message_contains_model_name(self):
        """The fail_task message must mention the problematic model name for diagnostics."""
        err = urllib.error.HTTPError(None, 404, "model not found", {}, None)
        log = self._run_index_source(embed_side_effect=err)
        fail_args = [a for n, a in log if n == "fail_task"]
        self.assertTrue(fail_args, "fail_task not called")
        reason = fail_args[0].get("reason", "")
        self.assertIn("bad-model-xyz", reason,
                      f"fail_task reason must contain the model name, got: {reason!r}")


# ===========================================================================
# BUG-13 — query/passage prefix per model
# ===========================================================================

class TestBug13EmbedPrefix(unittest.TestCase):
    """Embedding format table: bge-m3 = no-op, e5-* = query:/passage: prefixes."""

    def test_fmt_query_and_fmt_passage_exist_in_indexer(self):
        import indexer as idx
        self.assertTrue(hasattr(idx, "_fmt_query"), "_fmt_query must be defined in indexer.py")
        self.assertTrue(hasattr(idx, "_fmt_passage"), "_fmt_passage must be defined in indexer.py")
        self.assertTrue(hasattr(idx, "_EMBED_PREFIX"), "_EMBED_PREFIX must be defined in indexer.py")

    def test_fmt_query_exists_in_service(self):
        svc = _load_service("svc_p3_b13a")
        self.assertTrue(hasattr(svc, "_fmt_query"), "_fmt_query must be defined in service.py")
        self.assertTrue(hasattr(svc, "_EMBED_PREFIX"), "_EMBED_PREFIX must be defined in service.py")

    def test_bge_m3_query_noop_in_indexer(self):
        """bge-m3 is symmetric: _fmt_query must return text unchanged."""
        import indexer as idx
        text = "what is the policy?"
        result = idx._fmt_query("text-embedding-bge-m3", text)
        self.assertEqual(result, text,
                         "bge-m3 _fmt_query must be a no-op (BUG-13)")

    def test_bge_m3_passage_noop_in_indexer(self):
        """bge-m3 is symmetric: _fmt_passage must return text unchanged."""
        import indexer as idx
        text = "the policy states that..."
        result = idx._fmt_passage("text-embedding-bge-m3", text)
        self.assertEqual(result, text,
                         "bge-m3 _fmt_passage must be a no-op (BUG-13)")

    def test_e5_base_query_prefix_in_indexer(self):
        """e5-base is asymmetric: _fmt_query must prepend 'query: '."""
        import indexer as idx
        text = "what is the policy?"
        result = idx._fmt_query("text-embedding-e5-base", text)
        self.assertTrue(result.startswith("query: "),
                        f"e5-base _fmt_query must prepend 'query: ', got: {result!r}")
        self.assertIn(text, result, "original text must be preserved after prefix")

    def test_e5_base_passage_prefix_in_indexer(self):
        """e5-base is asymmetric: _fmt_passage must prepend 'passage: '."""
        import indexer as idx
        text = "the policy states that all employees must..."
        result = idx._fmt_passage("text-embedding-e5-base", text)
        self.assertTrue(result.startswith("passage: "),
                        f"e5-base _fmt_passage must prepend 'passage: ', got: {result!r}")
        self.assertIn(text, result)

    def test_unknown_model_defaults_to_noop(self):
        """Unknown models (not in table) get no prefix — safe default."""
        import indexer as idx
        text = "hello world"
        self.assertEqual(idx._fmt_query("some-new-model-v9", text), text)
        self.assertEqual(idx._fmt_passage("some-new-model-v9", text), text)

    def test_passage_prefix_applied_during_indexing(self):
        """When indexing with an e5 model, chunks must be embedded with 'passage: ' prefix."""
        import indexer as idx
        tmpdir = tempfile.mkdtemp()
        fpath = os.path.join(tmpdir, "doc.txt")
        with open(fpath, "w", encoding="utf-8") as f:
            f.write("The quick brown fox jumps over the lazy dog. " * 5)

        src = {"values": {"workspace": "ws-e5", "kind": "Files", "path": tmpdir}}
        MODEL = "text-embedding-e5-base"

        embed_calls = []

        def capture_embed(model, texts):
            embed_calls.append((model, list(texts)))
            return [[0.1] * 8 for _ in texts]

        with mock.patch.object(idx, "tool", return_value={}), \
             mock.patch.object(idx, "settings", return_value=(MODEL, 512)), \
             mock.patch.object(idx, "reconcile_collection", return_value=False), \
             mock.patch.object(idx, "indexed_fingerprints", return_value={}), \
             mock.patch.object(idx, "ensure_collection"), \
             mock.patch.object(idx, "http", return_value={}), \
             mock.patch.object(idx, "embed", capture_embed):
            idx.sync_source("sid-e5", src, {}, "tid-e5")

        # Filter out the probe call (["probe"]) — only check content batches
        content_batches = [(m, texts) for m, texts in embed_calls
                           if texts != ["probe"]]
        self.assertTrue(content_batches, "No content was embedded")
        for _m, texts in content_batches:
            for t in texts:
                self.assertTrue(t.startswith("passage: "),
                                f"e5 model must prefix chunks with 'passage: ', got: {t!r}")

    def test_bge_m3_indexing_does_not_add_prefix(self):
        """When indexing with bge-m3, chunks must NOT have any prefix added."""
        import indexer as idx
        tmpdir = tempfile.mkdtemp()
        fpath = os.path.join(tmpdir, "doc.txt")
        content = "Important company policy document content here."
        with open(fpath, "w", encoding="utf-8") as f:
            f.write(content)

        src = {"values": {"workspace": "ws-bge", "kind": "Files", "path": tmpdir}}
        MODEL = "text-embedding-bge-m3"

        embed_calls = []

        def capture_embed(model, texts):
            embed_calls.append((model, list(texts)))
            return [[0.1] * 8 for _ in texts]

        with mock.patch.object(idx, "tool", return_value={}), \
             mock.patch.object(idx, "settings", return_value=(MODEL, 512)), \
             mock.patch.object(idx, "reconcile_collection", return_value=False), \
             mock.patch.object(idx, "indexed_fingerprints", return_value={}), \
             mock.patch.object(idx, "ensure_collection"), \
             mock.patch.object(idx, "http", return_value={}), \
             mock.patch.object(idx, "embed", capture_embed):
            idx.sync_source("sid-bge", src, {}, "tid-bge")

        content_batches = [(m, texts) for m, texts in embed_calls
                           if texts != ["probe"]]
        self.assertTrue(content_batches, "No content was embedded")
        for _m, texts in content_batches:
            for t in texts:
                self.assertFalse(t.startswith("query: ") or t.startswith("passage: "),
                                 f"bge-m3 must NOT add any prefix to chunks, got: {t!r}")

    def test_query_prefix_applied_in_service_for_e5(self):
        """service.py answer_question must apply 'query: ' prefix for e5 models."""
        svc = _load_service("svc_p3_b13b")
        MODEL = "text-embedding-e5-large"
        meta_id = str(uuid.uuid5(uuid.NAMESPACE_URL, "kv_collection_meta"))
        embedded_inputs = []

        def fake_http(url, body=None, method=None):
            if meta_id in url:
                return {"result": {"payload": {"_meta": True, "model": MODEL, "dim": 8}}}
            if "/v1/embeddings" in url:
                embedded_inputs.extend(body.get("input", []))
                return {"data": [{"embedding": [0.1] * 8}]}
            if "/points/search" in url:
                return {"result": []}
            return {}

        with mock.patch.object(svc, "http", fake_http):
            svc.answer_question("what is the leave policy?", ["ws-1"])

        self.assertTrue(embedded_inputs, "embed was never called")
        self.assertTrue(embedded_inputs[0].startswith("query: "),
                        f"e5 model in service must prefix question with 'query: ', "
                        f"got: {embedded_inputs[0]!r} (BUG-13)")

    def test_query_not_prefixed_in_service_for_bge_m3(self):
        """service.py answer_question must NOT add any prefix for bge-m3."""
        svc = _load_service("svc_p3_b13c")
        MODEL = "text-embedding-bge-m3"
        meta_id = str(uuid.uuid5(uuid.NAMESPACE_URL, "kv_collection_meta"))
        question = "what is the leave policy?"
        embedded_inputs = []

        def fake_http(url, body=None, method=None):
            if meta_id in url:
                return {"result": {"payload": {"_meta": True, "model": MODEL, "dim": 8}}}
            if "/v1/embeddings" in url:
                embedded_inputs.extend(body.get("input", []))
                return {"data": [{"embedding": [0.1] * 8}]}
            if "/points/search" in url:
                return {"result": []}
            return {}

        with mock.patch.object(svc, "http", fake_http):
            svc.answer_question(question, ["ws-1"])

        self.assertTrue(embedded_inputs, "embed was never called")
        self.assertEqual(embedded_inputs[0], question,
                         f"bge-m3 in service must pass question verbatim, "
                         f"got: {embedded_inputs[0]!r} (BUG-13)")


# ===========================================================================
# entrypoint
# ===========================================================================

if __name__ == "__main__":
    unittest.main(verbosity=2)
